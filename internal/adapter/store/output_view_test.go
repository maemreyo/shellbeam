package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/maemreyo/shellbeam/internal/app/outputview"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func TestOutputExtentDistinguishesRetainedEmptyNonEmptyAndUnknown(t *testing.T) {
	r := openOutputViewRepo(t)
	reserveOutputViewSession(t, r, "empty")

	empty, err := r.OutputExtent(context.Background(), "empty")
	if err != nil {
		t.Fatal(err)
	}
	if empty.State != outputview.RetentionRetained || empty.Bytes != 0 || empty.Terminal {
		t.Fatalf("empty extent=%#v", empty)
	}

	reserveOutputViewSession(t, r, "data")
	if _, got := r.AppendOutput(context.Background(), "data", []byte("hello")); got.Err != nil {
		t.Fatal(got.Err)
	}
	data, err := r.OutputExtent(context.Background(), "data")
	if err != nil {
		t.Fatal(err)
	}
	if data.State != outputview.RetentionRetained || data.Bytes != 5 || data.Terminal {
		t.Fatalf("data extent=%#v", data)
	}

	missing, err := r.OutputExtent(context.Background(), "missing")
	if err != nil {
		t.Fatal(err)
	}
	if missing.State != outputview.RetentionUnavailable || missing.Bytes != 0 {
		t.Fatalf("missing extent=%#v", missing)
	}
}

func TestOutputExtentReportsCompactedAndUnexpectedMissingSeparately(t *testing.T) {
	r := openOutputViewRepo(t)
	reserveOutputViewSession(t, r, "compact")
	if _, got := r.AppendOutput(context.Background(), "compact", []byte("hello")); got.Err != nil {
		t.Fatal(got.Err)
	}
	terminal := session.Snapshot{SchemaVersion: 1, OperationID: "op-compact", SessionID: "compact", DaemonIncarnation: "d", State: session.Completed, Outcome: session.Success, OutputBytes: 5, OutputAvailable: true, UpdatedAt: time.Now().UTC()}
	if got := r.AdvanceSession(context.Background(), terminal); got.Err != nil {
		t.Fatal(got.Err)
	}
	if got := r.Compact(context.Background(), "compact"); got.Err != nil {
		t.Fatal(got.Err)
	}
	compacted, err := r.OutputExtent(context.Background(), "compact")
	if err != nil {
		t.Fatal(err)
	}
	if compacted.State != outputview.RetentionCompacted || compacted.Bytes != 5 || !compacted.Terminal {
		t.Fatalf("compacted extent=%#v", compacted)
	}

	reserveOutputViewSession(t, r, "lost")
	if _, got := r.AppendOutput(context.Background(), "lost", []byte("hello")); got.Err != nil {
		t.Fatal(got.Err)
	}
	lostSnapshot, err := r.LoadSession(context.Background(), "lost")
	if err != nil {
		t.Fatal(err)
	}
	lostSnapshot.OutputBytes = 5
	lostSnapshot.OutputAvailable = true
	if got := r.AdvanceSession(context.Background(), lostSnapshot); got.Err != nil {
		t.Fatal(got.Err)
	}
	if err := os.Remove(filepath.Join(r.root, "sessions", "lost", "output.log")); err != nil {
		t.Fatal(err)
	}
	lost, err := r.OutputExtent(context.Background(), "lost")
	if err != nil {
		t.Fatal(err)
	}
	if lost.State != outputview.RetentionUnavailable {
		t.Fatalf("lost extent=%#v", lost)
	}
}

func openOutputViewRepo(t *testing.T) *Repository {
	t.Helper()
	r, err := Open(filepath.Join(t.TempDir(), "state"), Limits{MaxSessions: 8, MaxSessionOutput: 1 << 20, MaxTotalState: 8 << 20, ControlReserve: 1024})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func reserveOutputViewSession(t *testing.T, r *Repository, sid string) {
	t.Helper()
	res := operation.Reservation{SchemaVersion: 1, OperationID: operation.ID("op-" + sid), SessionID: operation.SessionID(sid), Fingerprint: "fp-" + sid, Command: "true", CWD: "/tmp", Shell: "/bin/sh", DaemonIncarnation: "d"}
	if _, created, got := r.ReserveOperation(context.Background(), res); got.Err != nil || !created {
		t.Fatalf("reserve created=%v result=%#v", created, got)
	}
}
