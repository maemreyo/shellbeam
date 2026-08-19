package decisionprotocol

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
)

func (f *fakeEpisodeLedger) RecordSelectionProposal(_ context.Context, proposal core.SelectionProposal) (core.CanonicalRecordEnvelope, bool, error) {
	for _, record := range f.records {
		if record.Kind != core.RecordSelectionProposal {
			continue
		}
		var existing core.SelectionProposal
		if err := json.Unmarshal(record.Body, &existing); err != nil {
			return core.CanonicalRecordEnvelope{}, false, err
		}
		if existing.ProposalID == proposal.ProposalID {
			if existing == proposal {
				return record, false, nil
			}
			return core.CanonicalRecordEnvelope{}, false, core.NewReasonError(core.ReasonIdempotencyConflict, "proposal identity conflict")
		}
	}
	env, err := f.append(core.RecordSelectionProposal, proposal)
	return env, err == nil, err
}

func (f *fakeEpisodeLedger) CommitSelectionCAS(_ context.Context, intent core.SelectionCommitIntent, commit core.SelectionCommit) (core.SelectionCommit, bool, error) {
	if f.selectionCommitFailures > 0 {
		f.selectionCommitFailures--
		return core.SelectionCommit{}, false, errors.New("injected selection persistence failure")
	}
	for _, record := range f.records {
		switch record.Kind {
		case core.RecordClosure:
			return core.SelectionCommit{}, false, core.NewReasonError(core.ReasonEpisodeTerminalConflict, "episode already closed")
		case core.RecordSelectionCommit:
			var existing core.SelectionCommit
			if err := json.Unmarshal(record.Body, &existing); err != nil {
				return core.SelectionCommit{}, false, err
			}
			if existing.IdempotencyKey == commit.IdempotencyKey {
				if existing.SemanticIntentFingerprint == commit.SemanticIntentFingerprint {
					return existing, false, nil
				}
				return core.SelectionCommit{}, false, core.NewReasonError(core.ReasonIdempotencyConflict, "idempotency key reused")
			}
			return core.SelectionCommit{}, false, core.NewReasonError(core.ReasonTerminalSelectionConflict, "selection already committed")
		}
	}
	if _, err := f.append(core.RecordSelectionCommit, commit); err != nil {
		return core.SelectionCommit{}, false, err
	}
	return commit, true, nil
}

func (f *fakeEpisodeLedger) CloseEpisodeCAS(_ context.Context, closure core.DecisionClosure) (core.DecisionClosure, bool, error) {
	for _, record := range f.records {
		switch record.Kind {
		case core.RecordSelectionCommit:
			return core.DecisionClosure{}, false, core.NewReasonError(core.ReasonEpisodeTerminalConflict, "episode already committed")
		case core.RecordClosure:
			var existing core.DecisionClosure
			if err := json.Unmarshal(record.Body, &existing); err != nil {
				return core.DecisionClosure{}, false, err
			}
			if existing.EpisodeID == closure.EpisodeID && existing.ProjectionDigest == closure.ProjectionDigest && existing.Reason == closure.Reason {
				return existing, false, nil
			}
			return core.DecisionClosure{}, false, core.NewReasonError(core.ReasonEpisodeTerminalConflict, "episode already closed")
		}
	}
	if _, err := f.append(core.RecordClosure, closure); err != nil {
		return core.DecisionClosure{}, false, err
	}
	return closure, true, nil
}

