package store

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	dp "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

type observationFixture struct {
	repo       *Repository
	store      *DecisionProtocolStore
	episode    dp.Episode
	experiment dp.Experiment
	prediction dp.PredictionBinding
	link       dp.ExperimentExecutionLink
}

func setupObservationFixture(t *testing.T, root, experimentID string) observationFixture {
	t.Helper()
	r := openDecisionProtocolRepo(t, root)
	store := NewDecisionProtocolStore(r)
	ctx := context.Background()
	ep := dpStoredEpisode("ep-observation")
	ep.WorkspaceID = "ws_01M0CJX5KTQFA7JCHCRVC8SHFV"
	ep.Baseline.SourceGeneration = "gen_" + strings.Repeat("a", 64)
	if _, _, err := store.CreateEpisode(ctx, ep); err != nil {
		t.Fatal(err)
	}
	candidate := dpCandidate("cand-observation", string(ep.EpisodeID), "")
	if _, _, err := store.CreateCandidate(ctx, candidate); err != nil {
		t.Fatal(err)
	}
	experiment := dpExperiment(experimentID, string(ep.EpisodeID))
	if _, _, err := store.DefineExperiment(ctx, experiment); err != nil {
		t.Fatal(err)
	}
	prediction := dpPrediction("pred-observation", string(ep.EpisodeID), experimentID, string(candidate.CandidateID), dp.StructuredTestPass)
	prediction.Role = dp.PredictionRequired
	if _, _, err := store.BindPrediction(ctx, prediction); err != nil {
		t.Fatal(err)
	}
	digest, err := dp.PredictionSetDigest([]dp.PredictionBinding{prediction})
	if err != nil {
		t.Fatal(err)
	}
	hw, err := store.CurrentHighWater(ctx)
	if err != nil {
		t.Fatal(err)
	}
	seal := dp.ExperimentSeal{ExperimentID: experiment.ExperimentID, SourceGeneration: ep.Baseline.SourceGeneration, SealedPredictionDigest: digest, BaseProjectionCutRef: dp.DecisionProjectionCutRef{EpisodeID: ep.EpisodeID, CanonicalRecordHighWater: hw}, BaseCandidateProjectionDigest: "proj_" + strings.Repeat("b", 64), SealedAt: time.Unix(20, 0).UTC()}
	if _, _, err := store.SealExperimentCAS(ctx, seal); err != nil {
		t.Fatal(err)
	}
	obsFingerprint := experimentObservationFingerprint(t, experimentID)
	want := withExperiment(dpAdmissionReservation("op-observation", obsFingerprint), experimentID)
	_, link, created, result := r.ReserveExperimentOperation(ctx, want, dp.ExperimentExecutionLink{ExperimentID: experiment.ExperimentID})
	if result.Err != nil || !created {
		t.Fatalf("link created=%v result=%#v", created, result)
	}
	return observationFixture{repo: r, store: store, episode: ep, experiment: experiment, prediction: prediction, link: link}
}

func observationBinding(f observationFixture, bindingID, cut string, at time.Time) dp.ExperimentObservationBinding {
	return dp.ExperimentObservationBinding{
		SchemaVersion: 1, BindingID: dp.BindingID(bindingID), ExperimentID: f.experiment.ExperimentID,
		OperationID: f.link.OperationID, SourceGeneration: f.link.SourceGeneration, ObservationSemanticsVersion: 1,
		DerivationCutDigest: cut, PredictionResults: []dp.PredictionResult{{PredictionID: f.prediction.PredictionID, Status: dp.PredictionMatch, BasisRefs: []string{"receipt:" + f.link.OperationID}}}, MaterializedAt: at.UTC(),
	}
}

func TestObservationBindingCountNeverExceedsOne(t *testing.T) {
	f := setupObservationFixture(t, filepath.Join(t.TempDir(), "state"), "exp-observation-one")
	first := observationBinding(f, "bind-observation-a", "cut_"+strings.Repeat("1", 64), time.Unix(30, 0))
	stored, created, err := f.repo.MaterializeExperimentObservationCAS(context.Background(), first)
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	if stored.BindingID != first.BindingID {
		t.Fatalf("stored=%#v", stored)
	}
	assertExperimentObservationCount(t, f, 1)
}

func TestObservationMaterializationSameSemanticCutReplaysSameBinding(t *testing.T) {
	f := setupObservationFixture(t, filepath.Join(t.TempDir(), "state"), "exp-observation-replay")
	first := observationBinding(f, "bind-observation-a", "cut_"+strings.Repeat("2", 64), time.Unix(30, 0))
	stored, created, err := f.repo.MaterializeExperimentObservationCAS(context.Background(), first)
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	retry := observationBinding(f, "bind-observation-b", first.DerivationCutDigest, time.Unix(99, 0))
	replayed, created, err := f.repo.MaterializeExperimentObservationCAS(context.Background(), retry)
	if err != nil || created || replayed.BindingID != stored.BindingID || !replayed.MaterializedAt.Equal(stored.MaterializedAt) {
		t.Fatalf("replayed=%#v created=%v err=%v", replayed, created, err)
	}
	assertExperimentObservationCount(t, f, 1)
}

