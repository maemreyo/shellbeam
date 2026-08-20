package store

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	dp "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
)

func dpExperiment(id, ep string) dp.Experiment {
	return dp.Experiment{SchemaVersion: 1, ExperimentID: dp.ExperimentID(id), EpisodeID: dp.EpisodeID(ep), DeclaredByActorRef: "actor", DeclaredAt: time.Unix(10, 0).UTC()}
}
func dpPrediction(id, ep, exp, cand string, status dp.StructuredTestStatus) dp.PredictionBinding {
	return dp.PredictionBinding{PredictionID: dp.PredictionID(id), EpisodeID: dp.EpisodeID(ep), ExperimentID: dp.ExperimentID(exp), CandidateID: dp.CandidateID(cand), Role: dp.PredictionDiscriminator, Predicate: dp.ObservationPredicate{Kind: dp.PredicateStructuredTestStatus, StructuredTestStatus: &dp.StructuredTestStatusPredicate{Target: dp.StructuredTargetTestCase, Package: "pkg", Name: "TestRace", ExpectedStatus: status}}, SourceGeneration: "gen_" + strings.Repeat("a", 64), CommittedAt: time.Unix(11, 0).UTC()}
}

func TestPredictionBindAfterSealReturnsExperimentAlreadySealed(t *testing.T) {
	r := openDecisionProtocolRepo(t, filepath.Join(t.TempDir(), "state"))
	store := NewDecisionProtocolStore(r)
	ctx := context.Background()
	ep := dpStoredEpisode("ep-1")
	ep.Baseline.SourceGeneration = "gen_" + strings.Repeat("a", 64)
	if _, _, err := store.CreateEpisode(ctx, ep); err != nil {
		t.Fatal(err)
	}
	cand := dpCandidate("cand-a", "ep-1", "")
	if _, _, err := store.CreateCandidate(ctx, cand); err != nil {
		t.Fatal(err)
	}
	exp := dpExperiment("exp-1", "ep-1")
	if _, _, err := store.DefineExperiment(ctx, exp); err != nil {
		t.Fatal(err)
	}
	p := dpPrediction("pred-1", "ep-1", "exp-1", "cand-a", dp.StructuredTestPass)
	if _, _, err := store.BindPrediction(ctx, p); err != nil {
		t.Fatal(err)
	}
	digest, err := dp.PredictionSetDigest([]dp.PredictionBinding{p})
	if err != nil {
		t.Fatal(err)
	}
	hw, _ := store.CurrentHighWater(ctx)
	seal := dp.ExperimentSeal{ExperimentID: "exp-1", SourceGeneration: ep.Baseline.SourceGeneration, SealedPredictionDigest: digest, BaseProjectionCutRef: dp.DecisionProjectionCutRef{EpisodeID: "ep-1", CanonicalRecordHighWater: hw}, BaseCandidateProjectionDigest: "proj_" + strings.Repeat("b", 64), SealedAt: time.Unix(12, 0).UTC()}
	if _, _, err := store.SealExperimentCAS(ctx, seal); err != nil {
		t.Fatal(err)
	}
	_, _, err = store.BindPrediction(ctx, dpPrediction("pred-2", "ep-1", "exp-1", "cand-a", dp.StructuredTestFail))
	reason, ok := dp.ReasonOf(err)
	if !ok || reason != dp.ReasonExperimentAlreadySealed {
		t.Fatalf("err=%v reason=%q", err, reason)
	}
}