func selectionService(t *testing.T, policyContent core.PolicyContent) (*Service, *fakeEpisodeLedger, *fakePolicyStore) {
	t.Helper()
	digest, err := core.PolicyDigest(policyContent)
	if err != nil {
		t.Fatal(err)
	}
	policy := &fakePolicyStore{put: true}
	policy.snapshot = core.PolicySnapshot{SchemaVersion: 1, RepositoryID: dpRepoID, PolicyDigest: digest, Content: policyContent}
	policy.currentSnapshot = policy.snapshot
	policy.currentActivation = core.PolicyActivation{ActivationID: "act-selection", RepositoryID: dpRepoID, PolicyDigest: digest, ProposalGeneration: "gen_" + strings.Repeat("a", 64), ActivationGeneration: "gen_" + strings.Repeat("b", 64), Authority: core.AuthorityExplicitCaller, ActorRef: "actor", ActivatedAt: time.Unix(10, 0).UTC()}
	policy.currentOK = true
	ledger := &fakeEpisodeLedger{}
	ws, snap := validDPWorkspaceAndSnapshot(t, "a")
	svc := NewService(policy, nil, EpisodeDependencies{Mutations: ledger, Selections: ledger, Ledger: ledger, Workspaces: fakeDPWorkspaceInspector{ws}, Snapshots: fakeDPSourceSnapshotter{snap}})
	if _, err := svc.CreateEpisode(context.Background(), CreateEpisodeRequest{EpisodeID: "ep-selection", Kind: core.EpisodeDiagnosis, RepositoryID: dpRepoID, WorkspaceID: dpWorkspaceID, ActorRef: "actor"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateCandidate(context.Background(), core.Candidate{CandidateID: "cand-a", EpisodeID: "ep-selection", SemanticClaim: "A", DeclaredByActorRef: "actor", DeclaredAt: time.Unix(20, 0).UTC()}); err != nil {
		t.Fatal(err)
	}
	return svc, ledger, policy
}

func TestSelectionProposalPersistsWhileProtocolBlocked(t *testing.T) {
	req := core.DecisionRequirement{RequirementID: "challenge", Kind: core.RequirementCandidateChallenge, CandidateChallenge: &core.CandidateChallengeRequirement{MinimumDistinctLineages: 2}}
	svc, ledger, _ := selectionService(t, task7Policy(req))
	before, err := svc.Project(context.Background(), "ep-selection", "cand-a")
	if err != nil {
		t.Fatal(err)
	}
	if before.Protocol.Gate != core.GateBlocked {
		t.Fatalf("gate=%s", before.Protocol.Gate)
	}
	got, err := svc.ProposeSelection(context.Background(), ProposeSelectionRequest{EpisodeID: "ep-selection", CandidateID: "cand-a", ActorRef: "actor", Rationale: "prefer A"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ProposalID == "" {
		t.Fatal("missing proposal id")
	}
	count := 0
	for _, record := range ledger.records {
		if record.Kind == core.RecordSelectionProposal {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("proposals=%d", count)
	}
}

func TestSelectionProposalDoesNotChangeEpisodeState(t *testing.T) {
	svc, _, _ := selectionService(t, task7Policy())
	if _, err := svc.ProposeSelection(context.Background(), ProposeSelectionRequest{EpisodeID: "ep-selection", CandidateID: "cand-a", ActorRef: "actor"}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Project(context.Background(), "ep-selection", "cand-a")
	if err != nil {
		t.Fatal(err)
	}
	if got.EpisodeState != core.EpisodeOpen {
		t.Fatalf("state=%s", got.EpisodeState)
	}
}

func TestNormalCommitRequiresClearGateAndSettledLinkedExperiments(t *testing.T) {
	t.Run("blocked_gate", func(t *testing.T) {
		req := core.DecisionRequirement{RequirementID: "challenge", Kind: core.RequirementCandidateChallenge, CandidateChallenge: &core.CandidateChallengeRequirement{MinimumDistinctLineages: 2}}
		svc, _, policy := selectionService(t, task7Policy(req))
		p, err := svc.Project(context.Background(), "ep-selection", "cand-a")
		if err != nil {
			t.Fatal(err)
		}
		_, err = svc.CommitSelection(context.Background(), CommitSelectionRequest{EpisodeID: "ep-selection", CandidateID: "cand-a", ActorRef: "actor", ExpectedPolicyDigest: policy.snapshot.PolicyDigest, ExpectedProjectionDigest: p.ProjectionDigest, IdempotencyKey: "idem-blocked"})
		if r, ok := core.ReasonOf(err); !ok || r != core.ReasonProtocolBlocked {
			t.Fatalf("err=%v reason=%s", err, r)
		}
	})
	t.Run("post_abort_settling", func(t *testing.T) {
		svc, ledger, policy := selectionService(t, task7Policy())
		exp := core.Experiment{SchemaVersion: 1, ExperimentID: "exp-settling", EpisodeID: "ep-selection", DeclaredByActorRef: "actor", DeclaredAt: time.Unix(30, 0).UTC()}
		if _, err := ledger.append(core.RecordExperiment, exp); err != nil {
			t.Fatal(err)
		}
		seal := core.ExperimentSeal{ExperimentID: exp.ExperimentID, SourceGeneration: dpSourceGeneration(t, svc, "ep-selection"), SealedPredictionDigest: "pred_" + strings.Repeat("c", 64), BaseProjectionCutRef: core.DecisionProjectionCutRef{EpisodeID: "ep-selection", CanonicalRecordHighWater: core.RecordSeq(len(ledger.records))}, BaseCandidateProjectionDigest: "proj_" + strings.Repeat("d", 64), SealedAt: time.Unix(31, 0).UTC()}
		if _, err := ledger.append(core.RecordExperimentSeal, seal); err != nil {
			t.Fatal(err)
		}
		link := core.ExperimentExecutionLink{SchemaVersion: 1, LinkID: "link-settling", ExperimentID: exp.ExperimentID, OperationID: "op-settling", SessionID: "sess-settling", WorkspaceID: dpWorkspaceID, SourceGeneration: seal.SourceGeneration, AcceptedRequestFingerprint: strings.Repeat("1", 64), AcceptedExecutionFingerprint: strings.Repeat("2", 64), AcceptedObservationBindingFingerprint: strings.Repeat("3", 64), AdmittedAt: time.Unix(32, 0).UTC()}
		if _, err := ledger.append(core.RecordExperimentExecutionLink, link); err != nil {
			t.Fatal(err)
		}
		abort := core.ExperimentAbort{SchemaVersion: 1, AbortID: "abort-settling", ExperimentID: exp.ExperimentID, Phase: core.AbortAfterExecutionLink, ExecutionLinkID: link.LinkID, Reason: "stop", AbortedByActorRef: "actor", AbortedAt: time.Unix(33, 0).UTC()}
		if _, err := ledger.append(core.RecordExperimentAbort, abort); err != nil {
			t.Fatal(err)
		}
		p, err := svc.Project(context.Background(), "ep-selection", "cand-a")
		if err != nil {
			t.Fatal(err)
		}
		if p.Protocol.Gate != core.GateClear {
			t.Fatalf("gate=%s", p.Protocol.Gate)
		}
		_, err = svc.CommitSelection(context.Background(), CommitSelectionRequest{EpisodeID: "ep-selection", CandidateID: "cand-a", ActorRef: "actor", ExpectedPolicyDigest: policy.snapshot.PolicyDigest, ExpectedProjectionDigest: p.ProjectionDigest, IdempotencyKey: "idem-settling"})
		if r, ok := core.ReasonOf(err); !ok || r != core.ReasonObservationNotSettled {
			t.Fatalf("err=%v reason=%s", err, r)
		}
	})
}

func TestCommitSelectionRejectsPolicyProjectionSourceAndOverridePreconditions(t *testing.T) {
	svc, _, policy := selectionService(t, task7Policy())
	p, err := svc.Project(context.Background(), "ep-selection", "cand-a")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		req    CommitSelectionRequest
		reason core.ReasonCode
	}{
		{"policy", CommitSelectionRequest{EpisodeID: "ep-selection", CandidateID: "cand-a", ActorRef: "actor", ExpectedPolicyDigest: "pol_" + strings.Repeat("f", 64), ExpectedProjectionDigest: p.ProjectionDigest, IdempotencyKey: "i1"}, core.ReasonPolicyConflict},
		{"projection", CommitSelectionRequest{EpisodeID: "ep-selection", CandidateID: "cand-a", ActorRef: "actor", ExpectedPolicyDigest: policy.snapshot.PolicyDigest, ExpectedProjectionDigest: "proj_" + strings.Repeat("e", 64), IdempotencyKey: "i2"}, core.ReasonProjectionConflict},
		{"override", CommitSelectionRequest{EpisodeID: "ep-selection", CandidateID: "cand-a", ActorRef: "actor", ExpectedPolicyDigest: policy.snapshot.PolicyDigest, ExpectedProjectionDigest: p.ProjectionDigest, OverrideRef: "override-not-yet", IdempotencyKey: "i3"}, core.ReasonOverrideAuthorityNotAdmissible},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.CommitSelection(context.Background(), tc.req)
			if r, ok := core.ReasonOf(err); !ok || r != tc.reason {
				t.Fatalf("err=%v reason=%s", err, r)
			}
		})
	}
}

func TestCloseUnresolvedDoesNotRequireClearGate(t *testing.T) {
	req := core.DecisionRequirement{RequirementID: "challenge", Kind: core.RequirementCandidateChallenge, CandidateChallenge: &core.CandidateChallengeRequirement{MinimumDistinctLineages: 2}}
	svc, _, _ := selectionService(t, task7Policy(req))
	p, err := svc.Project(context.Background(), "ep-selection", "")
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.CloseUnresolved(context.Background(), CloseUnresolvedRequest{EpisodeID: "ep-selection", ActorRef: "actor", ProjectionDigest: p.ProjectionDigest, Reason: "not enough evidence", UnresolvedDimensions: []string{"root-cause"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != core.ClosureUnresolved {
		t.Fatalf("closure=%#v", got)
	}
}

func dpSourceGeneration(t *testing.T, svc *Service, episodeID core.EpisodeID) string {
	t.Helper()
	ep, ok, err := svc.mutations.FindEpisode(context.Background(), episodeID)
	if err != nil || !ok {
		t.Fatalf("episode: %v %v", ok, err)
	}
	return ep.Baseline.SourceGeneration
}

func TestCommitSelectionRejectsStaleSourceGeneration(t *testing.T) {
	svc, _, policy := selectionService(t, task7Policy())
	p, err := svc.Project(context.Background(), "ep-selection", "cand-a")
	if err != nil {
		t.Fatal(err)
	}
	_, stale := validDPWorkspaceAndSnapshot(t, "b")
	svc.snapshots = fakeDPSourceSnapshotter{snap: stale}
	_, err = svc.CommitSelection(context.Background(), CommitSelectionRequest{EpisodeID: "ep-selection", CandidateID: "cand-a", ActorRef: "actor", ExpectedPolicyDigest: policy.snapshot.PolicyDigest, ExpectedProjectionDigest: p.ProjectionDigest, IdempotencyKey: "idem-stale"})
	if r, ok := core.ReasonOf(err); !ok || r != core.ReasonStaleEpisodeSourceGeneration {
		t.Fatalf("err=%v reason=%s", err, r)
	}
}

func TestCommitSelectionMapsIndeterminateGateToStableReason(t *testing.T) {
	req := core.DecisionRequirement{RequirementID: "verify", Kind: core.RequirementVerifierAssessment, VerifierAssessment: &core.VerifierAssessmentRequirement{MinimumSupportingAssessments: 1, RequiredContextClass: core.ContextIndependentModel}}
	svc, ledger, policy := selectionService(t, task7Policy(req))
	assessment := core.VerifierAssessment{AssessmentID: "as-indeterminate", EpisodeID: "ep-selection", ActorRef: "actor", DeclaredContextClass: core.ContextIndependentModel, PreferredCandidates: []core.CandidateID{"cand-a"}, Rationale: "unqualified", CreatedAt: time.Unix(40, 0).UTC()}
	if _, err := ledger.append(core.RecordVerifierAssessment, assessment); err != nil {
		t.Fatal(err)
	}
	p, err := svc.Project(context.Background(), "ep-selection", "cand-a")
	if err != nil {
		t.Fatal(err)
	}
	if p.Protocol.Gate != core.GateIndeterminate {
		t.Fatalf("gate=%s", p.Protocol.Gate)
	}
	_, err = svc.CommitSelection(context.Background(), CommitSelectionRequest{EpisodeID: "ep-selection", CandidateID: "cand-a", ActorRef: "actor", ExpectedPolicyDigest: policy.snapshot.PolicyDigest, ExpectedProjectionDigest: p.ProjectionDigest, IdempotencyKey: "idem-indeterminate"})
	if r, ok := core.ReasonOf(err); !ok || r != core.ReasonProtocolIndeterminate {
		t.Fatalf("err=%v reason=%s", err, r)
	}
}