func TestObservationMaterializationDifferentCutReturnsConflict(t *testing.T) {
	f := setupObservationFixture(t, filepath.Join(t.TempDir(), "state"), "exp-observation-conflict")
	first := observationBinding(f, "bind-observation-a", "cut_"+strings.Repeat("3", 64), time.Unix(30, 0))
	if _, _, err := f.repo.MaterializeExperimentObservationCAS(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	second := observationBinding(f, "bind-observation-b", "cut_"+strings.Repeat("4", 64), time.Unix(31, 0))
	_, _, err := f.repo.MaterializeExperimentObservationCAS(context.Background(), second)
	reason, ok := dp.ReasonOf(err)
	if !ok || reason != dp.ReasonExperimentObservationBindingConflict {
		t.Fatalf("err=%v reason=%q", err, reason)
	}
	assertExperimentObservationCount(t, f, 1)
}

func TestObservationMaterializationConcurrentIdenticalCutCreatesOnePhysicalBinding(t *testing.T) {
	f := setupObservationFixture(t, filepath.Join(t.TempDir(), "state"), "exp-observation-race")
	cut := "cut_" + strings.Repeat("5", 64)
	var wg sync.WaitGroup
	results := make(chan dp.ExperimentObservationBinding, 2)
	errs := make(chan error, 2)
	for i, id := range []string{"bind-observation-a", "bind-observation-b"} {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			stored, _, err := f.repo.MaterializeExperimentObservationCAS(context.Background(), observationBinding(f, id, cut, time.Unix(int64(40+i), 0)))
			results <- stored
			errs <- err
		}(i, id)
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var bindingID dp.BindingID
	for result := range results {
		if bindingID == "" {
			bindingID = result.BindingID
		}
		if result.BindingID != bindingID {
			t.Fatalf("different replay bindings: %s vs %s", bindingID, result.BindingID)
		}
	}
	assertExperimentObservationCount(t, f, 1)
}

func TestObservationMaterializationRejectsIncompletePredictionSet(t *testing.T) {
	f := setupObservationFixture(t, filepath.Join(t.TempDir(), "state"), "exp-observation-predictions")
	binding := observationBinding(f, "bind-observation-a", "cut_"+strings.Repeat("6", 64), time.Unix(30, 0))
	binding.PredictionResults = []dp.PredictionResult{{PredictionID: "pred-other", Status: dp.PredictionMatch}}
	if _, _, err := f.repo.MaterializeExperimentObservationCAS(context.Background(), binding); err == nil {
		t.Fatal("incomplete prediction set accepted")
	}
	assertExperimentObservationCount(t, f, 0)
}

func assertExperimentObservationCount(t *testing.T, f observationFixture, want int) {
	t.Helper()
	hw, err := f.store.CurrentHighWater(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	records, err := f.store.ListEpisodeRecords(context.Background(), f.episode.EpisodeID, hw)
	if err != nil {
		t.Fatal(err)
	}
	got := 0
	for _, env := range records {
		if env.Kind == dp.RecordExperimentObservationBinding {
			got++
		}
	}
	if got != want {
		t.Fatalf("observation bindings=%d want=%d", got, want)
	}
}

var _ = operation.ID("")

func TestCloseAndPostAbortSettlementShareSameBindingCAS(t *testing.T) {
	t.Run("close", func(t *testing.T) {
		f := setupObservationFixture(t, filepath.Join(t.TempDir(), "state"), "exp-observation-close-shared")
		binding := observationBinding(f, "bind-observation-close", "cut_"+strings.Repeat("7", 64), time.Unix(50, 0))
		stored, _, err := f.repo.MaterializeExperimentObservationCAS(context.Background(), binding)
		if err != nil {
			t.Fatal(err)
		}
		closure := dp.ExperimentClosure{SchemaVersion: 1, ClosureID: "close-observation-shared", ExperimentID: f.experiment.ExperimentID, ObservationBindingID: stored.BindingID, ClosedByActorRef: "actor", ClosedAt: time.Unix(51, 0).UTC()}
		if _, _, err := f.store.CloseExperimentCAS(context.Background(), closure); err != nil {
			t.Fatal(err)
		}
		replayed, created, err := f.repo.MaterializeExperimentObservationCAS(context.Background(), observationBinding(f, "bind-observation-close-retry", binding.DerivationCutDigest, time.Unix(99, 0)))
		if err != nil || created || replayed.BindingID != stored.BindingID {
			t.Fatalf("replayed=%#v created=%v err=%v", replayed, created, err)
		}
		assertExperimentObservationCount(t, f, 1)
	})

	t.Run("post_abort", func(t *testing.T) {
		f := setupObservationFixture(t, filepath.Join(t.TempDir(), "state"), "exp-observation-abort-shared")
		abort := dp.ExperimentAbort{SchemaVersion: 1, AbortID: "abort-observation-shared", ExperimentID: f.experiment.ExperimentID, Phase: dp.AbortAfterExecutionLink, ExecutionLinkID: f.link.LinkID, Reason: "stop", AbortedByActorRef: "actor", AbortedAt: time.Unix(60, 0).UTC()}
		if _, _, err := f.store.AbortExperimentCAS(context.Background(), abort); err != nil {
			t.Fatal(err)
		}
		binding := observationBinding(f, "bind-observation-abort", "cut_"+strings.Repeat("8", 64), time.Unix(61, 0))
		stored, created, err := f.repo.MaterializeExperimentObservationCAS(context.Background(), binding)
		if err != nil || !created {
			t.Fatalf("created=%v err=%v", created, err)
		}
		replayed, created, err := f.repo.MaterializeExperimentObservationCAS(context.Background(), observationBinding(f, "bind-observation-abort-retry", binding.DerivationCutDigest, time.Unix(99, 0)))
		if err != nil || created || replayed.BindingID != stored.BindingID {
			t.Fatalf("replayed=%#v created=%v err=%v", replayed, created, err)
		}
		assertExperimentObservationCount(t, f, 1)
	})
}
