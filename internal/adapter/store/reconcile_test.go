package store

import (
	"context"
	"errors"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
	"os"
	"path/filepath"
	"testing"
)

func TestAbandonUnresolved(t *testing.T) {
	r, err := Open(filepath.Join(t.TempDir(), "state"), Limits{MaxSessions: 2, MaxSessionOutput: 100, MaxTotalState: 1 << 20, ControlReserve: 100})
	if err != nil {
		t.Fatal(err)
	}
	res := operation.Reservation{SchemaVersion: 1, OperationID: "op", SessionID: "s", Fingerprint: "f", Command: "x", CWD: "/", Shell: "/bin/sh", DaemonIncarnation: "old"}
	if _, _, got := r.ReserveOperation(context.Background(), res); got.Err != nil {
		t.Fatal(got.Err)
	}
	if err := r.AbandonUnresolved(context.Background(), "new"); err != nil {
		t.Fatal(err)
	}
	snap, err := r.LoadSession(context.Background(), "s")
	if err != nil {
		t.Fatal(err)
	}
	if snap.State != session.Abandoned || snap.Outcome != session.Ambiguous {
		t.Fatalf("%#v", snap)
	}
}

func TestAbandonUnresolvedRemovesUncommittedAtomicTemp(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r, err := Open(root, Limits{MaxSessions: 2, MaxSessionOutput: 100, MaxTotalState: 1 << 20, ControlReserve: 100})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "operations", ".shellbeam-crash-temp")
	if err = os.WriteFile(path, []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = r.AbandonUnresolved(context.Background(), "new"); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("temporary file still present: %v", err)
	}
}

func TestAbandonUnresolvedDefersDelegatedRecoveryMarkerToCoordinator(t *testing.T) {
	r := delegatedRepository(t, filepath.Join(t.TempDir(), "state"), 8)
	res := task4DelegatedReservation("op-reconcile-delegated", "session-reconcile-delegated", "")
	reserveDelegatedOperation(t, r, res)
	binding, ref := delegatedBindingAndRef(res, "provider_ref_reconcile")
	if _, created, got := r.ReserveDelegatedBinding(context.Background(), binding, ref); got.Err != nil || !created {
		t.Fatalf("binding created=%v result=%#v", created, got)
	}
	if err := r.AbandonUnresolved(context.Background(), "new-daemon"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.LoadReceipt(context.Background(), res.SessionID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("generic reconciliation published delegated receipt: %v", err)
	}
	candidates, err := r.ListDelegatedRecoveryCandidates(context.Background())
	if err != nil || len(candidates) != 1 || candidates[0].SessionID != string(res.SessionID) {
		t.Fatalf("candidates=%#v err=%v", candidates, err)
	}
}

func TestAbandonUnresolvedCanonicalizesDelegatedCrashBeforeRecoveryMarkerAsV5(t *testing.T) {
	r := delegatedRepository(t, filepath.Join(t.TempDir(), "state"), 8)
	res := task4DelegatedReservation("op-reconcile-pre-provider", "session-reconcile-pre-provider", "")
	reserveDelegatedOperation(t, r, res)
	if err := r.AbandonUnresolved(context.Background(), "new-daemon"); err != nil {
		t.Fatal(err)
	}
	rec, err := r.LoadReceipt(context.Background(), res.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.SchemaVersion != 5 || rec.SessionMode != delegated.ModeDelegatedInteractive || rec.AuthorityEpoch != res.AuthorityEpoch || rec.State != session.Abandoned || rec.Outcome != session.Ambiguous || rec.FailureReason != "daemon_restarted" {
		t.Fatalf("receipt=%#v", rec)
	}
	if rec.EvidenceAuthority != receipt.EvidenceAuthoritySessionLifecycleOnly || rec.InputAuthorityProvenance != receipt.InputAuthorityAgentOnly || rec.OutputComplete || rec.CaptureQuality != receipt.CaptureIncomplete || len(rec.CaptureReasons) != 1 || rec.CaptureReasons[0] != receipt.CaptureReasonTransportGap {
		t.Fatalf("capture/authority=%#v", rec)
	}
}

func TestAbandonUnresolvedBlocksDelegatedBindingWithMissingRecoveryMarker(t *testing.T) {
	r := delegatedRepository(t, filepath.Join(t.TempDir(), "state"), 8)
	res := task4DelegatedReservation("op-reconcile-missing-marker", "session-reconcile-missing-marker", "")
	reserveDelegatedOperation(t, r, res)
	binding, ref := delegatedBindingAndRef(res, "provider_ref_missing_marker")
	if _, created, got := r.ReserveDelegatedBinding(context.Background(), binding, ref); got.Err != nil || !created {
		t.Fatalf("binding created=%v result=%#v", created, got)
	}
	if err := os.Remove(r.delegatedRecoveryPath(res.SessionID)); err != nil {
		t.Fatal(err)
	}
	if err := r.AbandonUnresolved(context.Background(), "new-daemon"); !errors.Is(err, failure.DelegatedReconcileBlocked) {
		t.Fatalf("err=%v want delegated_reconcile_blocked", err)
	}
	if _, err := r.LoadReceipt(context.Background(), res.SessionID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing marker path invented terminal receipt: %v", err)
	}
}
