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
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func TestAbandonUnresolvedPreservesPersistentAndAbandonsDirect(t *testing.T) {
	r := openRecoveryRepository(t, filepath.Join(t.TempDir(), "state"))
	direct := operation.Reservation{
		SchemaVersion: 1, OperationID: "direct-restart-op", SessionID: "direct-restart-session",
		Fingerprint: "fp", Command: "sleep 10", CWD: "/tmp", Shell: "/bin/sh",
		DaemonIncarnation: "old-daemon", CreatedAt: time.Now().UTC(),
	}
	if _, created, result := r.ReserveOperation(context.Background(), direct); result.Err != nil || !created {
		t.Fatalf("direct reserve created=%v result=%#v", created, result)
	}
	now := time.Now().UTC().Add(time.Second)
	binding := persistentBinding("persistent-restart-session", "persistent-restart-op", "restart-server", now)
	reservePersistentOperationWithMetadata(t, r, binding, now)
	if _, created, result := r.ReservePersistentBinding(context.Background(), binding); result.Err != nil || !created {
		t.Fatalf("persistent binding created=%v result=%#v", created, result)
	}

	if err := r.AbandonUnresolved(context.Background(), "new-daemon"); err != nil {
		t.Fatal(err)
	}
	directReceipt, err := r.LoadReceipt(context.Background(), direct.SessionID)
	if err != nil || directReceipt.State != session.Abandoned || directReceipt.Outcome != session.Ambiguous {
		t.Fatalf("direct receipt=%#v err=%v", directReceipt, err)
	}
	if _, err := r.LoadReceipt(context.Background(), operation.SessionID(binding.SessionID)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("persistent receipt unexpectedly published: %v", err)
	}
	snap, err := r.LoadSession(context.Background(), operation.SessionID(binding.SessionID))
	if err != nil || snap.State.Terminal() {
		t.Fatalf("persistent snapshot=%#v err=%v", snap, err)
	}
}

func TestPersistentRecoveryCandidatesTrackOnlyProvisioningAndLive(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openRecoveryRepository(t, root)
	base := time.Date(2026, 8, 16, 1, 0, 0, 0, time.UTC)
	a := persistentBinding("persistent-recovery-a", "persistent-recovery-op-a", "recovery-a", base)
	b := persistentBinding("persistent-recovery-b", "persistent-recovery-op-b", "recovery-b", base.Add(time.Second))
	for _, binding := range []persistent.Binding{a, b} {
		reservePersistentOperationWithMetadata(t, r, binding, binding.CreatedAt)
		if _, created, result := r.ReservePersistentBinding(context.Background(), binding); result.Err != nil || !created {
			t.Fatalf("reserve %s created=%v result=%#v", binding.SessionID, created, result)
		}
	}
	first, err := r.ListPersistentRecoveryCandidates(context.Background())
	if err != nil || len(first) != 2 || first[0].SessionID != a.SessionID || first[1].SessionID != b.SessionID {
		t.Fatalf("first=%#v err=%v", first, err)
	}

	live := a
	live.Lifecycle = persistent.LifecycleLive
	live.UpdatedAt = live.UpdatedAt.Add(time.Second)
	if result := r.AdvancePersistentBinding(context.Background(), live); result.Err != nil {
		t.Fatal(result.Err)
	}
	terminal := b
	terminal.Lifecycle = persistent.LifecycleTerminal
	terminal.UpdatedAt = terminal.UpdatedAt.Add(time.Second)
	if result := r.AdvancePersistentBinding(context.Background(), terminal); result.Err != nil {
		t.Fatal(result.Err)
	}

	reopened := openRecoveryRepository(t, root)
	remaining, err := reopened.ListPersistentRecoveryCandidates(context.Background())
	if err != nil || len(remaining) != 1 || remaining[0].SessionID != a.SessionID || remaining[0].Lifecycle != persistent.LifecycleLive {
		t.Fatalf("remaining=%#v err=%v", remaining, err)
	}
	lost := remaining[0]
	lost.Lifecycle = persistent.LifecycleLost
	lost.UpdatedAt = lost.UpdatedAt.Add(time.Second)
	if result := reopened.AdvancePersistentBinding(context.Background(), lost); result.Err != nil {
		t.Fatal(result.Err)
	}
	empty, err := reopened.ListPersistentRecoveryCandidates(context.Background())
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty=%#v err=%v", empty, err)
	}
}

func TestPersistentRecoveryCandidatesRejectOrphanMarker(t *testing.T) {
	r := openRecoveryRepository(t, filepath.Join(t.TempDir(), "state"))
	orphan := persistentBinding("persistent-orphan-session", "persistent-orphan-op", "orphan", time.Now().UTC())
	if err := r.createPersistentRecoveryMarker(recoveryMarkerFor(orphan)); err != nil {
		t.Fatal(err)
	}
	candidates, err := r.ListPersistentRecoveryCandidates(context.Background())
	if !errors.Is(err, failure.SupervisorStateConflict) || len(candidates) != 0 {
		t.Fatalf("orphan candidates=%#v err=%v", candidates, err)
	}
}

func TestAbandonPersistentSessionPublishesCanonicalAmbiguityThenMarksLost(t *testing.T) {
	r := openRecoveryRepository(t, filepath.Join(t.TempDir(), "state"))
	now := time.Date(2026, 8, 16, 1, 30, 0, 0, time.UTC)
	binding := persistentBinding("persistent-lost-session", "persistent-lost-op", "lost-server", now)
	reservePersistentOperationWithMetadata(t, r, binding, now)
	if _, created, result := r.ReservePersistentBinding(context.Background(), binding); result.Err != nil || !created {
		t.Fatalf("binding created=%v result=%#v", created, result)
	}
	live := binding
	live.Lifecycle = persistent.LifecycleLive
	live.UpdatedAt = now.Add(time.Second)
	if result := r.AdvancePersistentBinding(context.Background(), live); result.Err != nil {
		t.Fatal(result.Err)
	}
	if result := r.AbandonPersistentSession(context.Background(), live, "new-daemon", "supervisor_auth_failed"); result.Err != nil {
		t.Fatal(result.Err)
	}
	rec, err := r.LoadReceipt(context.Background(), operation.SessionID(binding.SessionID))
	if err != nil {
		t.Fatal(err)
	}
	if rec.SchemaVersion != 4 || !rec.Persistent || rec.SessionName != binding.SessionName || rec.State != session.Abandoned || rec.Outcome != session.Ambiguous || rec.FailureReason != "supervisor_auth_failed" {
		t.Fatalf("receipt=%#v", rec)
	}
	stored, err := r.LoadPersistentBinding(context.Background(), operation.SessionID(binding.SessionID))
	if err != nil || stored.Lifecycle != persistent.LifecycleLost {
		t.Fatalf("binding=%#v err=%v", stored, err)
	}
	candidates, err := r.ListPersistentRecoveryCandidates(context.Background())
	if err != nil || len(candidates) != 0 {
		t.Fatalf("candidates=%#v err=%v", candidates, err)
	}
	if result := r.AbandonPersistentSession(context.Background(), stored, "new-daemon", "supervisor_auth_failed"); result.Err != nil {
		t.Fatalf("idempotent abandon failed: %#v", result)
	}
}
