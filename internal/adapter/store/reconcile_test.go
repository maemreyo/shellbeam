package store

import (
	"context"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/session"
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
