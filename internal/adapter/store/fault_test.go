package store

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

var persistenceFaultPoints = []string{
	"create.create_temp", "create.write", "create.file_sync", "create.close",
	"create.link", "create.open_dir", "create.dir_sync",
	"replace.create_temp", "replace.write", "replace.file_sync", "replace.close",
	"replace.rename", "replace.open_dir", "replace.dir_sync",
}

func TestReservationFaultBoundariesRecoverWithoutDuplicateAuthorization(t *testing.T) {
	for _, point := range persistenceFaultPoints {
		t.Run(point, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "state")
			r := openRecoveryRepository(t, root)
			r.writer = failAtomicWriter(point)
			res := operation.Reservation{
				SchemaVersion: 1, OperationID: "fault-op", SessionID: "fault-session",
				Fingerprint: "fingerprint", Command: "true", CWD: "/",
				Shell: "/bin/sh", DaemonIncarnation: "daemon",
			}
			_, created, result := r.ReserveOperation(context.Background(), res)
			if result.Err == nil || created {
				t.Fatalf("first result = %#v, created = %v", result, created)
			}
			_, statErr := os.Stat(filepath.Join(root, "operations", "fault-op.json"))
			committed := statErr == nil
			if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
				t.Fatal(statErr)
			}

			r = openRecoveryRepository(t, root)
			if err := r.AbandonUnresolved(context.Background(), "restarted-daemon"); err != nil {
				t.Fatal(err)
			}
			if committed {
				snap, err := r.LoadSession(context.Background(), res.SessionID)
				if err != nil {
					t.Fatal(err)
				}
				if snap.State != session.Abandoned || snap.Outcome != session.Ambiguous {
					t.Fatalf("reconciled snapshot = %#v", snap)
				}
			}
			_, created, result = r.ReserveOperation(context.Background(), res)
			if result.Err != nil {
				t.Fatal(result.Err)
			}
			if created == committed {
				t.Fatalf("created = %v, operation committed before retry = %v", created, committed)
			}
			if _, created, result = r.ReserveOperation(context.Background(), res); result.Err != nil || created {
				t.Fatalf("second replay result = %#v, created = %v", result, created)
			}
			if _, err := r.LoadSession(context.Background(), res.SessionID); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestTerminalFaultBoundariesPreserveOneImmutableReceipt(t *testing.T) {
	for _, point := range persistenceFaultPoints {
		t.Run(point, func(t *testing.T) {
			r, rec := terminalRepository(t)
			r.writer = failAtomicWriter(point)
			if result := r.PublishTerminal(context.Background(), rec); result.Err == nil {
				t.Fatal("fault did not interrupt terminal publication")
			}
			r.writer = atomicWriter{}
			if result := r.PublishTerminal(context.Background(), rec); result.Err != nil {
				t.Fatal(result.Err)
			}
			path := filepath.Join(r.root, "sessions", rec.SessionID, "receipt.json")
			before := readFile(t, path)
			different := rec
			different.State = session.Failed
			different.Outcome = session.Failure
			different.FailureReason = "conflicting_fact"
			if result := r.PublishTerminal(context.Background(), different); result.Err == nil || result.Err.Error() != "terminal_conflict" {
				t.Fatalf("conflict result = %#v", result)
			}
			if after := readFile(t, path); !bytes.Equal(before, after) {
				t.Fatal("conflict changed immutable receipt")
			}
			stored, err := r.LoadReceipt(context.Background(), operation.SessionID(rec.SessionID))
			if err != nil {
				t.Fatal(err)
			}
			if stored.Outcome != session.Success {
				t.Fatalf("stored receipt = %#v", stored)
			}
		})
	}
}

func failAtomicWriter(point string) atomicWriter {
	failed := false
	return atomicWriter{fail: func(got string) error {
		if !failed && got == point {
			failed = true
			return errors.New("injected persistence fault: " + point)
		}
		return nil
	}}
}
