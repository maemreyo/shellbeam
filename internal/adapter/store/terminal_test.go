package store

import (
	"context"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
	"os"
	"path/filepath"
	"testing"
)

func TestPublishThenCompactPreservesTombstone(t *testing.T) {
	r, err := Open(filepath.Join(t.TempDir(), "state"), Limits{MaxSessions: 1, MaxSessionOutput: 100, MaxTotalState: 1 << 20, ControlReserve: 100})
	if err != nil {
		t.Fatal(err)
	}
	res := operation.Reservation{SchemaVersion: 1, OperationID: "op", SessionID: "s", Fingerprint: "f", Command: "true", CWD: "/", Shell: "/bin/sh", DaemonIncarnation: "d"}
	if _, _, got := r.ReserveOperation(context.Background(), res); got.Err != nil {
		t.Fatal(got.Err)
	}
	if _, got := r.AppendOutput(context.Background(), "s", []byte("ok")); got.Err != nil {
		t.Fatal(got.Err)
	}
	zero := 0
	rec := receipt.Receipt{SchemaVersion: 1, OperationID: "op", SessionID: "s", Fingerprint: "f", DaemonIncarnation: "d", State: session.Completed, Outcome: session.Success, OutputBytes: 2, OutputComplete: true, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}, Exit: receipt.ExitEvidence{Reaped: true, Code: &zero}}
	if got := r.PublishTerminal(context.Background(), rec); got.Err != nil {
		t.Fatal(got.Err)
	}
	if got := r.Compact(context.Background(), "s"); got.Err != nil {
		t.Fatal(got.Err)
	}
	s, err := r.LoadSession(context.Background(), "s")
	if err != nil || s.OutputAvailable {
		t.Fatalf("snapshot=%#v err=%v", s, err)
	}
	if _, err := os.Stat(filepath.Join(r.root, "sessions", "s", "receipt.json")); err != nil {
		t.Fatal(err)
	}
}
