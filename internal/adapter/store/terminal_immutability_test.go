package store

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func TestPublishTerminalRejectsDifferentReceiptWithoutChangingState(t *testing.T) {
	r, rec := terminalRepository(t)
	if got := r.PublishTerminal(context.Background(), rec); got.Err != nil {
		t.Fatal(got.Err)
	}
	receiptPath := filepath.Join(r.root, "sessions", rec.SessionID, "receipt.json")
	metadataPath := filepath.Join(r.root, "sessions", rec.SessionID, "metadata.json")
	receiptBefore := readFile(t, receiptPath)
	metadataBefore := readFile(t, metadataPath)

	different := rec
	different.State = session.Failed
	different.Outcome = session.Failure
	different.FailureReason = "different_terminal_fact"
	if got := r.PublishTerminal(context.Background(), different); got.Err == nil || got.Err.Error() != "terminal_conflict" {
		t.Fatalf("error = %v, want terminal_conflict", got.Err)
	}
	if after := readFile(t, receiptPath); !bytes.Equal(receiptBefore, after) {
		t.Fatal("terminal conflict changed receipt bytes")
	}
	if after := readFile(t, metadataPath); !bytes.Equal(metadataBefore, after) {
		t.Fatal("terminal conflict changed metadata bytes")
	}
}

func TestPublishTerminalIdenticalReplayNeedsNoWriteAccess(t *testing.T) {
	r, rec := terminalRepository(t)
	if got := r.PublishTerminal(context.Background(), rec); got.Err != nil {
		t.Fatal(got.Err)
	}
	sessionDir := filepath.Join(r.root, "sessions", rec.SessionID)
	if err := os.Chmod(sessionDir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sessionDir, 0700) })

	if got := r.PublishTerminal(context.Background(), rec); got.Err != nil {
		t.Fatalf("identical replay: %v", got.Err)
	}
}

func terminalRepository(t *testing.T) (*Repository, receipt.Receipt) {
	t.Helper()
	r, err := Open(filepath.Join(t.TempDir(), "state"), Limits{
		MaxSessions: 1, MaxSessionOutput: 100, MaxTotalState: 1 << 20, ControlReserve: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	res := operation.Reservation{
		SchemaVersion: 1, OperationID: "op", SessionID: "s", Fingerprint: "f",
		Command: "true", CWD: "/", Shell: "/bin/sh", DaemonIncarnation: "d",
	}
	if _, _, got := r.ReserveOperation(context.Background(), res); got.Err != nil {
		t.Fatal(got.Err)
	}
	code := 0
	rec := receipt.Receipt{
		SchemaVersion: 1, OperationID: "op", SessionID: "s", Fingerprint: "f",
		DaemonIncarnation: "d", State: session.Completed, Outcome: session.Success,
		OutputComplete: true, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true},
		Exit: receipt.ExitEvidence{Reaped: true, Code: &code},
	}
	return r, rec
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
