package decisionprotocol

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
)

func discriminatorPrediction(id string, candidate core.CandidateID, name string, status core.StructuredTestStatus) core.PredictionBinding {
	return core.PredictionBinding{PredictionID: core.PredictionID(id), EpisodeID: "ep-1", ExperimentID: "exp-1", CandidateID: candidate, Role: core.PredictionDiscriminator, Predicate: core.ObservationPredicate{Kind: core.PredicateStructuredTestStatus, StructuredTestStatus: &core.StructuredTestStatusPredicate{Target: core.StructuredTargetTestCase, Package: "pkg", Name: name, ExpectedStatus: status}}, SourceGeneration: "gen_" + strings.Repeat("a", 64), CommittedAt: time.Unix(10, 0).UTC()}
}

func TestPotentialDiscriminationPairsSameDimensionDifferentOutcome(t *testing.T) {
	candidates := []core.CandidateProjection{{CandidateID: "a", LineageRoot: "a", State: core.CandidateActive}, {CandidateID: "b", LineageRoot: "b", State: core.CandidateActive}}
	preds := []core.PredictionBinding{discriminatorPrediction("p1", "a", "TestRace", core.StructuredTestPass), discriminatorPrediction("p2", "b", "TestRace", core.StructuredTestFail)}
	pairs, err := potentialDiscriminationPairs(candidates, preds, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 2 {
		t.Fatalf("pairs=%#v", pairs)
	}
}
func TestPotentialDiscriminationPairsSameOutcomeDoesNotQualify(t *testing.T) {
	candidates := []core.CandidateProjection{{CandidateID: "a", LineageRoot: "a", State: core.CandidateActive}, {CandidateID: "b", LineageRoot: "b", State: core.CandidateActive}}
	preds := []core.PredictionBinding{discriminatorPrediction("p1", "a", "TestRace", core.StructuredTestPass), discriminatorPrediction("p2", "b", "TestRace", core.StructuredTestPass)}
	pairs, err := potentialDiscriminationPairs(candidates, preds, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 0 {
		t.Fatalf("pairs=%#v", pairs)
	}
}
func TestPotentialDiscriminationPairsDifferentDimensionDoesNotQualify(t *testing.T) {
	candidates := []core.CandidateProjection{{CandidateID: "a", LineageRoot: "a", State: core.CandidateActive}, {CandidateID: "b", LineageRoot: "b", State: core.CandidateActive}}
	preds := []core.PredictionBinding{discriminatorPrediction("p1", "a", "TestRace", core.StructuredTestPass), discriminatorPrediction("p2", "b", "TestOther", core.StructuredTestFail)}
	pairs, err := potentialDiscriminationPairs(candidates, preds, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 0 {
		t.Fatalf("pairs=%#v", pairs)
	}
}
func TestPotentialDiscriminationPairsSupersededOrBlockedChallengerDoesNotQualify(t *testing.T) {
	preds := []core.PredictionBinding{discriminatorPrediction("p1", "a", "TestRace", core.StructuredTestPass), discriminatorPrediction("p2", "b", "TestRace", core.StructuredTestFail)}
	superseded := []core.CandidateProjection{{CandidateID: "a", LineageRoot: "a", State: core.CandidateActive}, {CandidateID: "b", LineageRoot: "b", State: core.CandidateSuperseded}}
	if pairs, _ := potentialDiscriminationPairs(superseded, preds, nil); len(pairs) != 0 {
		t.Fatalf("superseded pairs=%#v", pairs)
	}
	active := []core.CandidateProjection{{CandidateID: "a", LineageRoot: "a", State: core.CandidateActive}, {CandidateID: "b", LineageRoot: "b", State: core.CandidateActive}}
	if pairs, _ := potentialDiscriminationPairs(active, preds, map[core.CandidateID]bool{"b": true}); len(pairs) != 0 {
		t.Fatalf("blocked pairs=%#v", pairs)
	}
}

func TestSealRejectsPredictionFromOtherEpisodeOrSourceGeneration(t *testing.T) {
	bad := discriminatorPrediction("p1", "a", "TestRace", core.StructuredTestPass)
	bad.EpisodeID = "ep-other"
	if err := validateSealPredictions("ep-1", "gen_"+strings.Repeat("a", 64), []core.PredictionBinding{bad}); err == nil {
		t.Fatal("cross-episode prediction accepted")
	}
	bad.EpisodeID = "ep-1"
	bad.SourceGeneration = "gen_" + strings.Repeat("b", 64)
	if err := validateSealPredictions("ep-1", "gen_"+strings.Repeat("a", 64), []core.PredictionBinding{bad}); err == nil {
		t.Fatal("cross-generation prediction accepted")
	}
}

type fakeExperimentMutationStore struct{ ledger *fakeEpisodeLedger }

func (f *fakeExperimentMutationStore) DefineExperiment(_ context.Context, experiment core.Experiment) (core.CanonicalRecordEnvelope, bool, error) {
	env, err := f.ledger.append(core.RecordExperiment, experiment)
	return env, err == nil, err
}
func (f *fakeExperimentMutationStore) FindExperiment(_ context.Context, id core.ExperimentID) (core.Experiment, bool, error) {
	for _, record := range f.ledger.records {
		if record.Kind != core.RecordExperiment {
			continue
		}
		var experiment core.Experiment
		if err := json.Unmarshal(record.Body, &experiment); err != nil {
			return core.Experiment{}, false, err
		}
		if experiment.ExperimentID == id {
			return experiment, true, nil
		}
	}
	return core.Experiment{}, false, nil
}
func (f *fakeExperimentMutationStore) BindPrediction(_ context.Context, prediction core.PredictionBinding) (core.CanonicalRecordEnvelope, bool, error) {
	env, err := f.ledger.append(core.RecordPredictionBinding, prediction)
	return env, err == nil, err
}
func (f *fakeExperimentMutationStore) SealExperimentCAS(_ context.Context, seal core.ExperimentSeal) (core.CanonicalRecordEnvelope, bool, error) {
	env, err := f.ledger.append(core.RecordExperimentSeal, seal)
	return env, err == nil, err
}
func (f *fakeExperimentMutationStore) CloseExperimentCAS(_ context.Context, closure core.ExperimentClosure) (core.CanonicalRecordEnvelope, bool, error) {
	env, err := f.ledger.append(core.RecordExperimentClosure, closure)
	return env, err == nil, err
}
func (f *fakeExperimentMutationStore) AbortExperimentCAS(_ context.Context, abort core.ExperimentAbort) (core.CanonicalRecordEnvelope, bool, error) {
	env, err := f.ledger.append(core.RecordExperimentAbort, abort)
	return env, err == nil, err
}

type panicExecutionMutationStore struct {
	fakeExperimentMutationStore
	starts int
}

func (p *panicExecutionMutationStore) Start() {
	p.starts++
	panic("execution start must not be called")
}

func TestExperimentActionsDoNotCallExecutionStart(t *testing.T) {
	policy := &fakePolicyStore{}
	currentDPPolicy(t, policy)
	ledger := &fakeEpisodeLedger{}
	ws, snap := validDPWorkspaceAndSnapshot(t, "a")
	experiments := &panicExecutionMutationStore{fakeExperimentMutationStore: fakeExperimentMutationStore{ledger: ledger}}
	svc := NewService(policy, nil, EpisodeDependencies{Mutations: ledger, Experiments: experiments, Ledger: ledger, Workspaces: fakeDPWorkspaceInspector{ws}, Snapshots: fakeDPSourceSnapshotter{snap}})
	if _, err := svc.CreateEpisode(context.Background(), CreateEpisodeRequest{EpisodeID: "ep-1", Kind: core.EpisodeDiagnosis, RepositoryID: dpRepoID, WorkspaceID: dpWorkspaceID, ActorRef: "actor"}); err != nil {
		t.Fatal(err)
	}
	candidate := core.Candidate{CandidateID: "a", EpisodeID: "ep-1", SemanticClaim: "A", DeclaredByActorRef: "actor", DeclaredAt: time.Unix(2, 0).UTC()}
	if _, err := svc.CreateCandidate(context.Background(), candidate); err != nil {
		t.Fatal(err)
	}
	experiment := core.Experiment{SchemaVersion: 1, ExperimentID: "exp-1", EpisodeID: "ep-1", DeclaredByActorRef: "actor", DeclaredAt: time.Unix(3, 0).UTC()}
	if _, err := svc.DefineExperiment(context.Background(), experiment); err != nil {
		t.Fatal(err)
	}
	prediction := discriminatorPrediction("p1", "a", "TestRace", core.StructuredTestPass)
	prediction.SourceGeneration = snap.Generation
	if _, err := svc.BindPrediction(context.Background(), prediction); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.SealExperiment(context.Background(), "exp-1", "actor"); err != nil {
		t.Fatal(err)
	}
	_, _ = svc.CloseExperiment(context.Background(), "exp-1", "actor") // no observation binding yet: expected to fail closed, never schedule.
	if _, err := svc.AbortExperiment(context.Background(), "exp-1", core.AbortBeforeExecution, "stop", "actor"); err != nil {
		t.Fatal(err)
	}
	if experiments.starts != 0 {
		t.Fatalf("execution Start called %d times", experiments.starts)
	}
}

func task4ExperimentService(t *testing.T) (*Service, *fakeEpisodeLedger, *panicExecutionMutationStore, core.Candidate, core.Experiment, core.PredictionBinding) {
	t.Helper()
	policy := &fakePolicyStore{}
	currentDPPolicy(t, policy)
	ledger := &fakeEpisodeLedger{}
	ws, snap := validDPWorkspaceAndSnapshot(t, "a")
	experiments := &panicExecutionMutationStore{fakeExperimentMutationStore: fakeExperimentMutationStore{ledger: ledger}}
	svc := NewService(policy, nil, EpisodeDependencies{Mutations: ledger, Experiments: experiments, Ledger: ledger, Workspaces: fakeDPWorkspaceInspector{ws}, Snapshots: fakeDPSourceSnapshotter{snap}})
	if _, err := svc.CreateEpisode(context.Background(), CreateEpisodeRequest{EpisodeID: "ep-1", Kind: core.EpisodeDiagnosis, RepositoryID: dpRepoID, WorkspaceID: dpWorkspaceID, ActorRef: "actor"}); err != nil {
		t.Fatal(err)
	}
	candidate := core.Candidate{CandidateID: "a", EpisodeID: "ep-1", SemanticClaim: "A", DeclaredByActorRef: "actor", DeclaredAt: time.Unix(2, 0).UTC()}
	if _, err := svc.CreateCandidate(context.Background(), candidate); err != nil {
		t.Fatal(err)
	}
	experiment := core.Experiment{SchemaVersion: 1, ExperimentID: "exp-1", EpisodeID: "ep-1", DeclaredByActorRef: "actor", DeclaredAt: time.Unix(3, 0).UTC()}
	if _, err := svc.DefineExperiment(context.Background(), experiment); err != nil {
		t.Fatal(err)
	}
	prediction := discriminatorPrediction("p1", "a", "TestRace", core.StructuredTestPass)
	prediction.SourceGeneration = snap.Generation
	if _, err := svc.BindPrediction(context.Background(), prediction); err != nil {
		t.Fatal(err)
	}
	return svc, ledger, experiments, candidate, experiment, prediction
}

func TestSealFreezesPredictionDigestAndCanonicalLedgerHighWater(t *testing.T) {
	svc, ledger, _, _, experiment, prediction := task4ExperimentService(t)
	before, _ := ledger.CurrentHighWater(context.Background())
	seal, _, err := svc.SealExperiment(context.Background(), experiment.ExperimentID, "actor")
	if err != nil {
		t.Fatal(err)
	}
	want, err := core.PredictionSetDigest([]core.PredictionBinding{prediction})
	if err != nil {
		t.Fatal(err)
	}
	if seal.SealedPredictionDigest != want || seal.BaseProjectionCutRef.CanonicalRecordHighWater != before || seal.BaseProjectionCutRef.EpisodeID != "ep-1" {
		t.Fatalf("seal=%#v before=%d want=%s", seal, before, want)
	}
}

func TestProjectedExperimentLifecycleIsDerivedFromRecords(t *testing.T) {
	svc, ledger, _, _, experiment, prediction := task4ExperimentService(t)
	projection, err := svc.Inspect(context.Background(), "ep-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if state := experimentStateForTest(projection, experiment.ExperimentID); state != core.ExperimentDefined {
		t.Fatalf("defined state=%q", state)
	}
	if _, _, err := svc.SealExperiment(context.Background(), experiment.ExperimentID, "actor"); err != nil {
		t.Fatal(err)
	}
	projection, _ = svc.Inspect(context.Background(), "ep-1", "")
	if state := experimentStateForTest(projection, experiment.ExperimentID); state != core.ExperimentSealed {
		t.Fatalf("sealed state=%q", state)
	}
	link := core.ExperimentExecutionLink{SchemaVersion: 1, LinkID: "link-1", ExperimentID: experiment.ExperimentID, OperationID: "op-1", SessionID: "sess-1", WorkspaceID: dpWorkspaceID, SourceGeneration: prediction.SourceGeneration, AcceptedRequestFingerprint: strings.Repeat("a", 64), AcceptedExecutionFingerprint: strings.Repeat("b", 64), AcceptedObservationBindingFingerprint: strings.Repeat("c", 64), AdmittedAt: time.Unix(20, 0).UTC()}
	if _, err := ledger.append(core.RecordExperimentExecutionLink, link); err != nil {
		t.Fatal(err)
	}
	projection, _ = svc.Inspect(context.Background(), "ep-1", "")
	if state := experimentStateForTest(projection, experiment.ExperimentID); state != core.ExperimentObserving {
		t.Fatalf("observing state=%q", state)
	}
	if _, err := svc.AbortExperiment(context.Background(), experiment.ExperimentID, core.AbortAfterExecutionLink, "stop", "actor"); err != nil {
		t.Fatal(err)
	}
	projection, _ = svc.Inspect(context.Background(), "ep-1", "")
	if state := experimentStateForTest(projection, experiment.ExperimentID); state != core.ExperimentAborted {
		t.Fatalf("aborted state=%q", state)
	}
}

func experimentStateForTest(projection core.DecisionProjection, id core.ExperimentID) core.ExperimentLifecycleState {
	for _, experiment := range projection.Experiments {
		if experiment.ExperimentID == id {
			return experiment.State
		}
	}
	return ""
}

func TestProjectedExperimentLifecycleClosed(t *testing.T) {
	svc, ledger, _, _, experiment, prediction := task4ExperimentService(t)
	if _, _, err := svc.SealExperiment(context.Background(), experiment.ExperimentID, "actor"); err != nil {
		t.Fatal(err)
	}
	link := core.ExperimentExecutionLink{SchemaVersion: 1, LinkID: "link-close", ExperimentID: experiment.ExperimentID, OperationID: "op-close", SessionID: "sess-close", WorkspaceID: dpWorkspaceID, SourceGeneration: prediction.SourceGeneration, AcceptedRequestFingerprint: strings.Repeat("a", 64), AcceptedExecutionFingerprint: strings.Repeat("b", 64), AcceptedObservationBindingFingerprint: strings.Repeat("c", 64), AdmittedAt: time.Unix(20, 0).UTC()}
	if _, err := ledger.append(core.RecordExperimentExecutionLink, link); err != nil {
		t.Fatal(err)
	}
	binding := core.ExperimentObservationBinding{SchemaVersion: 1, BindingID: "bind-close", ExperimentID: experiment.ExperimentID, OperationID: "op-close", SourceGeneration: prediction.SourceGeneration, ObservationSemanticsVersion: 1, DerivationCutDigest: "cut_" + strings.Repeat("d", 64), PredictionResults: []core.PredictionResult{{PredictionID: prediction.PredictionID, Status: core.PredictionMatch}}, MaterializedAt: time.Unix(21, 0).UTC()}
	if _, err := ledger.append(core.RecordExperimentObservationBinding, binding); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CloseExperiment(context.Background(), experiment.ExperimentID, "actor"); err != nil {
		t.Fatal(err)
	}
	projection, err := svc.Inspect(context.Background(), "ep-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if state := experimentStateForTest(projection, experiment.ExperimentID); state != core.ExperimentClosed {
		t.Fatalf("closed state=%q", state)
	}
}

func setupTwoCandidateDiscrimination(t *testing.T) (*Service, *fakeEpisodeLedger, core.Experiment, core.PredictionBinding, core.PredictionBinding, string) {
	t.Helper()
	policy := &fakePolicyStore{}
	currentDPPolicy(t, policy)
	ledger := &fakeEpisodeLedger{}
	ws, snap := validDPWorkspaceAndSnapshot(t, "a")
	experiments := &panicExecutionMutationStore{fakeExperimentMutationStore: fakeExperimentMutationStore{ledger: ledger}}
	svc := NewService(policy, nil, EpisodeDependencies{Mutations: ledger, Experiments: experiments, Ledger: ledger, Workspaces: fakeDPWorkspaceInspector{ws}, Snapshots: fakeDPSourceSnapshotter{snap}})
	if _, err := svc.CreateEpisode(context.Background(), CreateEpisodeRequest{EpisodeID: "ep-1", Kind: core.EpisodeDiagnosis, RepositoryID: dpRepoID, WorkspaceID: dpWorkspaceID, ActorRef: "actor"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []core.CandidateID{"a", "b"} {
		if _, err := svc.CreateCandidate(context.Background(), core.Candidate{CandidateID: id, EpisodeID: "ep-1", SemanticClaim: string(id), DeclaredByActorRef: "actor", DeclaredAt: time.Unix(2, 0).UTC()}); err != nil {
			t.Fatal(err)
		}
	}
	experiment := core.Experiment{SchemaVersion: 1, ExperimentID: "exp-1", EpisodeID: "ep-1", DeclaredByActorRef: "actor", DeclaredAt: time.Unix(3, 0).UTC()}
	if _, err := svc.DefineExperiment(context.Background(), experiment); err != nil {
		t.Fatal(err)
	}
	a := discriminatorPrediction("pa", "a", "TestRace", core.StructuredTestPass)
	b := discriminatorPrediction("pb", "b", "TestRace", core.StructuredTestFail)
	a.SourceGeneration = snap.Generation
	b.SourceGeneration = snap.Generation
	if _, err := svc.BindPrediction(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.BindPrediction(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	return svc, ledger, experiment, a, b, snap.Generation
}

func appendRequiredMismatchForB(t *testing.T, ledger *fakeEpisodeLedger, source string) {
	t.Helper()
	blockExp := core.Experiment{SchemaVersion: 1, ExperimentID: "exp-block", EpisodeID: "ep-1", DeclaredByActorRef: "actor", DeclaredAt: time.Unix(30, 0).UTC()}
	if _, err := ledger.append(core.RecordExperiment, blockExp); err != nil {
		t.Fatal(err)
	}
	required := core.PredictionBinding{PredictionID: "p-block", EpisodeID: "ep-1", ExperimentID: "exp-block", CandidateID: "b", Role: core.PredictionRequired, Predicate: core.ObservationPredicate{Kind: core.PredicateOperationOutcome, OperationOutcome: &core.OperationOutcomePredicate{ExpectedOutcome: core.OperationSuccess}}, SourceGeneration: source, CommittedAt: time.Unix(31, 0).UTC()}
	if _, err := ledger.append(core.RecordPredictionBinding, required); err != nil {
		t.Fatal(err)
	}
	binding := core.ExperimentObservationBinding{SchemaVersion: 1, BindingID: "bind-block", ExperimentID: "exp-block", OperationID: "op-block", SourceGeneration: source, ObservationSemanticsVersion: 1, DerivationCutDigest: "cut_" + strings.Repeat("f", 64), PredictionResults: []core.PredictionResult{{PredictionID: "p-block", Status: core.PredictionMismatch}}, MaterializedAt: time.Unix(32, 0).UTC()}
	if _, err := ledger.append(core.RecordExperimentObservationBinding, binding); err != nil {
		t.Fatal(err)
	}
}

func TestSealExcludesAlreadyRequiredMismatchBlockedChallenger(t *testing.T) {
	svc, ledger, experiment, _, _, source := setupTwoCandidateDiscrimination(t)
	appendRequiredMismatchForB(t, ledger, source)
	seal, _, err := svc.SealExperiment(context.Background(), experiment.ExperimentID, "actor")
	if err != nil {
		t.Fatal(err)
	}
	if len(seal.PotentialDiscriminationPairs) != 0 {
		t.Fatalf("pairs=%#v", seal.PotentialDiscriminationPairs)
	}
}

func TestSealPotentialDiscriminationRemainsFrozenAfterLaterBlocker(t *testing.T) {
	svc, ledger, experiment, _, _, source := setupTwoCandidateDiscrimination(t)
	seal, _, err := svc.SealExperiment(context.Background(), experiment.ExperimentID, "actor")
	if err != nil {
		t.Fatal(err)
	}
	if len(seal.PotentialDiscriminationPairs) != 2 {
		t.Fatalf("initial pairs=%#v", seal.PotentialDiscriminationPairs)
	}
	appendRequiredMismatchForB(t, ledger, source)
	replay, _, err := svc.SealExperiment(context.Background(), experiment.ExperimentID, "actor")
	if err != nil {
		t.Fatal(err)
	}
	if len(replay.PotentialDiscriminationPairs) != 2 || replay.BaseProjectionCutRef != seal.BaseProjectionCutRef {
		t.Fatalf("replay=%#v seal=%#v", replay, seal)
	}
}
