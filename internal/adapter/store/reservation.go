package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	delegatedsession "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	inputtrace "github.com/maemreyo/shellbeam/internal/core/inputtrace"
	"github.com/maemreyo/shellbeam/internal/core/observation"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	persistentsession "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

// capacityRetryAfterMS is a hint, not a promise: a slot frees when a session
// ends, which the daemon cannot predict. It is short because the common case is
// a burst of work clearing quickly, and the active and limit counts alongside it
// are what let a caller decide whether waiting is worth it at all.
const capacityRetryAfterMS = 250

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
		// Say what the capacity actually is. A bare capacity_exceeded left an
		// agent unable to tell a daemon that is genuinely busy from one whose
		// slots had leaked, so it either retried blindly or concluded ShellBeam
		// was broken and asked for a restart.
		return existing, false, app.StoreResult{Durability: app.NoDurableChange, Err: failure.New(
			failure.CapacityExceeded,
			map[string]string{
				"active":         strconv.Itoa(active),
				"limit":          strconv.Itoa(r.limits.MaxSessions),
				"retry_after_ms": strconv.Itoa(capacityRetryAfterMS),
			}, nil)}
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
	created, metadata := r.finalizeReservationMetadata(want, seq)
	return want, created, metadata
}

func (r *Repository) finalizeReservationMetadata(want operation.Reservation, observationSeq observation.ChangeSeq) (bool, app.StoreResult) {
	metadata := withObservationSeq(r.ensureSessionMetadata(want), observationSeq)
	if metadata.Err == nil {
		return true, metadata
	}
	if metadata.Durability == app.AmbiguousChange {
		if compensationErr := r.finalizeAmbiguousAdmission(want); compensationErr != nil {
			metadata.Err = errors.Join(metadata.Err, compensationErr)
		}
	}
	return false, metadata
}

// finalizeAmbiguousAdmission closes the ownership gap created when session
// metadata was renamed into place but its parent directory sync failed. The
// operation reservation is already durable at this point, but Start must not
// be authorized because metadata durability is ambiguous. Since no runtime
// owner has been created yet, the only safe same-process outcome is canonical
// Abandoned/Ambiguous terminal state before returning the original error.
func (r *Repository) finalizeAmbiguousAdmission(want operation.Reservation) error {
	snap, err := r.LoadSession(context.Background(), want.SessionID)
	if err != nil {
		return fmt.Errorf("load ambiguous admission metadata: %w", err)
	}
	if snap.OperationID != string(want.OperationID) || snap.SessionID != string(want.SessionID) {
		return fmt.Errorf("ambiguous admission metadata identity mismatch")
	}
	if snap.State.Terminal() {
		return nil
	}
	rec := abandonedReceipt(snap, want, true, want.DaemonIncarnation)
	rec.FailureReason = "admission_metadata_ambiguous"
	delay := 25 * time.Millisecond
	for {
		result := r.PublishTerminal(context.Background(), rec)
		if result.Err == nil {
			return nil
		}
		time.Sleep(delay)
		if delay < time.Second {
			delay *= 2
			if delay > time.Second {
				delay = time.Second
			}
		}
	}
}

func (r *Repository) replayReservation(want, existing operation.Reservation) (operation.Reservation, bool, app.StoreResult) {
	if existing.EffectiveRequestFingerprint() != want.EffectiveRequestFingerprint() {
		return existing, false, app.StoreResult{Durability: app.DurableChange, Err: failure.New(failure.OperationConflict, map[string]string{"operation_id": string(existing.OperationID)}, nil)}
	}
	if existing.ObservationBindingFingerprint != want.ObservationBindingFingerprint {
		return existing, false, app.StoreResult{Durability: app.DurableChange, Err: failure.New(failure.OperationMetadataConflict, map[string]string{"operation_id": string(existing.OperationID)}, nil)}
	}
	if !sameTraceBinding(existing.Trace, want.Trace) {
		return existing, false, app.StoreResult{Durability: app.DurableChange, Err: failure.New(failure.OperationMetadataConflict, map[string]string{"operation_id": string(existing.OperationID), "field": "input_trace"}, nil)}
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
	if existing.Persistent != want.Persistent || existing.SessionMode != want.SessionMode || existing.AuthorityEpoch != want.AuthorityEpoch || existing.SessionName != want.SessionName {
		return existing, false, app.StoreResult{Durability: app.DurableChange, Err: failure.New(failure.OperationConflict, map[string]string{"operation_id": string(existing.OperationID)}, nil)}
	}
	return existing, false, r.ensureSessionMetadata(existing)
}

func sameTraceBinding(a, b *inputtrace.InstrumentationBinding) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	aDigest, aErr := a.Digest()
	bDigest, bErr := b.Digest()
	return aErr == nil && bErr == nil && aDigest == bDigest
}

