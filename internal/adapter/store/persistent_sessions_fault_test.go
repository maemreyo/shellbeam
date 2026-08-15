package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/failure"
)

func TestPersistentBindingRetryRepairsCrashAfterNameClaimBeforeBinding(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openRecoveryRepository(t, root)
	now := time.Date(2026, 8, 16, 5, 0, 0, 0, time.UTC)
	want := persistentBinding("persistent-session-a", "persistent-op-a", "dev-server", now)
	reservePersistentOperation(t, r, want.SessionID, want.OperationID, want.SessionName, now)

	// First create.link persists the name claim; second create.link is the binding publication.
	r.writer = failAtomicWriterNth("create.link", 2)
	if _, created, got := r.ReservePersistentBinding(context.Background(), want); got.Err == nil || created {
		t.Fatalf("faulted binding created=%v result=%#v", created, got)
	}

	r.writer = atomicWriter{}
	stored, created, got := r.ReservePersistentBinding(context.Background(), want)
	if got.Err != nil || !created || stored.SessionID != want.SessionID {
		t.Fatalf("repair created=%v stored=%#v result=%#v", created, stored, got)
	}
	if _, created, got := r.ReservePersistentBinding(context.Background(), want); got.Err != nil || created {
		t.Fatalf("post-repair replay created=%v result=%#v", created, got)
	}

	other := persistentBinding("persistent-session-b", "persistent-op-b", want.SessionName, now.Add(time.Second))
	reservePersistentOperation(t, r, other.SessionID, other.OperationID, other.SessionName, now.Add(time.Second))
	if _, created, got := r.ReservePersistentBinding(context.Background(), other); !errors.Is(got.Err, failure.PersistentSessionNameConflict) || created {
		t.Fatalf("orphan/repaired name claim allowed rebinding created=%v result=%#v", created, got)
	}
}
