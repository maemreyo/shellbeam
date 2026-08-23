package decisionprotocol

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	core "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

type fakeReceiptSource struct {
	receipts map[operation.ID]receipt.Receipt
}

func (f fakeReceiptSource) FindReceiptByOperation(_ context.Context, id operation.ID) (receipt.Receipt, bool, error) {
	rec, ok := f.receipts[id]
	return rec, ok, nil
}

type fakeStructuredSource struct{ result structuredapp.InspectResult }

func (f fakeStructuredSource) InspectStructured(context.Context, structuredapp.InspectRequest) (structuredapp.InspectResult, error) {
	return f.result, nil
}

type fakeVerificationSource struct {
	cut VerificationObservationCut
	set QualifiedEvidenceSet
}

func (f fakeVerificationSource) AcquireVerificationObservationCut(context.Context, operation.ID) (VerificationObservationCut, error) {
	return f.cut, nil
}
func (f fakeVerificationSource) QualifiedEvidenceForOperation(context.Context, operation.ID, VerificationObservationCut) (QualifiedEvidenceSet, error) {
	return f.set, nil
}

func (f *fakeExperimentMutationStore) MaterializeExperimentObservationCAS(_ context.Context, binding core.ExperimentObservationBinding) (core.ExperimentObservationBinding, bool, error) {
	for _, record := range f.ledger.records {
		if record.Kind != core.RecordExperimentObservationBinding {
			continue
		}
		var existing core.ExperimentObservationBinding
		if err := json.Unmarshal(record.Body, &existing); err != nil {
			return core.ExperimentObservationBinding{}, false, err
		}
		if existing.ExperimentID == binding.ExperimentID {
			if existing.DerivationCutDigest == binding.DerivationCutDigest {
				return existing, false, nil
			}
			return core.ExperimentObservationBinding{}, false, core.NewReasonError(core.ReasonExperimentObservationBindingConflict, "different observation cut")
		}
	}
	if _, err := f.ledger.append(core.RecordExperimentObservationBinding, binding); err != nil {
		return core.ExperimentObservationBinding{}, false, err
	}
	return binding, true, nil
}

