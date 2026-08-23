package decisionprotocol

import (
	"context"
	"fmt"

	core "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
	"github.com/maemreyo/shellbeam/internal/core/workspace"
)

type CreateEpisodeRequest struct {
	EpisodeID             core.EpisodeID
	Kind                  core.EpisodeKind
	RepositoryID          string
	WorkspaceID           string
	PredecessorEpisodeID  core.EpisodeID
	ExpectedPolicyDigest  string
	ExpectedActivationRef string
	ActorRef              string
}

func (s *Service) CreateEpisode(ctx context.Context, req CreateEpisodeRequest) (core.DecisionProjection, error) {
	if s == nil || s.policies == nil || s.mutations == nil || s.ledger == nil || s.workspaces == nil || s.snapshots == nil {
		return core.DecisionProjection{}, fmt.Errorf("decision episode dependencies unavailable")
	}
	if _, err := core.ParseEpisodeID(string(req.EpisodeID)); err != nil || req.Kind.Validate() != nil || req.RepositoryID == "" || req.WorkspaceID == "" || req.ActorRef == "" {
		return core.DecisionProjection{}, fmt.Errorf("invalid decision episode request")
	}
	if req.PredecessorEpisodeID != "" {
		if _, err := core.ParseEpisodeID(string(req.PredecessorEpisodeID)); err != nil {
			return core.DecisionProjection{}, fmt.Errorf("invalid predecessor episode id")
		}
	}

	policy, activation, ok, err := s.policies.CurrentEffectivePolicy(ctx, req.RepositoryID, req.Kind)
	if err != nil {
		return core.DecisionProjection{}, err
	}
	if !ok {
		return core.DecisionProjection{}, core.NewReasonError(core.ReasonPolicyConflict, "no current effective applicable decision policy")
	}
	if req.ExpectedPolicyDigest != "" && req.ExpectedPolicyDigest != policy.PolicyDigest {
		return core.DecisionProjection{}, core.NewReasonError(core.ReasonPolicyConflict, "current policy digest differs from expected guard")
	}
	if req.ExpectedActivationRef != "" && req.ExpectedActivationRef != activation.ActivationID {
		return core.DecisionProjection{}, core.NewReasonError(core.ReasonPolicyConflict, "current activation differs from expected guard")
	}

	ws, snap, err := s.currentEpisodeSource(ctx, req.WorkspaceID)
	if err != nil {
		return core.DecisionProjection{}, err
	}
	if string(ws.RepositoryID) != req.RepositoryID || string(ws.ID) != req.WorkspaceID {
		return core.DecisionProjection{}, fmt.Errorf("decision episode workspace identity mismatch")
	}
	episode := core.Episode{
		SchemaVersion:        1,
		EpisodeID:            req.EpisodeID,
		EpisodeKind:          req.Kind,
		RepositoryID:         req.RepositoryID,
		WorkspaceID:          req.WorkspaceID,
		PredecessorEpisodeID: req.PredecessorEpisodeID,
		Baseline:             core.EpisodeBaseline{SourceGeneration: snap.Generation},
		PolicyBinding:        core.EpisodePolicyBinding{PolicyID: policy.Content.PolicyID, PolicyDigest: policy.PolicyDigest, ActivationRef: activation.ActivationID},
		CreatedByActorRef:    req.ActorRef,
		CreatedAt:            s.now().UTC(),
	}
	if _, _, err := s.mutations.CreateEpisode(ctx, episode); err != nil {
		return core.DecisionProjection{}, err
	}
	return s.Inspect(ctx, req.EpisodeID, "")
}

func (s *Service) currentEpisodeSource(ctx context.Context, workspaceID string) (workspace.Workspace, workspace.FastSnapshot, error) {
	ws, err := s.workspaces.Inspect(ctx, workspaceID)
	if err != nil {
		return workspace.Workspace{}, workspace.FastSnapshot{}, err
	}
	if err := ws.Validate(); err != nil {
		return workspace.Workspace{}, workspace.FastSnapshot{}, fmt.Errorf("invalid decision episode workspace: %w", err)
	}
	snap := s.snapshots.ObserveFresh(ctx, ws.Root)
	if snap.Quality != workspace.QualityFresh || snap.Generation == "" {
		return workspace.Workspace{}, workspace.FastSnapshot{}, fmt.Errorf("fresh decision episode source generation unavailable")
	}
	if err := snap.Validate(); err != nil {
		return workspace.Workspace{}, workspace.FastSnapshot{}, fmt.Errorf("invalid decision episode source snapshot: %w", err)
	}
	if snap.RepositoryID != ws.RepositoryID || snap.WorkspaceID != ws.ID {
		return workspace.Workspace{}, workspace.FastSnapshot{}, fmt.Errorf("decision episode source snapshot workspace mismatch")
	}
	return ws, snap, nil
}
