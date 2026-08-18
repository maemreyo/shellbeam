package verification

import (
	"context"
	"fmt"

	core "github.com/maemreyo/shellbeam/internal/core/verification"
	"github.com/maemreyo/shellbeam/internal/core/workspace"
)

type PolicyState string

const (
	PolicyStateAbsent          PolicyState = "absent"
	PolicyStateEffective       PolicyState = "effective"
	PolicyStateProposalPending PolicyState = "proposal_pending"
	PolicyStateInvalid         PolicyState = "invalid"
	PolicyStateUnsupported     PolicyState = "unsupported"
)

type InspectRequest struct {
	WorkspaceID string     `json:"workspace_id"`
	ActivityID  string     `json:"activity_id,omitempty"`
	Phase       core.Phase `json:"phase"`
}

type Inspection struct {
	SchemaVersion    int                           `json:"schema_version"`
	Phase            core.Phase                    `json:"phase"`
	RepositoryID     string                        `json:"repository_id"`
	WorkspaceID      string                        `json:"workspace_id"`
	SourceGeneration string                        `json:"source_generation"`
	EffectivePolicy  *core.PolicySummary           `json:"effective_policy,omitempty"`
	ProposedPolicy   *core.PolicyProposalSummary   `json:"proposed_policy,omitempty"`
	PolicyState      PolicyState                   `json:"policy_state"`
	Affected         core.AffectedSurfaceSummary   `json:"affected_surface"`
	Obligations      []core.VerificationObligation `json:"obligations"`
	PolicyGaps       []core.PolicyGap              `json:"policy_gaps,omitempty"`
}

type PreviewPolicyRequest struct {
	WorkspaceID string `json:"workspace_id"`
	Profile     string `json:"profile,omitempty"`
}

type PolicyPreview struct {
	State        PolicyLoadState      `json:"state"`
	Proposal     *core.PolicyProposal `json:"proposal,omitempty"`
	RenderedTOML string               `json:"rendered_toml,omitempty"`
	Advisories   []string             `json:"advisories,omitempty"`
	RawDigest    string               `json:"raw_digest,omitempty"`
	Code         string               `json:"code,omitempty"`
}

type InspectionService struct {
	workspaces  WorkspaceInspector
	loader      PolicyLoader
	effective   EffectivePolicyStore
	affected    AffectedDeriver
	obligations ObligationDeriver
	waivers     ActiveWaiverReader
	projects    ProjectInspector
	starters    StarterPolicyPreviewer
}

func NewInspectionService(workspaces WorkspaceInspector, loader PolicyLoader, effective EffectivePolicyStore, affected AffectedDeriver, obligations ObligationDeriver, waivers ActiveWaiverReader, projects ProjectInspector, starters StarterPolicyPreviewer) *InspectionService {
	return &InspectionService{workspaces: workspaces, loader: loader, effective: effective, affected: affected, obligations: obligations, waivers: waivers, projects: projects, starters: starters}
}

func (s *InspectionService) Inspect(ctx context.Context, req InspectRequest) (Inspection, error) {
	if s == nil || s.workspaces == nil || s.loader == nil || s.effective == nil || s.affected == nil {
		return Inspection{}, fmt.Errorf("verification inspection unavailable")
	}
	if err := req.Phase.Validate(); err != nil {
		return Inspection{}, err
	}
	ws, err := s.workspaces.Inspect(ctx, req.WorkspaceID)
	if err != nil {
		return Inspection{}, err
	}
	affected, err := s.affected.Derive(ctx, AffectedRequest{WorkspaceID: req.WorkspaceID, ActivityID: req.ActivityID})
	if err != nil {
		return Inspection{}, err
	}
	load := s.loader.Load(ctx, ws)
	materialized, effectiveSummary, found, err := s.materializedPolicy(ctx, ws.RepositoryID)
	if err != nil {
		return Inspection{}, err
	}
	out := Inspection{SchemaVersion: 1, Phase: req.Phase, RepositoryID: string(ws.RepositoryID), WorkspaceID: string(ws.ID), SourceGeneration: affected.Surface.SourceGeneration, EffectivePolicy: effectiveSummary, ProposedPolicy: proposalSummary(load, found, materialized), PolicyState: resolvePolicyState(load, found, materialized), Obligations: []core.VerificationObligation{}}
	if !found {
		out.Affected = affected.Surface.Summary()
		return out, nil
	}
	classified, err := ApplyEffectiveClassifications(ClassificationProjectionRequest{BaseSurface: affected.Surface, EffectivePolicy: materialized})
	if err != nil {
		return Inspection{}, err
	}
	out.Affected = classified.Summary()
	if s.obligations == nil || s.waivers == nil {
		return Inspection{}, fmt.Errorf("verification obligation projection unavailable")
	}
	waivers, err := s.waivers.ActiveWaivers(ctx, WaiverScope{WorkspaceID: req.WorkspaceID, Phase: req.Phase, Generation: classified.SourceGeneration})
	if err != nil {
		return Inspection{}, err
	}
	derived, err := s.obligations.Derive(ctx, ObligationRequest{WorkspaceID: req.WorkspaceID, Phase: req.Phase, Policy: materialized, Surface: classified, ActiveWaivers: waivers})
	if err != nil {
		return Inspection{}, err
	}
	out.Obligations = derived.Obligations
	out.PolicyGaps = derived.PolicyGaps
	return out, nil
}

