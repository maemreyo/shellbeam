package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	observationapp "github.com/maemreyo/shellbeam/internal/app/observation"
	"github.com/maemreyo/shellbeam/internal/core/observation"
)

func TestObservationCommitTransitionFailureQueuesExactLiveRetry(t *testing.T) {
	r := openObservationRepository(t, filepath.Join(t.TempDir(), "state"))
	prepared, result := r.PrepareObservation(context.Background(), observationRequest("subject:commit-retry"))
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	retryWakeups := r.ObservationTransitionRetryWakeups()
	r.writer = failNthAtomicWriter("replace.rename", 1)
	failed := r.CommitObservationSequence(context.Background(), uint64(prepared.Obligation.ChangeSeq))
	if failed.Err == nil {
		t.Fatal("expected injected commit transition failure")
	}
	select {
	case <-retryWakeups:
	case <-time.After(time.Second):
		t.Fatal("failed commit transition did not enqueue a live retry")
	}
	remaining, err := r.RetryObservationTransitions(context.Background())
	if err != nil || remaining != 0 {
		t.Fatalf("retry remaining=%d err=%v", remaining, err)
	}
	got, err := r.readObservation(prepared.Obligation.ChangeSeq)
	if err != nil || got.State != observation.ObligationCommitted {
		t.Fatalf("obligation=%#v err=%v", got, err)
	}
	materialized, err := observationapp.NewMaterializer(r).Materialize(context.Background())
	if err != nil || materialized.PreparedGapAt != 0 || materialized.State.MaterializedThroughSeq != prepared.Obligation.ChangeSeq {
		t.Fatalf("materialized=%#v err=%v", materialized, err)
	}
}

func TestObservationAbortTransitionFailureQueuesExactLiveRetry(t *testing.T) {
	r := openObservationRepository(t, filepath.Join(t.TempDir(), "state"))
	prepared, result := r.PrepareObservation(context.Background(), observationRequest("subject:abort-retry"))
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	r.writer = failNthAtomicWriter("replace.rename", 1)
	failed := r.AbortObservationSequence(context.Background(), uint64(prepared.Obligation.ChangeSeq), observationAbortWriteFailed)
	if failed.Err == nil {
		t.Fatal("expected injected abort transition failure")
	}
	remaining, err := r.RetryObservationTransitions(context.Background())
	if err != nil || remaining != 0 {
		t.Fatalf("retry remaining=%d err=%v", remaining, err)
	}
	got, err := r.readObservation(prepared.Obligation.ChangeSeq)
	if err != nil || got.State != observation.ObligationAborted || got.AbortReason != observationAbortWriteFailed {
		t.Fatalf("obligation=%#v err=%v", got, err)
	}
	materialized, err := observationapp.NewMaterializer(r).Materialize(context.Background())
	if err != nil || materialized.PreparedGapAt != 0 || materialized.State.MaterializedThroughSeq != prepared.Obligation.ChangeSeq {
		t.Fatalf("materialized=%#v err=%v", materialized, err)
	}
}

func TestObservationLiveRetryDoesNotReconcileLegitimatePreparedObligation(t *testing.T) {
	r := openObservationRepository(t, filepath.Join(t.TempDir(), "state"))
	prepared, result := r.PrepareObservation(context.Background(), observationRequest("subject:in-flight"))
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	remaining, err := r.RetryObservationTransitions(context.Background())
	if err != nil || remaining != 0 {
		t.Fatalf("retry remaining=%d err=%v", remaining, err)
	}
	got, err := r.readObservation(prepared.Obligation.ChangeSeq)
	if err != nil || got.State != observation.ObligationPrepared {
		t.Fatalf("legitimate prepared obligation mutated: %#v err=%v", got, err)
	}
}

func TestProcessStartedSequenceCommitFailureQueuesLiveRetry(t *testing.T) {
	r := openObservationRepository(t, filepath.Join(t.TempDir(), "state"))
	prepared := r.PrepareProcessStartedObservation(context.Background(), "obs-op", "obs-session")
	if prepared.Err != nil || prepared.ObservationSeq == 0 {
		t.Fatalf("prepare=%#v", prepared)
	}
	r.writer = failNthAtomicWriter("replace.rename", 1)
	failed := r.CommitObservationSequence(context.Background(), prepared.ObservationSeq)
	if failed.Err == nil {
		t.Fatal("expected injected process-start transition failure")
	}
	remaining, err := r.RetryObservationTransitions(context.Background())
	if err != nil || remaining != 0 {
		t.Fatalf("retry remaining=%d err=%v", remaining, err)
	}
	got, err := r.readObservation(observation.ChangeSeq(prepared.ObservationSeq))
	if err != nil || got.State != observation.ObligationCommitted || got.Kind != observation.EventProcessStarted {
		t.Fatalf("obligation=%#v err=%v", got, err)
	}
}
