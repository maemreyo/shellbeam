package decisionprotocol

import (
	"context"
	"fmt"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
)

type ProposeSelectionRequest struct {
	EpisodeID   core.EpisodeID
	CandidateID core.CandidateID
	ActorRef    string
	Rationale   string
}

type CommitSelectionRequest struct {
	EpisodeID                core.EpisodeID
	CandidateID              core.CandidateID
	ActorRef                 string
	ExpectedPolicyDigest     string
	ExpectedProjectionDigest string
	OverrideRef              string
	IdempotencyKey           string
}

type CloseUnresolvedRequest struct {
	EpisodeID            core.EpisodeID
	ActorRef             string
	ProjectionDigest     string
	Reason               string
	UnresolvedDimensions []string
}

func (s *Service) ProposeSelection(ctx context.Context, req ProposeSelectionRequest) (core.SelectionProposal, error) {
	if s == nil || s.selections == nil {
		return core.SelectionProposal{}, fmt.Errorf("decision selection store unavailable")
	}
	projection, err := s.Project(ctx, req.EpisodeID, req.CandidateID)
	if err != nil {
		return core.SelectionProposal{}, err
	}
	if projection.EpisodeState != core.EpisodeOpen {
		return core.SelectionProposal{}, core.NewReasonError(core.ReasonEpisodeTerminalConflict, "episode already terminal")
	}
	proposal := core.SelectionProposal{
		ProposalID: semanticRecordID("proposal", string(req.EpisodeID), string(req.CandidateID), req.ActorRef, req.Rationale, s.now().UTC().Format(time.RFC3339Nano)),
		EpisodeID:  req.EpisodeID, CandidateID: req.CandidateID, ActorRef: req.ActorRef, Rationale: req.Rationale, CreatedAt: s.now().UTC(),
	}
	if err := proposal.Validate(); err != nil {
		return core.SelectionProposal{}, err
	}
	if _, _, err := s.selections.RecordSelectionProposal(ctx, proposal); err != nil {
		return core.SelectionProposal{}, err
	}
	return proposal, nil
}

