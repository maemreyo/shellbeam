package verification

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/project"
	core "github.com/maemreyo/shellbeam/internal/core/verification"
	"github.com/maemreyo/shellbeam/internal/core/workspace"
)

const AuthorityExplicitCaller = "explicit_caller"

type PolicyService struct {
	workspaces WorkspaceLookup
	loader     PolicyLoader
	policies   PolicyAuthorityStore
	waivers    WaiverAuthorityStore
	snapshots  SourceSnapshotter
	projects   ProjectInspector
	commands   ProjectCommandResolver
	now        func() time.Time
}

type ActivateRequest struct {
	ActivationID           string
	WorkspaceID            string
	ProposedPolicyDigest   string
	ExpectedPreviousDigest string
	ProposalGeneration     string
	Authority              string
	Actor                  string
}

type SetWaiverRequest struct {
	WaiverID     string
	WorkspaceID  string
	PolicyDigest string
	RuleID       string
	Phase        core.Phase
	Generation   string
	CheckpointID string
	Authority    string
	Actor        string
	Reason       string
	ExpiresAt    time.Time
	ExpiresPhase core.Phase
}

type RevokeWaiverRequest struct {
	WaiverID    string
	WorkspaceID string
	Authority   string
	Actor       string
}

type WaiverScope struct {
	WorkspaceID  string
	Phase        core.Phase
	Generation   string
	CheckpointID string
}

func NewPolicyService(workspaces WorkspaceLookup, loader PolicyLoader, policies PolicyAuthorityStore, waivers WaiverAuthorityStore, snapshots SourceSnapshotter, projects ProjectInspector, commands ProjectCommandResolver) *PolicyService {
	return &PolicyService{workspaces: workspaces, loader: loader, policies: policies, waivers: waivers, snapshots: snapshots, projects: projects, commands: commands, now: func() time.Time { return time.Now().UTC() }}
}

func (s *PolicyService) Activate(ctx context.Context, req ActivateRequest) (core.ActivationWriteResult, error) {
	ws, err := s.workspace(ctx, req.WorkspaceID)
	if err != nil {
		return core.ActivationWriteResult{}, err
	}
	intent := core.PolicyActivationIntent{ActivationID: req.ActivationID, RepositoryID: string(ws.RepositoryID), PreviousEffectiveDigest: req.ExpectedPreviousDigest, ProposedPolicyDigest: req.ProposedPolicyDigest, ProposalGeneration: req.ProposalGeneration, Authority: req.Authority, Actor: req.Actor}
	if err := intent.Validate(); err != nil {
		return core.ActivationWriteResult{}, err
	}
	if req.Authority != AuthorityExplicitCaller {
		return core.ActivationWriteResult{}, fmt.Errorf("unsupported verification activation authority")
	}

	// Retry identity is checked before proposal/source/project observations.
	if existing, found, err := s.policies.FindActivation(ctx, ws.RepositoryID, req.ActivationID); err != nil {
		return core.ActivationWriteResult{}, err
	} else if found {
		fp, _ := core.ActivationIntentFingerprint(intent)
		if existing.IntentFingerprint != fp {
			return core.ActivationWriteResult{}, fmt.Errorf("activation id conflicts with different intent")
		}
		commit := core.PolicyActivationCommit{Intent: intent, ProposalOrigin: existing.ProposalOrigin, ProfileOrigin: existing.ProfileOrigin, ActivationGeneration: existing.ActivationGeneration}
		return s.policies.ActivatePolicyCAS(ctx, commit)
	}

	if s.loader == nil || s.snapshots == nil {
		return core.ActivationWriteResult{}, fmt.Errorf("verification activation dependencies unavailable")
	}
	load := s.loader.Load(ctx, ws)
	if load.State != PolicyLoadValid || load.Proposal == nil {
		return core.ActivationWriteResult{}, fmt.Errorf("current verification policy proposal unavailable")
	}
	proposal := *load.Proposal
	if proposal.RepositoryID != string(ws.RepositoryID) || proposal.Digest != req.ProposedPolicyDigest || proposal.Origin != core.ProposalRepositoryAuthored {
		return core.ActivationWriteResult{}, fmt.Errorf("current verification policy proposal mismatch")
	}
	if err := validateProjectRequirements(ctx, req.WorkspaceID, proposal.Content, s.projects, s.commands); err != nil {
		return core.ActivationWriteResult{}, err
	}

	fresh := s.snapshots.ObserveFresh(ctx, ws.Root)
	if fresh.Quality != workspace.QualityFresh || fresh.Generation == "" {
		return core.ActivationWriteResult{}, fmt.Errorf("fresh activation source generation unavailable")
	}
	if err := fresh.Validate(); err != nil {
		return core.ActivationWriteResult{}, fmt.Errorf("invalid fresh activation snapshot: %w", err)
	}
	if fresh.RepositoryID != ws.RepositoryID || fresh.WorkspaceID != ws.ID {
		return core.ActivationWriteResult{}, fmt.Errorf("activation snapshot workspace mismatch")
	}
	if fresh.Generation == req.ProposalGeneration {
		return core.ActivationWriteResult{}, fmt.Errorf("policy cannot activate for introducing generation")
	}

	snapshot := core.PolicySnapshot{RepositoryID: proposal.RepositoryID, Digest: proposal.Digest, Content: proposal.Content}
	if _, err := s.policies.PutPolicySnapshot(ctx, snapshot); err != nil {
		return core.ActivationWriteResult{}, err
	}
	return s.policies.ActivatePolicyCAS(ctx, core.PolicyActivationCommit{Intent: intent, ProposalOrigin: proposal.Origin, ProfileOrigin: proposal.ProfileOrigin, ActivationGeneration: fresh.Generation})
}

