package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
)

func TestDelegatedRecoveryRepairsPublicBindingAndPrivateRefFromMarker(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := delegatedRepository(t, root, 8)
	res := task4DelegatedReservation("op-restart", "session-restart", "dev")
	reserveDelegatedOperation(t, r, res)
	binding, ref := delegatedBindingAndRef(res, "provider_ref_restart")
	if _, _, got := r.ReserveDelegatedBinding(context.Background(), binding, ref); got.Err != nil {
		t.Fatal(got.Err)
	}

	if err := os.Remove(r.delegatedBindingPath(res.SessionID)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(r.delegatedProviderRefPath(res.SessionID)); err != nil {
		t.Fatal(err)
	}
	r = delegatedRepository(t, root, 8)
	candidates, err := r.ListDelegatedRecoveryCandidates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0] != binding {
		t.Fatalf("candidates=%#v", candidates)
	}
	if got, err := r.LoadDelegatedProviderRef(context.Background(), res.SessionID); err != nil || got != ref {
		t.Fatalf("repaired ref=%#v err=%v", got, err)
	}
}

func TestDelegatedRecoveryRepairsInterruptedBindingCreate(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := delegatedRepository(t, root, 8)
	res := task4DelegatedReservation("op-fault", "session-fault", "")
	reserveDelegatedOperation(t, r, res)
	binding, ref := delegatedBindingAndRef(res, "provider_ref_fault")
	// recovery marker = first create.link, provider ref = second, public binding = third.
	r.writer = failAtomicWriterNth("create.link", 3)
	if _, created, got := r.ReserveDelegatedBinding(context.Background(), binding, ref); got.Err == nil || created || got.Durability == app.NoDurableChange {
		t.Fatalf("fault reserve created=%v result=%#v", created, got)
	}
	r.writer = atomicWriter{onBytes: r.addStateBytes}

	r = delegatedRepository(t, root, 8)
	candidates, err := r.ListDelegatedRecoveryCandidates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0] != binding {
		t.Fatalf("candidates=%#v", candidates)
	}
	if got, err := r.LoadDelegatedProviderRef(context.Background(), res.SessionID); err != nil || got != ref {
		t.Fatalf("provider ref=%#v err=%v", got, err)
	}
}

func TestDelegatedTerminalAdvanceReportsRecoveryMarkerCleanupFailure(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := delegatedRepository(t, root, 8)
	res := task4DelegatedReservation("op-cleanup", "session-cleanup", "")
	reserveDelegatedOperation(t, r, res)
	binding, ref := delegatedBindingAndRef(res, "provider_ref_cleanup")
	if _, _, got := r.ReserveDelegatedBinding(context.Background(), binding, ref); got.Err != nil {
		t.Fatal(got.Err)
	}
	live := binding
	live.Lifecycle = delegated.LifecycleLive
	live.UpdatedAt = live.UpdatedAt.Add(time.Second)
	if got := r.AdvanceDelegatedBinding(context.Background(), live); got.Err != nil {
		t.Fatal(got.Err)
	}

	if err := os.Chmod(r.delegatedRecoveryDir(), 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(r.delegatedRecoveryDir(), 0o700)
	terminal := live
	terminal.Lifecycle = delegated.LifecycleTerminal
	terminal.UpdatedAt = terminal.UpdatedAt.Add(time.Second)
	got := r.AdvanceDelegatedBinding(context.Background(), terminal)
	if got.Err == nil || got.Durability != app.DurableChange {
		t.Fatalf("cleanup failure result=%#v", got)
	}
}
