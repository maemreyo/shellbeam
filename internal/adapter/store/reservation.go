package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	persistentsession "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func (r *Repository) ReserveOperation(ctx context.Context, want operation.Reservation) (operation.Reservation, bool, app.StoreResult) {
	unlock := r.lock(want.OperationID)
	defer unlock()
	r.admit.Lock()
	defer r.admit.Unlock()
	path := filepath.Join(r.root, "operations", string(want.OperationID)+".json")
	var existing operation.Reservation
	if err := readStrict(path, &existing); err == nil {
		stored, created, result := r.replayReservation(want, existing)
		if result.Err == nil {
			if candidateErr := r.EnsureEvidenceCandidate(ctx, stored); candidateErr != nil {
				result.Err = candidateErr
			}
		}
		return stored, created, result
	} else if !errors.Is(err, ErrNotFound) {
		return existing, false, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if err := validateReservation(want); err != nil {
		return existing, false, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	active, used, err := r.admissionCounters()
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
	if err := r.EnsureEvidenceCandidate(ctx, want); err != nil {
		return existing, false, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	seq, prepared := r.prepareAdmissionObservation(ctx, want)
	if prepared.Err != nil {
		return existing, false, prepared
	}
	result := r.writer.Create(path, want)
	if result.Err != nil {
		if errors.Is(result.Err, os.ErrExist) {
			r.abortObservationBestEffort(seq, observationAbortConflict)
			if err = readStrict(path, &existing); err != nil {
				return existing, false, withObservationSeq(app.StoreResult{Durability: app.AmbiguousChange, Err: err}, seq)
			}
			stored, created, replay := r.replayReservation(want, existing)
			if replay.Err == nil {
				if candidateErr := r.EnsureEvidenceCandidate(ctx, stored); candidateErr != nil {
					replay.Err = candidateErr
				}
			}
			return stored, created, withObservationSeq(replay, seq)
		}
		if result.Durability == app.NoDurableChange || !r.reservationFileMatches(path, want) {
			r.abortObservationBestEffort(seq, observationAbortWriteFailed)
		}
		return existing, false, withObservationSeq(result, seq)
	}
	r.commitObservationBestEffort(seq)
	metadata := withObservationSeq(r.ensureSessionMetadata(want), seq)
	if metadata.Err != nil {
		return want, false, metadata
	}
	return want, true, metadata
}

func (r *Repository) replayReservation(want, existing operation.Reservation) (operation.Reservation, bool, app.StoreResult) {
	if existing.EffectiveRequestFingerprint() != want.EffectiveRequestFingerprint() {
		return existing, false, app.StoreResult{Durability: app.DurableChange, Err: failure.New(failure.OperationConflict, map[string]string{"operation_id": string(existing.OperationID)}, nil)}
	}
	if existing.ObservationBindingFingerprint != want.ObservationBindingFingerprint {
		return existing, false, app.StoreResult{Durability: app.DurableChange, Err: failure.New(failure.OperationMetadataConflict, map[string]string{"operation_id": string(existing.OperationID)}, nil)}
	}
	if existing.SchemaVersion == 3 || want.SchemaVersion == 3 || existing.ProjectCommand != nil || want.ProjectCommand != nil {
		if existing.ProjectCommand == nil || want.ProjectCommand == nil {
			return existing, false, app.StoreResult{Durability: app.DurableChange, Err: failure.New(failure.ProjectCommandBindingConflict, map[string]string{"operation_id": string(existing.OperationID)}, nil)}
		}
		existingDigest, existingErr := existing.ProjectCommand.Digest()
		wantDigest, wantErr := want.ProjectCommand.Digest()
		if existingErr != nil || wantErr != nil || existingDigest != wantDigest {
			return existing, false, app.StoreResult{Durability: app.DurableChange, Err: failure.New(failure.ProjectCommandBindingConflict, map[string]string{"operation_id": string(existing.OperationID)}, nil)}
		}
	}
	if existing.Persistent != want.Persistent || existing.SessionName != want.SessionName {
		return existing, false, app.StoreResult{Durability: app.DurableChange, Err: failure.New(failure.OperationConflict, map[string]string{"operation_id": string(existing.OperationID)}, nil)}
	}
	return existing, false, r.ensureSessionMetadata(existing)
}

func validateReservation(v operation.Reservation) error {
	if v.OperationID == "" || v.SessionID == "" {
		return fmt.Errorf("invalid reservation")
	}
	switch v.SchemaVersion {
	case 1:
		if v.Fingerprint == "" || v.ProjectCommand != nil || v.Intent != nil || v.EnvironmentBinding != nil {
			return fmt.Errorf("invalid reservation")
		}
	case 2:
		if v.RequestFingerprint == "" || v.ExecutionFingerprint == "" || v.ProjectCommand != nil {
			return fmt.Errorf("invalid reservation")
		}
		if v.StructuredAdapter != "" && !operation.ValidStructuredAdapterID(v.StructuredAdapter) {
			return fmt.Errorf("invalid reservation")
		}
		if v.Intent != nil {
			if err := v.Intent.Validate(); err != nil {
				return fmt.Errorf("invalid reservation: %w", err)
			}
		}
		switch v.ExecutionMode {
		case "":
			// Legacy v2 reservation written before explicit execution-mode binding.
		case operation.ExecutionModeShell:
			if v.Command == "" || len(v.Argv) != 0 || v.Shell == "" || v.Executable == "" {
				return fmt.Errorf("invalid reservation")
			}
		case operation.ExecutionModeArgv:
			if len(v.Argv) == 0 || v.Argv[0] == "" || v.Command != "" || v.Shell != "" || v.Executable == "" {
				return fmt.Errorf("invalid reservation")
			}
		default:
			return fmt.Errorf("invalid reservation")
		}
	case 3:
		if v.RequestFingerprint == "" || v.ExecutionFingerprint == "" || v.ProjectCommand == nil || v.Intent != nil {
			return fmt.Errorf("invalid reservation")
		}
		if v.StructuredAdapter != "" && !operation.ValidStructuredAdapterID(v.StructuredAdapter) {
			return fmt.Errorf("invalid reservation")
		}
		if err := v.ProjectCommand.Validate(); err != nil {
			return fmt.Errorf("invalid reservation: %w", err)
		}
		if v.ExecutionMode != operation.ExecutionModeArgv || v.Command != "" || v.Shell != "" || v.Executable == "" || len(v.Argv) == 0 || v.Argv[0] == "" {
			return fmt.Errorf("invalid reservation")
		}
		if !slices.Equal(v.Argv, v.ProjectCommand.ResolvedArgv) || v.CWD != v.ProjectCommand.ResolvedCWD || v.LogicalCWD != v.ProjectCommand.LogicalCWD || v.WorkspaceID == "" {
			return fmt.Errorf("invalid reservation")
		}
	case 4:
		if err := validatePersistentReservation(v); err != nil {
			return err
		}
	default:
		return fmt.Errorf("invalid reservation")
	}
	if v.EnvironmentBinding != nil {
		if err := v.EnvironmentBinding.Validate(); err != nil {
			return fmt.Errorf("invalid reservation environment binding: %w", err)
		}
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
	return r.writeSessionMetadata(string(v.SessionID), snap)
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

func validatePersistentReservation(v operation.Reservation) error {
	if !v.Persistent || v.RequestFingerprint == "" || v.ExecutionFingerprint == "" {
		return fmt.Errorf("invalid persistent reservation")
	}
	if v.TTY {
		return fmt.Errorf("invalid persistent reservation")
	}
	if v.SessionName != "" {
		if err := persistentsession.ValidateSessionName(v.SessionName); err != nil {
			return fmt.Errorf("invalid persistent reservation: %w", err)
		}
	}
	if v.StructuredAdapter != "" && !operation.ValidStructuredAdapterID(v.StructuredAdapter) {
		return fmt.Errorf("invalid persistent reservation")
	}
	if v.ProjectCommand != nil {
		if v.Intent != nil || v.Evidence != nil || v.ExecutionMode != operation.ExecutionModeArgv || v.Command != "" || v.Shell != "" || v.Executable == "" || len(v.Argv) == 0 || v.Argv[0] == "" {
			return fmt.Errorf("invalid persistent typed reservation")
		}
		if err := v.ProjectCommand.Validate(); err != nil {
			return fmt.Errorf("invalid persistent typed reservation: %w", err)
		}
		if !slices.Equal(v.Argv, v.ProjectCommand.ResolvedArgv) || v.CWD != v.ProjectCommand.ResolvedCWD || v.LogicalCWD != v.ProjectCommand.LogicalCWD || v.WorkspaceID == "" {
			return fmt.Errorf("invalid persistent typed reservation")
		}
		return nil
	}
	if v.Intent != nil {
		if err := v.Intent.Validate(); err != nil {
			return fmt.Errorf("invalid persistent reservation: %w", err)
		}
	}
	switch v.ExecutionMode {
	case operation.ExecutionModeShell:
		if v.Command == "" || len(v.Argv) != 0 || v.Shell == "" || v.Executable == "" {
			return fmt.Errorf("invalid persistent reservation")
		}
	case operation.ExecutionModeArgv:
		if len(v.Argv) == 0 || v.Argv[0] == "" || v.Command != "" || v.Shell != "" || v.Executable == "" {
			return fmt.Errorf("invalid persistent reservation")
		}
	default:
		return fmt.Errorf("invalid persistent reservation")
	}
	return nil
}
