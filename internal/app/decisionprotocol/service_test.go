package decisionprotocol

import (
	"context"
	"fmt"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
)

func TestDecisionProtocolRuntimeUsesExistingStructuredAndVerificationReaders(t *testing.T) {
	ledger := &fakeEpisodeLedger{}
	policy := &fakePolicyStore{}
	structured := &fakeStructuredSource{}
	verification := &fakeVerificationSource{}
	svc, err := NewRuntimeService(policy, fakeActivationGenerationSource{generation: "gen_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, EpisodeDependencies{
		Mutations: ledger, Experiments: &fakeExperimentMutationStore{ledger: ledger}, Ledger: ledger,
		Workspaces: fakeDPWorkspaceInspector{}, Snapshots: fakeDPSourceSnapshotter{},
		Receipts: fakeReceiptSource{}, Structured: structured, Verification: verification,
		Assessments: ledger, Selections: ledger, Authorities: ledger,
		AuthorityResolver: &fakeAuthorityResolver{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if svc.structured != structured || svc.verification != verification {
		t.Fatal("runtime replaced existing structured/verification readers")
	}
}

func TestDecisionProtocolRuntimeRejectsMissingRequiredReadSide(t *testing.T) {
	ledger := &fakeEpisodeLedger{}
	policy := &fakePolicyStore{}
	_, err := NewRuntimeService(policy, fakeActivationGenerationSource{generation: "gen_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, EpisodeDependencies{
		Mutations: ledger, Experiments: &fakeExperimentMutationStore{ledger: ledger}, Ledger: ledger,
		Workspaces: fakeDPWorkspaceInspector{}, Snapshots: fakeDPSourceSnapshotter{},
		Receipts: fakeReceiptSource{}, Structured: fakeStructuredSource{}, Verification: nil,
		Assessments: ledger, Selections: ledger, Authorities: ledger,
		AuthorityResolver: &fakeAuthorityResolver{},
	})
	if err == nil {
		t.Fatal("runtime composed without verification read side")
	}
}

func TestRuntimeInputFacadeReplaysServerOwnedTimestampFields(t *testing.T) {
	policy := &fakePolicyStore{}
	currentDPPolicy(t, policy)
	ledger := &fakeEpisodeLedger{}
	experiments := &fakeExperimentMutationStore{ledger: ledger}
	ws, snap := validDPWorkspaceAndSnapshot(t, "a")
	svc := NewService(policy, nil, EpisodeDependencies{Mutations: ledger, Experiments: experiments, Ledger: ledger, Workspaces: fakeDPWorkspaceInspector{ws}, Snapshots: fakeDPSourceSnapshotter{snap}})
	svc.now = func() time.Time { return time.Unix(1000, 0).UTC() }
	if _, err := svc.CreateEpisode(context.Background(), CreateEpisodeRequest{EpisodeID: "ep-runtime", Kind: core.EpisodeDiagnosis, RepositoryID: dpRepoID, WorkspaceID: dpWorkspaceID, ActorRef: "actor"}); err != nil {
		t.Fatal(err)
	}
	candidateReq := CreateCandidateInputRequest{EpisodeID: "ep-runtime", CandidateID: "cand-runtime", SemanticClaim: "claim", CandidateKind: "diagnosis", ActorRef: "actor"}
	if _, err := svc.CreateCandidateInput(context.Background(), candidateReq); err != nil {
		t.Fatal(err)
	}
	candidate, found, err := ledger.FindCandidate(context.Background(), "cand-runtime")
	if err != nil || !found {
		t.Fatal("candidate missing")
	}
	firstCandidateTime := candidate.DeclaredAt
	svc.now = func() time.Time { return time.Unix(2000, 0).UTC() }
	if _, err := svc.CreateCandidateInput(context.Background(), candidateReq); err != nil {
		t.Fatal(err)
	}
	candidate, _, _ = ledger.FindCandidate(context.Background(), "cand-runtime")
	if candidate.DeclaredAt != firstCandidateTime {
		t.Fatalf("candidate replay regenerated declared_at: first=%s replay=%s", firstCandidateTime, candidate.DeclaredAt)
	}

	experimentReq := DefineExperimentInputRequest{EpisodeID: "ep-runtime", ExperimentID: "exp-runtime", ActorRef: "actor"}
	if _, err := svc.DefineExperimentInput(context.Background(), experimentReq); err != nil {
		t.Fatal(err)
	}
	experiment, found, err := experiments.FindExperiment(context.Background(), "exp-runtime")
	if err != nil || !found {
		t.Fatal("experiment missing")
	}
	firstExperimentTime := experiment.DeclaredAt
	svc.now = func() time.Time { return time.Unix(3000, 0).UTC() }
	if _, err := svc.DefineExperimentInput(context.Background(), experimentReq); err != nil {
		t.Fatal(err)
	}
	experiment, _, _ = experiments.FindExperiment(context.Background(), "exp-runtime")
	if experiment.DeclaredAt != firstExperimentTime {
		t.Fatalf("experiment replay regenerated declared_at: first=%s replay=%s", firstExperimentTime, experiment.DeclaredAt)
	}

	predictionReq := BindPredictionInputRequest{EpisodeID: "ep-runtime", ExperimentID: "exp-runtime", PredictionID: "pred-runtime", CandidateID: "cand-runtime", Role: core.PredictionRequired, Predicate: core.ObservationPredicate{Kind: core.PredicateOperationOutcome, OperationOutcome: &core.OperationOutcomePredicate{ExpectedOutcome: core.OperationSuccess}}}
	if _, err := svc.BindPredictionInput(context.Background(), predictionReq); err != nil {
		t.Fatal(err)
	}
	cut, _ := ledger.CurrentHighWater(context.Background())
	records, _ := ledger.ListEpisodeRecords(context.Background(), "ep-runtime", cut)
	predictions := predictionsForExperiment(records, "exp-runtime")
	if len(predictions) != 1 {
		t.Fatalf("predictions=%#v", predictions)
	}
	firstPredictionTime := predictions[0].CommittedAt
	svc.now = func() time.Time { return time.Unix(4000, 0).UTC() }
	if _, err := svc.BindPredictionInput(context.Background(), predictionReq); err != nil {
		t.Fatal(err)
	}
	cut, _ = ledger.CurrentHighWater(context.Background())
	records, _ = ledger.ListEpisodeRecords(context.Background(), "ep-runtime", cut)
	predictions = predictionsForExperiment(records, "exp-runtime")
	if len(predictions) != 1 || predictions[0].CommittedAt != firstPredictionTime {
		t.Fatalf("prediction replay regenerated committed_at or duplicated: %#v", predictions)
	}
}

type runtimeRevisionLedger struct{ *fakeEpisodeLedger }

func (r *runtimeRevisionLedger) ReviseCandidateCAS(_ context.Context, parent core.CandidateID, child core.Candidate) (core.CanonicalRecordEnvelope, error) {
	if child.RevisesCandidateID != parent {
		return core.CanonicalRecordEnvelope{}, fmt.Errorf("parent mismatch")
	}
	return r.append(core.RecordCandidate, child)
}

func TestRuntimeRevisionInputReplaysServerOwnedDeclaredAt(t *testing.T) {
	policy := &fakePolicyStore{}
	currentDPPolicy(t, policy)
	ledger := &fakeEpisodeLedger{}
	mutations := &runtimeRevisionLedger{fakeEpisodeLedger: ledger}
	ws, snap := validDPWorkspaceAndSnapshot(t, "a")
	svc := NewService(policy, nil, EpisodeDependencies{Mutations: mutations, Ledger: ledger, Workspaces: fakeDPWorkspaceInspector{ws}, Snapshots: fakeDPSourceSnapshotter{snap}})
	svc.now = func() time.Time { return time.Unix(5000, 0).UTC() }
	if _, err := svc.CreateEpisode(context.Background(), CreateEpisodeRequest{EpisodeID: "ep-revise", Kind: core.EpisodeDiagnosis, RepositoryID: dpRepoID, WorkspaceID: dpWorkspaceID, ActorRef: "actor"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateCandidateInput(context.Background(), CreateCandidateInputRequest{EpisodeID: "ep-revise", CandidateID: "cand-parent", SemanticClaim: "parent", ActorRef: "actor"}); err != nil {
		t.Fatal(err)
	}
	req := ReviseCandidateInputRequest{EpisodeID: "ep-revise", ParentCandidateID: "cand-parent", CandidateID: "cand-child", SemanticClaim: "child", ActorRef: "actor"}
	if _, err := svc.ReviseCandidateInput(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	child, found, err := ledger.FindCandidate(context.Background(), "cand-child")
	if err != nil || !found {
		t.Fatal("revision child missing")
	}
	first := child.DeclaredAt
	svc.now = func() time.Time { return time.Unix(6000, 0).UTC() }
	if _, err := svc.ReviseCandidateInput(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	child, _, _ = ledger.FindCandidate(context.Background(), "cand-child")
	if child.DeclaredAt != first || child.RevisesCandidateID != "cand-parent" {
		t.Fatalf("revision replay changed canonical identity: %#v", child)
	}
}
