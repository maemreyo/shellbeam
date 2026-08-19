package store

import (
	"context"
	"path/filepath"
	"strings"
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