func TestSealCASReplaysIdenticalAndRejectsDifferentSeal(t *testing.T) {
	r := openDecisionProtocolRepo(t, filepath.Join(t.TempDir(), "state"))
	store := NewDecisionProtocolStore(r)
	ctx := context.Background()
	ep := dpStoredEpisode("ep-1")
	ep.Baseline.SourceGeneration = "gen_" + strings.Repeat("a", 64)
	if _, _, err := store.CreateEpisode(ctx, ep); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.DefineExperiment(ctx, dpExperiment("exp-1", "ep-1")); err != nil {
		t.Fatal(err)
	}
	p := dpPrediction("pred-1", "ep-1", "exp-1", "cand-a", dp.StructuredTestPass)
	if _, err := store.AppendRecord(ctx, dp.RecordCandidate, dpCandidate("cand-a", "ep-1", "")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.BindPrediction(ctx, p); err != nil {
		t.Fatal(err)
	}
	digest, _ := dp.PredictionSetDigest([]dp.PredictionBinding{p})
	hw, _ := store.CurrentHighWater(ctx)
	seal := dp.ExperimentSeal{ExperimentID: "exp-1", SourceGeneration: ep.Baseline.SourceGeneration, SealedPredictionDigest: digest, BaseProjectionCutRef: dp.DecisionProjectionCutRef{EpisodeID: "ep-1", CanonicalRecordHighWater: hw}, BaseCandidateProjectionDigest: "proj_" + strings.Repeat("b", 64), SealedAt: time.Unix(12, 0).UTC()}
	first, created, err := store.SealExperimentCAS(ctx, seal)
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	replay, created, err := store.SealExperimentCAS(ctx, seal)
	if err != nil || created || replay.CanonicalRecordSeq != first.CanonicalRecordSeq {
		t.Fatalf("replay=%#v created=%v err=%v", replay, created, err)
	}
	different := seal
	different.BaseCandidateProjectionDigest = "proj_" + strings.Repeat("c", 64)
	if _, _, err := store.SealExperimentCAS(ctx, different); err == nil {
		t.Fatal("different second seal accepted")
	}
}

func TestExperimentCloseAndAbortAreMutuallyExclusive(t *testing.T) {
	r := openDecisionProtocolRepo(t, filepath.Join(t.TempDir(), "state"))
	store := NewDecisionProtocolStore(r)
	ctx := context.Background()
	ep := dpStoredEpisode("ep-1")
	if _, _, err := store.CreateEpisode(ctx, ep); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.DefineExperiment(ctx, dpExperiment("exp-1", "ep-1")); err != nil {
		t.Fatal(err)
	}
	abort := dp.ExperimentAbort{SchemaVersion: 1, AbortID: "abort-1", ExperimentID: "exp-1", Phase: dp.AbortBeforeExecution, Reason: "stop", AbortedByActorRef: "actor", AbortedAt: time.Unix(20, 0).UTC()}
	if _, _, err := store.AbortExperimentCAS(ctx, abort); err != nil {
		t.Fatal(err)
	}
	closure := dp.ExperimentClosure{SchemaVersion: 1, ClosureID: "close-1", ExperimentID: "exp-1", ObservationBindingID: "bind-1", ClosedByActorRef: "actor", ClosedAt: time.Unix(21, 0).UTC()}
	if _, _, err := store.CloseExperimentCAS(ctx, closure); err == nil {
		t.Fatal("closure accepted after abort")
	}
}

func TestSealCASRejectsStaleCanonicalProjectionCut(t *testing.T) {
	r := openDecisionProtocolRepo(t, filepath.Join(t.TempDir(), "state"))
	store := NewDecisionProtocolStore(r)
	ctx := context.Background()
	ep := dpStoredEpisode("ep-1")
	ep.Baseline.SourceGeneration = "gen_" + strings.Repeat("a", 64)
	if _, _, err := store.CreateEpisode(ctx, ep); err != nil {
		t.Fatal(err)
	}
	cand := dpCandidate("cand-a", "ep-1", "")
	if _, _, err := store.CreateCandidate(ctx, cand); err != nil {
		t.Fatal(err)
	}
	exp := dpExperiment("exp-1", "ep-1")
	if _, _, err := store.DefineExperiment(ctx, exp); err != nil {
		t.Fatal(err)
	}
	p := dpPrediction("pred-1", "ep-1", "exp-1", "cand-a", dp.StructuredTestPass)
	if _, _, err := store.BindPrediction(ctx, p); err != nil {
		t.Fatal(err)
	}
	digest, _ := dp.PredictionSetDigest([]dp.PredictionBinding{p})
	cut, _ := store.CurrentHighWater(ctx)
	if _, _, err := store.CreateCandidate(ctx, dpCandidate("cand-b", "ep-1", "")); err != nil {
		t.Fatal(err)
	}
	seal := dp.ExperimentSeal{ExperimentID: "exp-1", SourceGeneration: ep.Baseline.SourceGeneration, SealedPredictionDigest: digest, BaseProjectionCutRef: dp.DecisionProjectionCutRef{EpisodeID: "ep-1", CanonicalRecordHighWater: cut}, BaseCandidateProjectionDigest: "proj_" + strings.Repeat("b", 64), SealedAt: time.Unix(12, 0).UTC()}
	_, _, err := store.SealExperimentCAS(ctx, seal)
	reason, ok := dp.ReasonOf(err)
	if !ok || reason != dp.ReasonProjectionConflict {
		t.Fatalf("err=%v reason=%q", err, reason)
	}
}

func TestExperimentSealVsPredictionBindRaceIsEpistemicallyExact(t *testing.T) {
	r := openDecisionProtocolRepo(t, filepath.Join(t.TempDir(), "state"))
	store := NewDecisionProtocolStore(r)
	ctx := context.Background()
	ep := dpStoredEpisode("ep-seal-bind-race")
	ep.Baseline.SourceGeneration = "gen_" + strings.Repeat("a", 64)
	if _, _, err := store.CreateEpisode(ctx, ep); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.CreateCandidate(ctx, dpCandidate("cand-a", string(ep.EpisodeID), "")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.DefineExperiment(ctx, dpExperiment("exp-seal-bind-race", string(ep.EpisodeID))); err != nil {
		t.Fatal(err)
	}
	first := dpPrediction("pred-first", string(ep.EpisodeID), "exp-seal-bind-race", "cand-a", dp.StructuredTestPass)
	if _, _, err := store.BindPrediction(ctx, first); err != nil {
		t.Fatal(err)
	}
	firstDigest, err := dp.PredictionSetDigest([]dp.PredictionBinding{first})
	if err != nil {
		t.Fatal(err)
	}
	cut, err := store.CurrentHighWater(ctx)
	if err != nil {
		t.Fatal(err)
	}
	seal := dp.ExperimentSeal{ExperimentID: "exp-seal-bind-race", SourceGeneration: ep.Baseline.SourceGeneration, SealedPredictionDigest: firstDigest, BaseProjectionCutRef: dp.DecisionProjectionCutRef{EpisodeID: ep.EpisodeID, CanonicalRecordHighWater: cut}, BaseCandidateProjectionDigest: "proj_" + strings.Repeat("b", 64), SealedAt: time.Unix(12, 0).UTC()}
	second := dpPrediction("pred-racing", string(ep.EpisodeID), "exp-seal-bind-race", "cand-a", dp.StructuredTestFail)
	start := make(chan struct{})
	sealErr, bindErr := make(chan error, 1), make(chan error, 1)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); <-start; _, _, err := store.SealExperimentCAS(ctx, seal); sealErr <- err }()
	go func() { defer wg.Done(); <-start; _, _, err := store.BindPrediction(ctx, second); bindErr <- err }()
	close(start)
	wg.Wait()
	sErr, bErr := <-sealErr, <-bindErr
	if (sErr == nil) == (bErr == nil) {
		t.Fatalf("seal_err=%v bind_err=%v", sErr, bErr)
	}
	if sErr == nil {
		if reason, ok := dp.ReasonOf(bErr); !ok || reason != dp.ReasonExperimentAlreadySealed {
			t.Fatalf("bind err=%v reason=%q", bErr, reason)
		}
		return
	}
	// If the racing bind wins, the original seal must fail closed. The store may
	// detect this either as a prediction-set mismatch or as a stale projection
	// cut; both are safe because no incomplete seal becomes durable.
	latest, err := store.CurrentHighWater(ctx)
	if err != nil {
		t.Fatal(err)
	}
	bothDigest, err := dp.PredictionSetDigest([]dp.PredictionBinding{first, second})
	if err != nil {
		t.Fatal(err)
	}
	seal.SealedPredictionDigest = bothDigest
	seal.BaseProjectionCutRef.CanonicalRecordHighWater = latest
	seal.SealedAt = time.Unix(13, 0).UTC()
	if _, _, err := store.SealExperimentCAS(ctx, seal); err != nil {
		t.Fatalf("seal after winning bind: %v", err)
	}
}

