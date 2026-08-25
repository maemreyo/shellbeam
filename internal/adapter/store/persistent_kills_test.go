package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	persistent "github.com/maemreyo/shellbeam/internal/core/persistentsession"
)

func TestPersistentKillLedgerReplaysConflictAndPersistsAcrossReopen(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openRecoveryRepository(t, root)
	now := time.Date(2026, 8, 25, 13, 0, 0, 0, time.UTC)
	binding := persistentBinding("persistent-kill-session", "persistent-kill-op", "persistent-kill", now)
	reservePersistentOperation(t, r, binding.SessionID, binding.OperationID, binding.SessionName, now)
	stored, created, got := r.ReservePersistentBinding(context.Background(), binding)
	if got.Err != nil || !created {
		t.Fatalf("binding created=%v stored=%#v result=%#v", created, stored, got)
	}
	live := stored
	live.Lifecycle = persistent.LifecycleLive
	live.UpdatedAt = now.Add(time.Second)
	if got := r.AdvancePersistentBinding(context.Background(), live); got.Err != nil {
		t.Fatalf("advance live=%#v", got)
	}

	record, created, got := r.ReservePersistentKill(context.Background(), operation.SessionID(binding.SessionID), "kill-replay-1", "TERM", false)
	if got.Err != nil || !created || record.Complete || !record.Needed {
		t.Fatalf("reserve created=%v record=%#v result=%#v", created, record, got)
	}
	record.Attempted, record.Succeeded, record.Complete = true, true, true
	completed, got := r.CompletePersistentKill(context.Background(), record)
	if got.Err != nil || !completed.Complete || !completed.Attempted || !completed.Succeeded {
		t.Fatalf("complete record=%#v result=%#v", completed, got)
	}
	replayed, created, got := r.ReservePersistentKill(context.Background(), operation.SessionID(binding.SessionID), "kill-replay-1", "TERM", true)
	if got.Err != nil || created || replayed != completed {
		t.Fatalf("replay created=%v record=%#v result=%#v", created, replayed, got)
	}
	if _, _, got := r.ReservePersistentKill(context.Background(), operation.SessionID(binding.SessionID), "kill-replay-1", "KILL", true); !errors.Is(got.Err, failure.OperationMetadataConflict) {
		t.Fatalf("changed signal result=%#v", got)
	}

	reopened := openRecoveryRepository(t, root)
	persisted, found, err := reopened.LookupPersistentKill(context.Background(), operation.SessionID(binding.SessionID), "kill-replay-1")
	if err != nil || !found || persisted != completed {
		t.Fatalf("reopened found=%v record=%#v err=%v", found, persisted, err)
	}
}

func TestPersistentKillLedgerTerminalAdmissionIsDurableNoop(t *testing.T) {
	r := openRecoveryRepository(t, filepath.Join(t.TempDir(), "state"))
	now := time.Date(2026, 8, 25, 13, 1, 0, 0, time.UTC)
	binding := persistentBinding("persistent-kill-terminal", "persistent-kill-terminal-op", "persistent-kill-terminal", now)
	reservePersistentOperation(t, r, binding.SessionID, binding.OperationID, binding.SessionName, now)
	if _, created, got := r.ReservePersistentBinding(context.Background(), binding); got.Err != nil || !created {
		t.Fatalf("binding created=%v result=%#v", created, got)
	}
	record, created, got := r.ReservePersistentKill(context.Background(), operation.SessionID(binding.SessionID), "kill-terminal-noop", "TERM", true)
	if got.Err != nil || !created || !record.Complete || record.Needed || record.Attempted || record.Succeeded {
		t.Fatalf("terminal reserve created=%v record=%#v result=%#v", created, record, got)
	}
}
