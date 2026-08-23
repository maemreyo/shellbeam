package decisionprotocol

import (
	"context"
	"fmt"

	core "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
)

func (s *Service) CreateCandidate(ctx context.Context, candidate core.Candidate) (core.DecisionProjection, error) {
	if s == nil || s.mutations == nil {
		return core.DecisionProjection{}, fmt.Errorf("decision candidate store unavailable")
	}
	if _, _, err := s.mutations.CreateCandidate(ctx, candidate); err != nil {
		return core.DecisionProjection{}, err
	}
	return s.Inspect(ctx, candidate.EpisodeID, candidate.CandidateID)
}

func (s *Service) ReviseCandidate(ctx context.Context, parent core.CandidateID, child core.Candidate) (core.DecisionProjection, error) {
	if s == nil || s.mutations == nil {
		return core.DecisionProjection{}, fmt.Errorf("decision candidate store unavailable")
	}
	if _, err := s.mutations.ReviseCandidateCAS(ctx, parent, child); err != nil {
		return core.DecisionProjection{}, err
	}
	return s.Inspect(ctx, child.EpisodeID, child.CandidateID)
}