func (s *Service) CommitSelection(ctx context.Context, req CommitSelectionRequest) (core.SelectionCommit, error) {
	if s == nil || s.selections == nil || s.mutations == nil {
		return core.SelectionCommit{}, fmt.Errorf("decision selection dependencies unavailable")
	}
	episode, found, err := s.mutations.FindEpisode(ctx, req.EpisodeID)
	if err != nil {
		return core.SelectionCommit{}, err
	}
	if !found {
		return core.SelectionCommit{}, ErrEpisodeNotFound
	}
	if req.ExpectedPolicyDigest != episode.PolicyBinding.PolicyDigest {
		return core.SelectionCommit{}, core.NewReasonError(core.ReasonPolicyConflict, "expected policy digest does not match episode binding")
	}
	intent := core.SelectionCommitIntent{EpisodeID: req.EpisodeID, CandidateID: req.CandidateID, ActorRef: req.ActorRef, PolicyDigest: episode.PolicyBinding.PolicyDigest, ProjectionDigest: req.ExpectedProjectionDigest, SourceGeneration: episode.Baseline.SourceGeneration, Override: req.OverrideRef != "", OverrideRef: req.OverrideRef}
	fingerprint, err := core.SelectionIntentFingerprint(intent)
	if err != nil {
		return core.SelectionCommit{}, err
	}
	if existing, ok, err := s.selections.FindSelectionCommitByIdempotencyKey(ctx, req.IdempotencyKey); err != nil {
		return core.SelectionCommit{}, err
	} else if ok {
		if existing.SemanticIntentFingerprint != fingerprint {
			return core.SelectionCommit{}, core.NewReasonError(core.ReasonIdempotencyConflict, "idempotency key reused for different selection intent")
		}
		return existing, nil
	}
	projection, err := s.Project(ctx, req.EpisodeID, req.CandidateID)
	if err != nil {
		return core.SelectionCommit{}, err
	}
	if projection.EpisodeState != core.EpisodeOpen {
		return core.SelectionCommit{}, core.NewReasonError(core.ReasonEpisodeTerminalConflict, "episode already terminal")
	}
	if !projection.SourceCompatible {
		return core.SelectionCommit{}, core.NewReasonError(core.ReasonStaleEpisodeSourceGeneration, "episode source generation is stale")
	}
	if req.ExpectedProjectionDigest != projection.ProjectionDigest {
		return core.SelectionCommit{}, core.NewReasonError(core.ReasonProjectionConflict, "expected projection digest is stale")
	}
	if !candidateActive(projection.Candidates, req.CandidateID) {
		return core.SelectionCommit{}, core.NewReasonError(core.ReasonTerminalSelectionConflict, "candidate is not active")
	}
	if err := requireSettledLinkedExperiments(projection.Experiments); err != nil {
		return core.SelectionCommit{}, err
	}
	var authorization *core.OverrideAuthorization
	if req.OverrideRef == "" {
		switch projection.Protocol.Gate {
		case core.GateBlocked:
			return core.SelectionCommit{}, core.NewReasonError(core.ReasonProtocolBlocked, "decision protocol gate blocked")
		case core.GateIndeterminate:
			return core.SelectionCommit{}, core.NewReasonError(core.ReasonProtocolIndeterminate, "decision protocol gate indeterminate")
		case core.GateClear:
		default:
			return core.SelectionCommit{}, core.NewReasonError(core.ReasonProtocolIndeterminate, "decision protocol gate unavailable")
		}
	} else {
		authorization, err = s.authorizeOverrideCommit(ctx, episode, projection, req.OverrideRef)
		if err != nil {
			return core.SelectionCommit{}, err
		}
	}
	commit := core.SelectionCommit{CommitID: semanticRecordID("commit", string(req.EpisodeID), req.IdempotencyKey, fingerprint), EpisodeID: req.EpisodeID, CandidateID: req.CandidateID, PolicyDigest: episode.PolicyBinding.PolicyDigest, ProjectionDigest: projection.ProjectionDigest, SourceGeneration: episode.Baseline.SourceGeneration, OverrideRef: req.OverrideRef, OverrideAuthorization: authorization, IdempotencyKey: req.IdempotencyKey, SemanticIntentFingerprint: fingerprint, CommittedByActorRef: req.ActorRef, CommittedAt: s.now().UTC()}
	if err := commit.Validate(); err != nil {
		return core.SelectionCommit{}, err
	}
	stored, _, err := s.selections.CommitSelectionCAS(ctx, intent, commit)
	if err != nil {
		return core.SelectionCommit{}, err
	}
	return stored, nil
}

func (s *Service) CloseUnresolved(ctx context.Context, req CloseUnresolvedRequest) (core.DecisionClosure, error) {
	if s == nil || s.selections == nil || s.mutations == nil {
		return core.DecisionClosure{}, fmt.Errorf("decision selection dependencies unavailable")
	}
	_, found, err := s.mutations.FindEpisode(ctx, req.EpisodeID)
	if err != nil {
		return core.DecisionClosure{}, err
	}
	if !found {
		return core.DecisionClosure{}, ErrEpisodeNotFound
	}
	projection, err := s.Project(ctx, req.EpisodeID, "")
	if err != nil {
		return core.DecisionClosure{}, err
	}
	if projection.EpisodeState != core.EpisodeOpen {
		return core.DecisionClosure{}, core.NewReasonError(core.ReasonEpisodeTerminalConflict, "episode already terminal")
	}
	if req.ProjectionDigest != projection.ProjectionDigest {
		return core.DecisionClosure{}, core.NewReasonError(core.ReasonProjectionConflict, "closure projection digest is stale")
	}
	closure := core.DecisionClosure{EpisodeID: req.EpisodeID, Kind: core.ClosureUnresolved, Reason: req.Reason, UnresolvedDimensions: append([]string(nil), req.UnresolvedDimensions...), ActorRef: req.ActorRef, ProjectionDigest: req.ProjectionDigest, ClosedAt: s.now().UTC()}
	if err := closure.Validate(); err != nil {
		return core.DecisionClosure{}, err
	}
	stored, _, err := s.selections.CloseEpisodeCAS(ctx, closure)
	if err != nil {
		return core.DecisionClosure{}, err
	}
	return stored, nil
}

func requireSettledLinkedExperiments(experiments []core.ExperimentProjection) error {
	for _, experiment := range experiments {
		if experiment.ObservationState == core.ObservationSettling {
			return core.NewReasonError(core.ReasonObservationNotSettled, "linked experiment observation is still settling")
		}
	}
	return nil
}
