package decisionprotocol

import (
	"context"
	"fmt"

	core "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
)

type Service struct {
	policies   PolicyStore
	generation ActivationGenerationSource
}

func NewService(policies PolicyStore, generation ActivationGenerationSource) *Service {
	return &Service{policies: policies, generation: generation}
}

type PutPolicySnapshotRequest struct {
	RepositoryID string
	Content      core.PolicyContent
}

type ActivatePolicyRequest struct {
	RepositoryID                 string
	ActivationID                 string
	PolicyDigest                 string
	ProposalGeneration           string
	ExpectedPreviousPolicyDigest string
	ActorRef                     string
}

func (s *Service) PutPolicySnapshot(ctx context.Context, req PutPolicySnapshotRequest) (core.PolicySnapshot, error) {
	if s == nil || s.policies == nil || req.RepositoryID == "" {
		return core.PolicySnapshot{}, fmt.Errorf("decision policy store unavailable or repository missing")
	}
	digest, err := core.PolicyDigest(req.Content)
	if err != nil {
		return core.PolicySnapshot{}, err
	}
	snapshot := core.PolicySnapshot{SchemaVersion: 1, RepositoryID: req.RepositoryID, PolicyDigest: digest, Content: req.Content}
	if err := snapshot.Validate(); err != nil {
		return core.PolicySnapshot{}, err
	}
	if _, err := s.policies.PutPolicySnapshot(ctx, snapshot); err != nil {
		return core.PolicySnapshot{}, err
	}
	return snapshot, nil
}

func (s *Service) ActivatePolicy(ctx context.Context, req ActivatePolicyRequest) (core.PolicyActivation, error) {
	if s == nil || s.policies == nil || s.generation == nil {
		return core.PolicyActivation{}, fmt.Errorf("decision policy activation authority unavailable")
	}
	activationGeneration, err := s.generation.CurrentActivationGeneration(ctx, req.RepositoryID)
	if err != nil {
		return core.PolicyActivation{}, err
	}
	commit := core.PolicyActivationCommit{
		Intent: core.PolicyActivationIntent{
			ActivationID: req.ActivationID, RepositoryID: req.RepositoryID,
			PreviousEffectivePolicyDigest: req.ExpectedPreviousPolicyDigest,
			ProposedPolicyDigest:          req.PolicyDigest, ProposalGeneration: req.ProposalGeneration,
			Authority: core.AuthorityExplicitCaller, ActorRef: req.ActorRef,
		},
		ActivationGeneration: activationGeneration,
	}
	if err := commit.Validate(); err != nil {
		return core.PolicyActivation{}, err
	}
	result, err := s.policies.ActivatePolicyCAS(ctx, commit)
	if err != nil {
		return core.PolicyActivation{}, err
	}
	return result.Record, nil
}