func validateProjectRequirements(ctx context.Context, workspaceID string, content core.PolicyContent, inspector ProjectInspector, resolver ProjectCommandResolver) error {
	var requirements []core.EvidenceRequirement
	for _, rule := range content.Rules {
		for _, req := range rule.Evidence {
			if req.ProjectCommandID != "" {
				requirements = append(requirements, req)
			}
		}
	}
	if len(requirements) == 0 {
		return nil
	}
	if inspector == nil || resolver == nil {
		return fmt.Errorf("project command verification binding unavailable")
	}
	inspection, err := inspector.Inspect(ctx, workspaceID)
	if err != nil {
		return err
	}
	if (inspection.Status != project.StatusValid && inspection.Status != project.StatusReviewDue) || inspection.Manifest == nil || inspection.Manifest.SchemaVersion != project.ManifestSchemaV2 {
		return fmt.Errorf("project manifest is not eligible for verification binding")
	}
	for _, req := range requirements {
		if _, err := resolver.Resolve(ctx, workspaceID, req.ProjectCommandID, req.Params); err != nil {
			return fmt.Errorf("verification project command %q unresolved: %w", req.ProjectCommandID, err)
		}
	}
	return nil
}

func (s *PolicyService) SetWaiver(ctx context.Context, req SetWaiverRequest) (core.WaiverWriteResult, error) {
	ws, err := s.workspace(ctx, req.WorkspaceID)
	if err != nil {
		return core.WaiverWriteResult{}, err
	}
	if req.Authority != AuthorityExplicitCaller {
		return core.WaiverWriteResult{}, fmt.Errorf("unsupported verification waiver authority")
	}
	current, found, err := s.policies.CurrentActivation(ctx, ws.RepositoryID)
	if err != nil {
		return core.WaiverWriteResult{}, err
	}
	if !found || current.ProposedPolicyDigest != req.PolicyDigest {
		return core.WaiverWriteResult{}, fmt.Errorf("waiver policy is not current")
	}
	snapshot, found, err := s.policies.LoadPolicySnapshot(ctx, ws.RepositoryID, req.PolicyDigest)
	if err != nil {
		return core.WaiverWriteResult{}, err
	}
	if !found {
		return core.WaiverWriteResult{}, fmt.Errorf("waiver policy snapshot unavailable")
	}
	rule, ok := findPolicyRule(snapshot.Content, req.RuleID)
	if !ok || !containsPhase(rule.Phases, req.Phase) {
		return core.WaiverWriteResult{}, fmt.Errorf("waiver rule/phase not present in current policy")
	}
	intent := core.VerificationWaiverIntent{WaiverID: req.WaiverID, RepositoryID: string(ws.RepositoryID), PolicyDigest: req.PolicyDigest, RuleID: req.RuleID, Phase: req.Phase, Generation: req.Generation, CheckpointID: req.CheckpointID, Authority: req.Authority, Actor: req.Actor, Reason: req.Reason, ExpiresAt: req.ExpiresAt, ExpiresPhase: req.ExpiresPhase}
	if err := intent.Validate(); err != nil {
		return core.WaiverWriteResult{}, err
	}
	if !req.ExpiresAt.IsZero() && !s.now().Before(req.ExpiresAt) {
		return core.WaiverWriteResult{}, fmt.Errorf("waiver already expired")
	}
	return s.waivers.PutWaiver(ctx, intent)
}