func TestExperimentCloseVsAbortRaceHasOneTerminalRecord(t *testing.T) {
	f := setupObservationFixture(t, filepath.Join(t.TempDir(), "state"), "exp-close-abort-race")
	binding := observationBinding(f, "bind-close-abort-race", "cut_"+strings.Repeat("7", 64), time.Unix(30, 0))
	stored, _, err := f.repo.MaterializeExperimentObservationCAS(context.Background(), binding)
	if err != nil {
		t.Fatal(err)
	}
	closure := dp.ExperimentClosure{SchemaVersion: 1, ClosureID: "close-race", ExperimentID: f.experiment.ExperimentID, ObservationBindingID: stored.BindingID, ClosedByActorRef: "actor", ClosedAt: time.Unix(31, 0).UTC()}
	abort := dp.ExperimentAbort{SchemaVersion: 1, AbortID: "abort-race", ExperimentID: f.experiment.ExperimentID, Phase: dp.AbortAfterExecutionLink, ExecutionLinkID: f.link.LinkID, Reason: "race", AbortedByActorRef: "actor", AbortedAt: time.Unix(31, 0).UTC()}
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, _, err := f.store.CloseExperimentCAS(context.Background(), closure)
		errs <- err
	}()
	go func() {
		defer wg.Done()
		<-start
		_, _, err := f.store.AbortExperimentCAS(context.Background(), abort)
		errs <- err
	}()
	close(start)
	wg.Wait()
	close(errs)
	success, rejected := 0, 0
	for err := range errs {
		if err == nil {
			success++
		} else {
			rejected++
		}
	}
	if success != 1 || rejected != 1 {
		t.Fatalf("success=%d rejected=%d", success, rejected)
	}
	hw, err := f.store.CurrentHighWater(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	records, err := f.store.ListEpisodeRecords(context.Background(), f.episode.EpisodeID, hw)
	if err != nil {
		t.Fatal(err)
	}
	terminal := 0
	for _, record := range records {
		if record.Kind == dp.RecordExperimentClosure || record.Kind == dp.RecordExperimentAbort {
			terminal++
		}
	}
	if terminal != 1 {
		t.Fatalf("experiment terminal records=%d", terminal)
	}
}
