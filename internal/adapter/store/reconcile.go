package store

import (
	"context"
	"errors"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
	"os"
	"path/filepath"
	"time"
)

func (r *Repository) AbandonUnresolved(ctx context.Context, newIncarnation string) error {
	entries, err := os.ReadDir(filepath.Join(r.root, "sessions"))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			return errors.New("unexpected state entry")
		}
		var snap session.Snapshot
		if err = readStrict(filepath.Join(r.root, "sessions", entry.Name(), "metadata.json"), &snap); err != nil {
			return err
		}
		if snap.State.Terminal() {
			continue
		}
		reservation, loadErr := r.LoadOperation(ctx, operation.ID(snap.OperationID))
		if loadErr != nil {
			return loadErr
		}
		rec := receipt.Receipt{SchemaVersion: 1, OperationID: snap.OperationID, SessionID: snap.SessionID, Fingerprint: reservation.Fingerprint, DaemonIncarnation: newIncarnation, State: session.Abandoned, Outcome: session.Ambiguous, FailureReason: "daemon_restarted", OutputComplete: false}
		if got := r.PublishTerminal(ctx, rec); got.Err != nil {
			return got.Err
		}
		snap.State = session.Abandoned
		snap.Outcome = session.Ambiguous
		snap.DaemonIncarnation = newIncarnation
		snap.UpdatedAt = time.Now().UTC()
		if got := r.AdvanceSession(ctx, snap); got.Err != nil {
			return got.Err
		}
	}
	return nil
}