func validateReservation(v operation.Reservation) error {
	if v.OperationID == "" || v.SessionID == "" {
		return fmt.Errorf("invalid reservation")
	}
	if v.ResourceLimits != nil {
		if v.SchemaVersion == 1 || v.SchemaVersion == 4 {
			return fmt.Errorf("invalid reservation")
		}
		if err := v.ResourceLimits.Validate(); err != nil {
			return fmt.Errorf("invalid reservation resource limits: %w", err)
		}
	}
	if v.SchemaVersion != 5 && (v.SessionMode != "" || v.AuthorityEpoch != 0) {
		return fmt.Errorf("invalid reservation")
	}
	if err := validateReservationSchema(v); err != nil {
		return err
	}
	if v.EnvironmentBinding != nil {
		if err := v.EnvironmentBinding.Validate(); err != nil {
			return fmt.Errorf("invalid reservation environment binding: %w", err)
		}
	}
	if v.Trace != nil {
		if err := v.Trace.Validate(); err != nil {
			return fmt.Errorf("invalid reservation input trace binding: %w", err)
		}
	}
	return nil
}

func validateReservationSchema(v operation.Reservation) error {
	switch v.SchemaVersion {
	case 1:
		if v.Fingerprint == "" || v.ProjectCommand != nil || v.Intent != nil || v.EnvironmentBinding != nil || v.Trace != nil {
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
	case 5:
		if err := validateDelegatedReservation(v); err != nil {
			return err
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

func validateDelegatedReservation(v operation.Reservation) error {
	if v.SessionMode != delegatedsession.ModeDelegatedInteractive || v.AuthorityEpoch != 1 || v.Persistent || v.TTY {
		return fmt.Errorf("invalid delegated reservation")
	}
	if v.RequestFingerprint == "" || v.ExecutionFingerprint == "" || v.Evidence != nil {
		return fmt.Errorf("invalid delegated reservation")
	}
	if v.SessionName != "" {
		if err := persistentsession.ValidateSessionName(v.SessionName); err != nil {
			return fmt.Errorf("invalid delegated reservation: %w", err)
		}
	}
	if v.StructuredAdapter != "" && !operation.ValidStructuredAdapterID(v.StructuredAdapter) {
		return fmt.Errorf("invalid delegated reservation")
	}
	if v.ProjectCommand != nil {
		if v.Intent != nil || v.ExecutionMode != operation.ExecutionModeArgv || v.Command != "" || v.Shell != "" || v.Executable == "" || len(v.Argv) == 0 || v.Argv[0] == "" {
			return fmt.Errorf("invalid delegated typed reservation")
		}
		if err := v.ProjectCommand.Validate(); err != nil {
			return fmt.Errorf("invalid delegated typed reservation: %w", err)
		}
		if !slices.Equal(v.Argv, v.ProjectCommand.ResolvedArgv) || v.CWD != v.ProjectCommand.ResolvedCWD || v.LogicalCWD != v.ProjectCommand.LogicalCWD || v.WorkspaceID == "" {
			return fmt.Errorf("invalid delegated typed reservation")
		}
		return nil
	}
	if v.Intent != nil {
		if err := v.Intent.Validate(); err != nil {
			return fmt.Errorf("invalid delegated reservation: %w", err)
		}
	}
	switch v.ExecutionMode {
	case operation.ExecutionModeShell:
		if v.Command == "" || len(v.Argv) != 0 || v.Shell == "" || v.Executable == "" {
			return fmt.Errorf("invalid delegated reservation")
		}
	case operation.ExecutionModeArgv:
		if len(v.Argv) == 0 || v.Argv[0] == "" || v.Command != "" || v.Shell != "" || v.Executable == "" {
			return fmt.Errorf("invalid delegated reservation")
		}
	default:
		return fmt.Errorf("invalid delegated reservation")
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
