package decisionprotocol

import (
	"context"
	"fmt"
	"reflect"

	core "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
)

// NewRuntimeService composes the bounded Decision Protocol facade from existing
// durable stores and read-side observation services. Execution ownership is
// intentionally absent: experiment execution remains an ordinary daemon start.
func NewRuntimeService(policies PolicyStore, generation ActivationGenerationSource, deps EpisodeDependencies) (*Service, error) {
	if policies == nil || generation == nil || deps.Mutations == nil || deps.Experiments == nil || deps.Ledger == nil || deps.Workspaces == nil || deps.Snapshots == nil || deps.Receipts == nil || deps.Structured == nil || deps.Verification == nil || deps.Assessments == nil || deps.Selections == nil || deps.Authorities == nil || deps.AuthorityResolver == nil {
		return nil, fmt.Errorf("decision protocol runtime dependencies unavailable")
	}
	return NewService(policies, generation, deps), nil
}

type CreateCandidateInputRequest struct {
	EpisodeID     core.EpisodeID
	CandidateID   core.CandidateID
	SemanticClaim string
	CandidateKind string
	ActorRef      string
}

func (s *Service) CreateCandidateInput(ctx context.Context, req CreateCandidateInputRequest) (core.DecisionProjection, error) {
	if s == nil || s.mutations == nil {
		return core.DecisionProjection{}, fmt.Errorf("decision candidate store unavailable")
	}
	if existing, found, err := s.mutations.FindCandidate(ctx, req.CandidateID); err != nil {
		return core.DecisionProjection{}, err
	} else if found {
		if existing.EpisodeID != req.EpisodeID || existing.SemanticClaim != req.SemanticClaim || existing.CandidateKind != req.CandidateKind || existing.DeclaredByActorRef != req.ActorRef || existing.RevisesCandidateID != "" {
			return core.DecisionProjection{}, fmt.Errorf("candidate id conflicts with different semantic intent")
		}
		return s.Inspect(ctx, existing.EpisodeID, existing.CandidateID)
	}
	candidate := core.Candidate{CandidateID: req.CandidateID, EpisodeID: req.EpisodeID, SemanticClaim: req.SemanticClaim, CandidateKind: req.CandidateKind, DeclaredByActorRef: req.ActorRef, DeclaredAt: s.now().UTC()}
	return s.CreateCandidate(ctx, candidate)
}

type ReviseCandidateInputRequest struct {
	EpisodeID         core.EpisodeID
	ParentCandidateID core.CandidateID
	CandidateID       core.CandidateID
	SemanticClaim     string
	CandidateKind     string
	ActorRef          string
}

func (s *Service) ReviseCandidateInput(ctx context.Context, req ReviseCandidateInputRequest) (core.DecisionProjection, error) {
	if s == nil || s.mutations == nil {
		return core.DecisionProjection{}, fmt.Errorf("decision candidate store unavailable")
	}
	if existing, found, err := s.mutations.FindCandidate(ctx, req.CandidateID); err != nil {
		return core.DecisionProjection{}, err
	} else if found {
		if existing.EpisodeID != req.EpisodeID || existing.SemanticClaim != req.SemanticClaim || existing.CandidateKind != req.CandidateKind || existing.DeclaredByActorRef != req.ActorRef || existing.RevisesCandidateID != req.ParentCandidateID {
			return core.DecisionProjection{}, fmt.Errorf("candidate revision id conflicts with different semantic intent")
		}
		return s.Inspect(ctx, existing.EpisodeID, existing.CandidateID)
	}
	child := core.Candidate{CandidateID: req.CandidateID, EpisodeID: req.EpisodeID, SemanticClaim: req.SemanticClaim, CandidateKind: req.CandidateKind, RevisesCandidateID: req.ParentCandidateID, DeclaredByActorRef: req.ActorRef, DeclaredAt: s.now().UTC()}
	return s.ReviseCandidate(ctx, req.ParentCandidateID, child)
}

type DefineExperimentInputRequest struct {
	EpisodeID    core.EpisodeID
	ExperimentID core.ExperimentID
	ActorRef     string
}

func (s *Service) DefineExperimentInput(ctx context.Context, req DefineExperimentInputRequest) (core.DecisionProjection, error) {
	if s == nil || s.experiments == nil {
		return core.DecisionProjection{}, fmt.Errorf("decision experiment store unavailable")
	}
	if existing, found, err := s.experiments.FindExperiment(ctx, req.ExperimentID); err != nil {
		return core.DecisionProjection{}, err
	} else if found {
		if existing.EpisodeID != req.EpisodeID || existing.DeclaredByActorRef != req.ActorRef {
			return core.DecisionProjection{}, fmt.Errorf("experiment id conflicts with different semantic intent")
		}
		return s.Inspect(ctx, existing.EpisodeID, "")
	}
	experiment := core.Experiment{SchemaVersion: 1, ExperimentID: req.ExperimentID, EpisodeID: req.EpisodeID, DeclaredByActorRef: req.ActorRef, DeclaredAt: s.now().UTC()}
	return s.DefineExperiment(ctx, experiment)
}

type BindPredictionInputRequest struct {
	EpisodeID    core.EpisodeID
	ExperimentID core.ExperimentID
	PredictionID core.PredictionID
	CandidateID  core.CandidateID
	Role         core.PredictionRole
	Predicate    core.ObservationPredicate
}

func (s *Service) BindPredictionInput(ctx context.Context, req BindPredictionInputRequest) (core.DecisionProjection, error) {
	if s == nil || s.ledger == nil || s.mutations == nil || s.experiments == nil {
		return core.DecisionProjection{}, fmt.Errorf("decision prediction dependencies unavailable")
	}
	cut, err := s.ledger.CurrentHighWater(ctx)
	if err != nil {
		return core.DecisionProjection{}, err
	}
	records, err := s.ledger.ListEpisodeRecords(ctx, req.EpisodeID, cut)
	if err != nil {
		return core.DecisionProjection{}, err
	}
	for _, existing := range predictionsForExperiment(records, req.ExperimentID) {
		if existing.PredictionID != req.PredictionID {
			continue
		}
		if existing.EpisodeID != req.EpisodeID || existing.ExperimentID != req.ExperimentID || existing.CandidateID != req.CandidateID || existing.Role != req.Role || !reflect.DeepEqual(existing.Predicate, req.Predicate) {
			return core.DecisionProjection{}, fmt.Errorf("prediction id conflicts with different semantic intent")
		}
		return s.Inspect(ctx, existing.EpisodeID, existing.CandidateID)
	}
	episode, found, err := s.mutations.FindEpisode(ctx, req.EpisodeID)
	if err != nil {
		return core.DecisionProjection{}, err
	}
	if !found {
		return core.DecisionProjection{}, fmt.Errorf("prediction episode unavailable")
	}
	prediction := core.PredictionBinding{PredictionID: req.PredictionID, EpisodeID: req.EpisodeID, ExperimentID: req.ExperimentID, CandidateID: req.CandidateID, Role: req.Role, Predicate: req.Predicate, SourceGeneration: episode.Baseline.SourceGeneration, CommittedAt: s.now().UTC()}
	return s.BindPrediction(ctx, prediction)
}
