package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func (r *Repository) AbandonUnresolved(ctx context.Context, newIncarnation string) error {
	if err := r.repairCommittedOperations(ctx); err != nil {
		return err
	}
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
		existing, receiptErr := r.LoadReceipt(ctx, operation.SessionID(snap.SessionID))
		if receiptErr == nil {
			if got := r.PublishTerminal(ctx, existing); got.Err != nil {
				return got.Err
			}
			continue
		}
		if !errors.Is(receiptErr, ErrNotFound) {
			return receiptErr
		}
		reservation, loadErr := r.LoadOperation(ctx, operation.ID(snap.OperationID))
		if loadErr != nil && !errors.Is(loadErr, ErrNotFound) {
			return loadErr
		}
		rec := abandonedReceipt(snap, reservation, loadErr == nil, newIncarnation)
		if got := r.PublishTerminal(ctx, rec); got.Err != nil {
			return got.Err
		}
	}
	return nil
}

func (r *Repository) repairCommittedOperations(ctx context.Context) error {
	entries, err := os.ReadDir(filepath.Join(r.root, "operations"))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), ".shellbeam-") {
			if err := os.Remove(filepath.Join(r.root, "operations", entry.Name())); err != nil {
				return err
			}
			continue
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			return errors.New("unexpected operation entry")
		}
		id := operation.ID(strings.TrimSuffix(entry.Name(), ".json"))
		reservation, err := r.LoadOperation(ctx, id)
		if err != nil {
			return err
		}
		if _, err = r.LoadSession(ctx, reservation.SessionID); err == nil {
			continue
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		if result := r.ensureSessionMetadata(reservation); result.Err != nil {
			return result.Err
		}
	}
	return nil
}

func abandonedReceipt(snap session.Snapshot, reservation operation.Reservation, hasReservation bool, incarnation string) receipt.Receipt {
	rec := receipt.Receipt{
		SchemaVersion: 1, OperationID: snap.OperationID, SessionID: snap.SessionID,
		DaemonIncarnation: incarnation, State: session.Abandoned, Outcome: session.Ambiguous,
		FailureReason: "daemon_restarted", OutputComplete: false,
	}
	if !hasReservation {
		return rec
	}
	if reservation.SchemaVersion == 2 {
		rec.SchemaVersion = 2
		rec.RequestFingerprint = reservation.RequestFingerprint
		rec.ExecutionFingerprint = reservation.ExecutionFingerprint
		rec.ObservationBindingFingerprint = reservation.ObservationBindingFingerprint
		rec.ExecutionMode = string(reservation.ExecutionMode)
		rec.Executable = reservation.Executable
		rec.Shell = reservation.Shell
		rec.CWD = reservation.CWD
		rec.TTY = reservation.TTY
		rec.TimeoutMS = reservation.TimeoutMS
		return rec
	}
	rec.Fingerprint = reservation.Fingerprint
	return rec
}
