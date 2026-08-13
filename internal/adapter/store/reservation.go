package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func (r *Repository) ReserveOperation(ctx context.Context, want operation.Reservation) (operation.Reservation, bool, app.StoreResult) {
	_ = ctx
	unlock := r.lock(want.OperationID)
	defer unlock()
	r.admit.Lock()
	defer r.admit.Unlock()
	path := filepath.Join(r.root, "operations", string(want.OperationID)+".json")
	var existing operation.Reservation
	if err := readStrict(path, &existing); err == nil {
		return r.replayReservation(want, existing)
	} else if !errors.Is(err, ErrNotFound) {
		return existing, false, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if err := validateReservation(want); err != nil {
		return existing, false, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	active, used, err := r.usage()
	if err != nil {
		return existing, false, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if active >= r.limits.MaxSessions {
		return existing, false, app.StoreResult{Durability: app.NoDurableChange, Err: fmt.Errorf("capacity_exceeded")}
	}
	if used+r.limits.ControlReserve > r.limits.MaxTotalState {
		return existing, false, app.StoreResult{Durability: app.NoDurableChange, Err: fmt.Errorf("persistence_unavailable")}
	}
	if want.CreatedAt.IsZero() {
		want.CreatedAt = time.Now().UTC()
	}
	if result := r.writer.Create(path, want); result.Err != nil {
		if !errors.Is(result.Err, os.ErrExist) {
			return existing, false, result
		}
		if err = readStrict(path, &existing); err != nil {
			return existing, false, app.StoreResult{Durability: app.AmbiguousChange, Err: err}
		}
		return r.replayReservation(want, existing)
	}
	if result := r.ensureSessionMetadata(want); result.Err != nil {
		return want, false, result
	}
	return want, true, app.StoreResult{Durability: app.DurableChange}
}

func (r *Repository) replayReservation(want, existing operation.Reservation) (operation.Reservation, bool, app.StoreResult) {
	if existing.EffectiveRequestFingerprint() != want.EffectiveRequestFingerprint() {
		return existing, false, app.StoreResult{Durability: app.DurableChange, Err: failure.New(failure.OperationConflict, map[string]string{"operation_id": string(existing.OperationID)}, nil)}
	}
	if existing.ObservationBindingFingerprint != want.ObservationBindingFingerprint {
		return existing, false, app.StoreResult{Durability: app.DurableChange, Err: failure.New(failure.OperationMetadataConflict, map[string]string{"operation_id": string(existing.OperationID)}, nil)}
	}
	return existing, false, r.ensureSessionMetadata(existing)
}

func validateReservation(v operation.Reservation) error {
	if v.OperationID == "" || v.SessionID == "" {
		return fmt.Errorf("invalid reservation")
	}
	switch v.SchemaVersion {
	case 1:
		if v.Fingerprint == "" {
			return fmt.Errorf("invalid reservation")
		}
	case 2:
		if v.RequestFingerprint == "" || v.ExecutionFingerprint == "" {
			return fmt.Errorf("invalid reservation")
		}
	default:
		return fmt.Errorf("invalid reservation")
	}
	return nil
}

func (r *Repository) ensureSessionMetadata(v operation.Reservation) app.StoreResult {
	sdir := filepath.Join(r.root, "sessions", string(v.SessionID))
	if err := ensurePrivateDir(sdir); err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	path := filepath.Join(sdir, "metadata.json")
	var current session.Snapshot
	if err := readStrict(path, &current); err == nil {
		if current.OperationID != string(v.OperationID) || current.SessionID != string(v.SessionID) {
			return app.StoreResult{Durability: app.NoDurableChange, Err: fmt.Errorf("session_conflict")}
		}
		return app.StoreResult{Durability: app.DurableChange}
	} else if !errors.Is(err, ErrNotFound) {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	snap := session.Snapshot{SchemaVersion: 1, OperationID: string(v.OperationID), SessionID: string(v.SessionID), DaemonIncarnation: v.DaemonIncarnation, State: session.Starting, OutputAvailable: true, UpdatedAt: time.Now().UTC()}
	return r.writer.Replace(path, snap)
}

func ensurePrivateDir(path string) error {
	err := os.Mkdir(path, 0700)
	if err == nil {
		return nil
	}
	if !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm() != 0700 || !ownedByCurrent(info) {
		return fmt.Errorf("unsafe session directory")
	}
	return nil
}