func task6OperationOutcomeService(t *testing.T) (*Service, *fakeEpisodeLedger, *panicExecutionMutationStore, core.Experiment, core.PredictionBinding, core.ExperimentExecutionLink) {
	t.Helper()
	policy := &fakePolicyStore{}
	currentDPPolicy(t, policy)
	ledger := &fakeEpisodeLedger{}
	ws, snap := validDPWorkspaceAndSnapshot(t, "a")
	experiments := &panicExecutionMutationStore{fakeExperimentMutationStore: fakeExperimentMutationStore{ledger: ledger}}
	zero := 0
	rec := receipt.Receipt{SchemaVersion: 1, OperationID: "op-task6", SessionID: "sess-task6", State: session.Completed, Outcome: session.Success, OutputComplete: true, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}, Exit: receipt.ExitEvidence{Reaped: true, Code: &zero}}
	svc := NewService(policy, nil, EpisodeDependencies{
		Mutations: ledger, Experiments: experiments, Ledger: ledger, Workspaces: fakeDPWorkspaceInspector{ws}, Snapshots: fakeDPSourceSnapshotter{snap},
		Receipts: fakeReceiptSource{receipts: map[operation.ID]receipt.Receipt{"op-task6": rec}},
	})
	if _, err := svc.CreateEpisode(context.Background(), CreateEpisodeRequest{EpisodeID: "ep-task6", Kind: core.EpisodeDiagnosis, RepositoryID: dpRepoID, WorkspaceID: dpWorkspaceID, ActorRef: "actor"}); err != nil {
		t.Fatal(err)
	}
	candidate := core.Candidate{CandidateID: "cand-task6", EpisodeID: "ep-task6", SemanticClaim: "candidate", DeclaredByActorRef: "actor", DeclaredAt: time.Unix(2, 0).UTC()}
	if _, err := svc.CreateCandidate(context.Background(), candidate); err != nil {
		t.Fatal(err)
	}
	experiment := core.Experiment{SchemaVersion: 1, ExperimentID: "exp-task6", EpisodeID: "ep-task6", DeclaredByActorRef: "actor", DeclaredAt: time.Unix(3, 0).UTC()}
	if _, err := svc.DefineExperiment(context.Background(), experiment); err != nil {
		t.Fatal(err)
	}
	prediction := core.PredictionBinding{PredictionID: "pred-task6", EpisodeID: "ep-task6", ExperimentID: experiment.ExperimentID, CandidateID: candidate.CandidateID, Role: core.PredictionRequired, Predicate: core.ObservationPredicate{Kind: core.PredicateOperationOutcome, OperationOutcome: &core.OperationOutcomePredicate{ExpectedOutcome: core.OperationSuccess}}, SourceGeneration: snap.Generation, CommittedAt: time.Unix(4, 0).UTC()}
	if _, err := svc.BindPrediction(context.Background(), prediction); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.SealExperiment(context.Background(), experiment.ExperimentID, "actor"); err != nil {
		t.Fatal(err)
	}
	link := core.ExperimentExecutionLink{SchemaVersion: 1, LinkID: "link-task6", ExperimentID: experiment.ExperimentID, OperationID: "op-task6", SessionID: "sess-task6", WorkspaceID: dpWorkspaceID, SourceGeneration: snap.Generation, AcceptedRequestFingerprint: strings.Repeat("a", 64), AcceptedExecutionFingerprint: strings.Repeat("b", 64), AcceptedObservationBindingFingerprint: strings.Repeat("c", 64), AdmittedAt: time.Unix(5, 0).UTC()}
	if _, err := ledger.append(core.RecordExperimentExecutionLink, link); err != nil {
		t.Fatal(err)
	}
	return svc, ledger, experiments, experiment, prediction, link
}

func TestCloseExperimentMaterializesObservationBeforeClosure(t *testing.T) {
	svc, ledger, _, experiment, prediction, _ := task6OperationOutcomeService(t)
	if _, err := svc.CloseExperiment(context.Background(), experiment.ExperimentID, "actor"); err != nil {
		t.Fatal(err)
	}
	var bindings, closures int
	for _, record := range ledger.records {
		switch record.Kind {
		case core.RecordExperimentObservationBinding:
			bindings++
			var binding core.ExperimentObservationBinding
			if err := json.Unmarshal(record.Body, &binding); err != nil {
				t.Fatal(err)
			}
			if len(binding.PredictionResults) != 1 || binding.PredictionResults[0].PredictionID != prediction.PredictionID || binding.PredictionResults[0].Status != core.PredictionMatch {
				t.Fatalf("binding=%#v", binding)
			}
		case core.RecordExperimentClosure:
			closures++
		}
	}
	if bindings != 1 || closures != 1 {
		t.Fatalf("bindings=%d closures=%d", bindings, closures)
	}
}

func TestPostLinkAbortSettlesThroughSameObservationCASWhenTerminal(t *testing.T) {
	svc, ledger, _, experiment, _, _ := task6OperationOutcomeService(t)
	if _, err := svc.AbortExperiment(context.Background(), experiment.ExperimentID, core.AbortAfterExecutionLink, "stop", "actor"); err != nil {
		t.Fatal(err)
	}
	projection, err := svc.Inspect(context.Background(), experiment.EpisodeID, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, got := range projection.Experiments {
		if got.ExperimentID == experiment.ExperimentID {
			if got.State != core.ExperimentAborted || got.ObservationState != core.ObservationSettled {
				t.Fatalf("projection=%#v", got)
			}
		}
	}
	bindings := 0
	for _, record := range ledger.records {
		if record.Kind == core.RecordExperimentObservationBinding {
			bindings++
		}
	}
	if bindings != 1 {
		t.Fatalf("bindings=%d", bindings)
	}
}