func (s *PolicyService) RevokeWaiver(ctx context.Context, req RevokeWaiverRequest) (core.RevocationWriteResult, error) {
	ws, err := s.workspace(ctx, req.WorkspaceID)
	if err != nil {
		return core.RevocationWriteResult{}, err
	}
	if req.Authority != AuthorityExplicitCaller {
		return core.RevocationWriteResult{}, fmt.Errorf("unsupported verification waiver authority")
	}
	if _, found, err := s.waivers.FindWaiver(ctx, ws.RepositoryID, req.WaiverID); err != nil {
		return core.RevocationWriteResult{}, err
	} else if !found {
		return core.RevocationWriteResult{}, fmt.Errorf("waiver not found")
	}
	return s.waivers.PutWaiverRevocation(ctx, core.WaiverRevocationIntent{RepositoryID: string(ws.RepositoryID), WaiverID: req.WaiverID, Authority: req.Authority, Actor: req.Actor})
}

func (s *PolicyService) ActiveWaivers(ctx context.Context, scope WaiverScope) ([]core.VerificationWaiver, error) {
	ws, err := s.workspace(ctx, scope.WorkspaceID)
	if err != nil {
		return nil, err
	}
	current, found, err := s.policies.CurrentActivation(ctx, ws.RepositoryID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	waivers, revocations, err := s.waivers.ListWaivers(ctx, ws.RepositoryID)
	if err != nil {
		return nil, err
	}
	revoked := map[string]bool{}
	for _, r := range revocations {
		revoked[r.WaiverID] = true
	}
	now := s.now()
	out := make([]core.VerificationWaiver, 0, len(waivers))
	for _, w := range waivers {
		if revoked[w.WaiverID] || w.PolicyDigest != current.ProposedPolicyDigest || w.Phase != scope.Phase {
			continue
		}
		if w.Generation != "" && w.Generation != scope.Generation {
			continue
		}
		if w.CheckpointID != "" && w.CheckpointID != scope.CheckpointID {
			continue
		}
		if !w.ExpiresAt.IsZero() && !now.Before(w.ExpiresAt) {
			continue
		}
		if w.ExpiresPhase != "" && w.ExpiresPhase == scope.Phase {
			continue
		}
		out = append(out, w)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].WaiverID < out[j].WaiverID })
	return out, nil
}

func findPolicyRule(content core.PolicyContent, id string) (core.Rule, bool) {
	for _, r := range content.Rules {
		if r.ID == id {
			return r, true
		}
	}
	return core.Rule{}, false
}
func containsPhase(phases []core.Phase, want core.Phase) bool {
	for _, p := range phases {
		if p == want {
			return true
		}
	}
	return false
}

func (s *PolicyService) workspace(ctx context.Context, workspaceID string) (workspace.Workspace, error) {
	if s == nil || s.workspaces == nil || s.policies == nil || s.waivers == nil {
		return workspace.Workspace{}, fmt.Errorf("verification policy service unavailable")
	}
	id, err := workspace.ParseWorkspaceID(workspaceID)
	if err != nil {
		return workspace.Workspace{}, err
	}
	values, err := s.workspaces.ListWorkspaces(ctx)
	if err != nil {
		return workspace.Workspace{}, err
	}
	for _, ws := range values {
		if ws.ID == id {
			return ws, nil
		}
	}
	return workspace.Workspace{}, fmt.Errorf("workspace not found")
}
