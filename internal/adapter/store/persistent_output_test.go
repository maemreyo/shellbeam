package store

import (
	"context"
	"errors"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func TestReconcilePersistentOutputUnseenReplayAndPartialOverlap(t *testing.T) {
	r := openPersistentOutputRepo(t, 64)
	ctx := context.Background()

	first, result := r.ReconcilePersistentOutput(ctx, "persistent-output", 0, []byte("hello"))
	if result.Err != nil || first.CanonicalExtent != 5 || first.AppendedBytes != 5 || first.Replay {
		t.Fatalf("first=%#v result=%#v", first, result)
	}
	replay, result := r.ReconcilePersistentOutput(ctx, "persistent-output", 0, []byte("hello"))
	if result.Err != nil || replay.CanonicalExtent != 5 || replay.AppendedBytes != 0 || !replay.Replay || result.ObservationSeq != 0 {
		t.Fatalf("replay=%#v result=%#v", replay, result)
	}
	partial, result := r.ReconcilePersistentOutput(ctx, "persistent-output", 3, []byte("lo!"))
	if result.Err != nil || partial.CanonicalExtent != 6 || partial.AppendedBytes != 1 || partial.Replay {
		t.Fatalf("partial=%#v result=%#v", partial, result)
	}
	got, next, err := r.ReadOutput(ctx, "persistent-output", 0, 64)
	if err != nil || string(got) != "hello!" || next != 6 {
		t.Fatalf("output=%q next=%d err=%v", got, next, err)
	}
}

func TestReconcilePersistentOutputRejectsMismatchGapAndLimit(t *testing.T) {
	r := openPersistentOutputRepo(t, 6)
	ctx := context.Background()
	if _, result := r.ReconcilePersistentOutput(ctx, "persistent-output", 0, []byte("hello")); result.Err != nil {
		t.Fatal(result.Err)
	}
	for name, tc := range map[string]struct {
		offset int64
		data   string
		reason string
	}{
		"mismatch": {1, "X", "overlap_mismatch"},
		"gap":      {6, "!", "gap"},
		"limit":    {5, "!!", "output_limit"},
	} {
		t.Run(name, func(t *testing.T) {
			_, result := r.ReconcilePersistentOutput(ctx, "persistent-output", tc.offset, []byte(tc.data))
			if !errors.Is(result.Err, failure.PersistentRecoveryOutputConflict) {
				t.Fatalf("result=%#v", result)
			}
			var typed *failure.Failure
			if !errors.As(result.Err, &typed) || typed.Details["reason"] != tc.reason {
				t.Fatalf("failure=%#v want reason=%q", typed, tc.reason)
			}
		})
	}
	got, next, err := r.ReadOutput(ctx, "persistent-output", 0, 64)
	if err != nil || string(got) != "hello" || next != 5 {
		t.Fatalf("output changed after conflicts: %q next=%d err=%v", got, next, err)
	}
}

func TestReconcilePersistentOutputAppendSuccessAckLostRetryDoesNotDuplicate(t *testing.T) {
	r := openPersistentOutputRepo(t, 64)
	ctx := context.Background()
	if _, result := r.ReconcilePersistentOutput(ctx, "persistent-output", 0, []byte("abcdef")); result.Err != nil {
		t.Fatal(result.Err)
	}
	// Simulate canonical append succeeding while the caller loses the response.
	replay, result := r.ReconcilePersistentOutput(ctx, "persistent-output", 0, []byte("abcdef"))
	if result.Err != nil || !replay.Replay || replay.AppendedBytes != 0 || replay.CanonicalExtent != 6 {
		t.Fatalf("replay=%#v result=%#v", replay, result)
	}
	got, _, err := r.ReadOutput(ctx, "persistent-output", 0, 64)
	if err != nil || string(got) != "abcdef" {
		t.Fatalf("output=%q err=%v", got, err)
	}
}

func openPersistentOutputRepo(t *testing.T, maxOutput int64) *Repository {
	t.Helper()
	r, err := Open(t.TempDir()+"/state", Limits{MaxSessions: 4, MaxSessionOutput: maxOutput, MaxTotalState: 1 << 20, ControlReserve: 1024})
	if err != nil {
		t.Fatal(err)
	}
	reservation := operation.Reservation{SchemaVersion: 1, OperationID: "persistent-output-op", SessionID: "persistent-output", Fingerprint: "fp", Command: "true", CWD: "/", Shell: "/bin/sh", DaemonIncarnation: "daemon"}
	if _, created, result := r.ReserveOperation(context.Background(), reservation); result.Err != nil || !created {
		t.Fatalf("reserve created=%v result=%#v", created, result)
	}
	return r
}
