package decisionprotocol

import (
	"context"
	"fmt"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
)

type Service struct {
	policies          PolicyStore
	generation        ActivationGenerationSource
	mutations         EpisodeMutationStore
	experiments       ExperimentMutationStore
	ledger            CanonicalLedgerStore
	workspaces        WorkspaceInspector
	snapshots         SourceSnapshotter
	receipts          ReceiptSource
	structured        StructuredSource
	verification      VerificationSource
	assessments       AssessmentStore
	qualifier         VerifierContextQualifier
	selections        SelectionStore
	authorities       AuthorityStore
	authorityResolver AuthorityResolver
	now               func() time.Time
}

func NewService(policies PolicyStore, generation ActivationGenerationSource, episodeDeps ...EpisodeDependencies) *Service {
	s := &Service{policies: policies, generation: generation, now: func() time.Time { return time.Now().UTC() }}
	if len(episodeDeps) > 0 {
		deps := episodeDeps[0]
		s.mutations, s.experiments, s.ledger, s.workspaces, s.snapshots = deps.Mutations, deps.Experiments, deps.Ledger, deps.Workspaces, deps.Snapshots
		s.receipts, s.structured, s.verification = deps.Receipts, deps.Structured, deps.Verification
		s.assessments, s.qualifier = deps.Assessments, deps.VerifierQualifier
		s.selections = deps.Selections
		s.authorities, s.authorityResolver = deps.Authorities, deps.AuthorityResolver
		if s.experiments == nil {
			if experiments, ok := any(deps.Mutations).(ExperimentMutationStore); ok {
				s.experiments = experiments
			}
		}
		if s.assessments == nil {
			if assessments, ok := any(deps.Mutations).(AssessmentStore); ok {
				s.assessments = assessments
			}
		}
	}
	return s
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
	intent := core.PolicyActivationIntent{
		ActivationID: req.ActivationID, RepositoryID: req.RepositoryID,
		PreviousEffectivePolicyDigest: req.ExpectedPreviousPolicyDigest,
		ProposedPolicyDigest:          req.PolicyDigest, ProposalGeneration: req.ProposalGeneration,
		Authority: core.AuthorityExplicitCaller, ActorRef: req.ActorRef,
	}
	if err := intent.Validate(); err != nil {
		return core.PolicyActivation{}, err
	}

	// Durable replay identity is resolved before any fresh source observation.
	// activation_generation is server-owned frozen state, not caller intent.
	if existing, found, err := s.policies.FindPolicyActivationCommit(ctx, req.RepositoryID, req.ActivationID); err != nil {
		return core.PolicyActivation{}, err
	} else if found {
		if err := existing.Validate(); err != nil {
			return core.PolicyActivation{}, err
		}
		want, _ := core.PolicyActivationIntentFingerprint(intent)
		got, _ := core.PolicyActivationIntentFingerprint(existing.Intent)
		if got != want {
			return core.PolicyActivation{}, fmt.Errorf("decision policy activation id conflicts with different intent")
		}
		result, err := s.policies.ActivatePolicyCAS(ctx, core.PolicyActivationCommit{Intent: intent, ActivationGeneration: existing.ActivationGeneration})
		if err != nil {
			return core.PolicyActivation{}, err
		}
		return result.Record, nil
	}

	activationGeneration, err := s.generation.CurrentActivationGeneration(ctx, req.RepositoryID)
	if err != nil {
		return core.PolicyActivation{}, err
	}
	commit := core.PolicyActivationCommit{Intent: intent, ActivationGeneration: activationGeneration}
	if err := commit.Validate(); err != nil {
		return core.PolicyActivation{}, err
	}
	result, err := s.policies.ActivatePolicyCAS(ctx, commit)
	if err != nil {
		return core.PolicyActivation{}, err
	}
	return result.Record, nil
}