func (s *InspectionService) PreviewPolicy(ctx context.Context, req PreviewPolicyRequest) (PolicyPreview, error) {
	if s == nil || s.workspaces == nil || s.loader == nil {
		return PolicyPreview{}, fmt.Errorf("verification policy preview unavailable")
	}
	ws, err := s.workspaces.Inspect(ctx, req.WorkspaceID)
	if err != nil {
		return PolicyPreview{}, err
	}
	if req.Profile == "" {
		load := s.loader.Load(ctx, ws)
		return PolicyPreview{State: load.State, Proposal: load.Proposal, RawDigest: load.RawDigest, Code: load.Code}, nil
	}
	if s.projects == nil || s.starters == nil {
		return PolicyPreview{}, fmt.Errorf("verification starter preview unavailable")
	}
	inspection, err := s.projects.Inspect(ctx, req.WorkspaceID)
	if err != nil {
		return PolicyPreview{}, err
	}
	preview, err := s.starters.Preview(ctx, req.Profile, string(ws.RepositoryID), inspection.Manifest)
	if err != nil {
		return PolicyPreview{}, err
	}
	preview.State = PolicyLoadValid
	return preview, nil
}

func (s *InspectionService) materializedPolicy(ctx context.Context, repositoryID workspace.RepositoryID) (core.MaterializedPolicy, *core.PolicySummary, bool, error) {
	activation, found, err := s.effective.CurrentActivation(ctx, repositoryID)
	if err != nil || !found {
		return core.MaterializedPolicy{}, nil, false, err
	}
	snapshot, found, err := s.effective.LoadPolicySnapshot(ctx, repositoryID, activation.ProposedPolicyDigest)
	if err != nil {
		return core.MaterializedPolicy{}, nil, false, err
	}
	if !found || snapshot.RepositoryID != string(repositoryID) || snapshot.Digest != activation.ProposedPolicyDigest {
		return core.MaterializedPolicy{}, nil, false, fmt.Errorf("effective verification policy snapshot unavailable")
	}
	digest, err := core.PolicyDigest(snapshot.Content)
	if err != nil || digest != snapshot.Digest {
		return core.MaterializedPolicy{}, nil, false, fmt.Errorf("effective verification policy snapshot invalid")
	}
	materialized := core.MaterializedPolicy{
		Snapshot: snapshot, Source: core.PolicyRepositoryAuthored, ProfileOrigin: activation.ProfileOrigin,
		ApprovalRef: activation.ActivationID, ApprovalAuthority: activation.Authority, ApprovedAt: activation.ActivatedAt,
	}
	summary := core.PolicySummary{
		PolicyID: snapshot.Content.PolicyID, Digest: snapshot.Digest, Source: materialized.Source,
		ProfileOrigin: materialized.ProfileOrigin, ApprovalRef: materialized.ApprovalRef,
		ApprovalAuthority: materialized.ApprovalAuthority, ApprovedAt: materialized.ApprovedAt,
	}
	return materialized, &summary, true, nil
}

func resolvePolicyState(load PolicyLoadResult, effective bool, materialized core.MaterializedPolicy) PolicyState {
	switch load.State {
	case PolicyLoadInvalid:
		return PolicyStateInvalid
	case PolicyLoadUnsupported:
		return PolicyStateUnsupported
	case PolicyLoadValid:
		if load.Proposal == nil {
			return PolicyStateInvalid
		}
		if !effective || load.Proposal.Digest != materialized.Snapshot.Digest {
			return PolicyStateProposalPending
		}
		return PolicyStateEffective
	case PolicyLoadAbsent:
		if effective {
			return PolicyStateEffective
		}
		return PolicyStateAbsent
	default:
		return PolicyStateInvalid
	}
}

func proposalSummary(load PolicyLoadResult, effective bool, materialized core.MaterializedPolicy) *core.PolicyProposalSummary {
	if load.State != PolicyLoadValid || load.Proposal == nil {
		return nil
	}
	if effective && load.Proposal.Digest == materialized.Snapshot.Digest {
		return nil
	}
	proposal := load.Proposal
	return &core.PolicyProposalSummary{PolicyID: proposal.Content.PolicyID, Digest: proposal.Digest, Origin: proposal.Origin, ProfileOrigin: proposal.ProfileOrigin}
}
