package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func TestReserveOperationRepairsMetadataFromCommittedOperation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openRecoveryRepository(t, root)
	res := operation.Reservation{
		SchemaVersion: 1, OperationID: "committed-op", SessionID: "committed-session",
		Fingerprint: "fingerprint", Command: "true", CWD: "/", Shell: "/bin/sh",
		DaemonIncarnation: "daemon-a", CreatedAt: time.Now().UTC(),
	}
	operationPath := filepath.Join(root, "operations", string(res.OperationID)+".json")
	if got := atomicCreateJSON(operationPath, res); got.Err != nil {
		t.Fatal(got.Err)
	}

	r = openRecoveryRepository(t, root)
	stored, created, got := r.ReserveOperation(context.Background(), res)
	if got.Err != nil {
		t.Fatal(got.Err)
	}
	if created {
		t.Fatal("committed operation was authorized as a new start")
	}
	if stored.SessionID != res.SessionID {
		t.Fatalf("session = %q", stored.SessionID)
	}
	snap, err := r.LoadSession(context.Background(), res.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if snap.State != session.Starting || snap.OperationID != string(res.OperationID) || snap.SessionID != string(res.SessionID) {
		t.Fatalf("repaired snapshot = %#v", snap)
	}
}

func TestAbandonUnresolvedClosesLegacyOrphanStartingMetadata(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openRecoveryRepository(t, root)
	sessionDir := filepath.Join(root, "sessions", "orphan-session")
	if err := mkdirPrivate(sessionDir); err != nil {
		t.Fatal(err)
	}
	snap := session.Snapshot{
		SchemaVersion: 1, OperationID: "orphan-op", SessionID: "orphan-session",
		DaemonIncarnation: "dead-daemon", State: session.Starting,
		OutputAvailable: true, UpdatedAt: time.Now().UTC(),
	}
	if got := atomicJSON(filepath.Join(sessionDir, "metadata.json"), snap); got.Err != nil {
		t.Fatal(got.Err)
	}

	r = openRecoveryRepository(t, root)
	if err := r.AbandonUnresolved(context.Background(), "new-daemon"); err != nil {
		t.Fatal(err)
	}
	repaired, err := r.LoadSession(context.Background(), "orphan-session")
	if err != nil {
		t.Fatal(err)
	}
	if repaired.State != session.Abandoned || repaired.Outcome != session.Ambiguous {
		t.Fatalf("snapshot = %#v", repaired)
	}
	rec, err := r.LoadReceipt(context.Background(), "orphan-session")
	if err != nil {
		t.Fatal(err)
	}
	if rec.State != session.Abandoned || rec.Outcome != session.Ambiguous || rec.OperationID != "orphan-op" {
		t.Fatalf("receipt = %#v", rec)
	}
}

func openRecoveryRepository(t *testing.T, root string) *Repository {
	t.Helper()
	r, err := Open(root, Limits{
		MaxSessions: 4, MaxSessionOutput: 1024, MaxTotalState: 1 << 20, ControlReserve: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func mkdirPrivate(path string) error {
	return os.Mkdir(path, 0700)
}
