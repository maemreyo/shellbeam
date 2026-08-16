package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/session"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	persistent "github.com/maemreyo/shellbeam/internal/core/persistentsession"
)

const maxPersistentRecoveryMarkerBytes = 20 << 10

type persistentRecoveryMarker struct {
	SchemaVersion int                `json:"schema_version"`
	Binding       persistent.Binding `json:"binding"`
}

func recoveryMarkerFor(binding persistent.Binding) persistentRecoveryMarker {
	initial := binding
	initial.Lifecycle = persistent.LifecycleProvisioning
	initial.UpdatedAt = initial.CreatedAt
	return persistentRecoveryMarker{SchemaVersion: 1, Binding: initial}
}

func (m persistentRecoveryMarker) validate() error {
	if m.SchemaVersion != 1 || m.Binding.Lifecycle != persistent.LifecycleProvisioning {
		return fmt.Errorf("invalid persistent recovery marker")
	}
	return m.Binding.Validate()
}

func (r *Repository) ListPersistentRecoveryCandidates(ctx context.Context) ([]persistent.Binding, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.persistentSessionMu.Lock()
	defer r.persistentSessionMu.Unlock()

	entries, err := os.ReadDir(r.persistentRecoveryDir())
	if err != nil {
		return nil, err
	}
	if len(entries) > r.limits.MaxSessions {
		return nil, failure.New(failure.PersistentHistoryExhausted, map[string]string{"reason": "recovery_index_limit"}, nil)
	}
	out := make([]persistent.Binding, 0, len(entries))
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			return nil, persistentStateConflict(persistent.Binding{}, "recovery_marker_entry", nil)
		}
		sessionID, parseErr := operation.ParseSessionID(strings.TrimSuffix(entry.Name(), ".json"))
		if parseErr != nil {
			return nil, persistentStateConflict(persistent.Binding{}, "recovery_marker_filename", parseErr)
		}
		marker, loadErr := r.loadPersistentRecoveryMarkerLocked(sessionID)
		if loadErr != nil {
			return nil, loadErr
		}
		binding, bindingErr := r.loadPersistentBindingLocked(sessionID)
		if errors.Is(bindingErr, ErrNotFound) {
			repaired, repairErr := r.repairPersistentBindingFromMarkerLocked(ctx, marker)
			if repairErr != nil {
				return nil, repairErr
			}
			binding = repaired
		} else if bindingErr != nil {
			return nil, bindingErr
		}
		if !samePersistentBindingIdentity(binding, marker.Binding) {
			return nil, persistentStateConflict(binding, "recovery_marker_identity", nil)
		}
		switch binding.Lifecycle {
		case persistent.LifecycleProvisioning, persistent.LifecycleLive:
			out = append(out, binding)
		case persistent.LifecycleTerminal, persistent.LifecycleLost:
			if err := r.removePersistentRecoveryMarkerLocked(sessionID); err != nil {
				return nil, err
			}
		default:
			return nil, persistentStateConflict(binding, "recovery_marker_lifecycle", nil)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SessionID < out[j].SessionID })
	return out, nil
}

func (r *Repository) createPersistentRecoveryMarker(marker persistentRecoveryMarker) error {
	r.persistentSessionMu.Lock()
	defer r.persistentSessionMu.Unlock()
	return r.createPersistentRecoveryMarkerLocked(marker)
}

func (r *Repository) ensurePersistentRecoveryMarkerLocked(binding persistent.Binding) app.StoreResult {
	marker := recoveryMarkerFor(binding)
	if err := marker.validate(); err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: persistentStateConflict(binding, "recovery_marker_invalid", err)}
	}
	path := r.persistentRecoveryPath(operation.SessionID(binding.SessionID))
	var existing persistentRecoveryMarker
	if err := readPrivateJSON(path, maxPersistentRecoveryMarkerBytes, &existing); err == nil {
		if existing.validate() != nil || !samePersistentBindingIdentity(existing.Binding, marker.Binding) {
			return app.StoreResult{Durability: app.DurableChange, Err: persistentStateConflict(binding, "recovery_marker_conflict", nil)}
		}
		return app.StoreResult{Durability: app.DurableChange}
	} else if !errors.Is(err, ErrNotFound) {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	result := r.writer.Create(path, marker)
	if result.Err == nil {
		return result
	}
	if errors.Is(result.Err, os.ErrExist) {
		if err := readPrivateJSON(path, maxPersistentRecoveryMarkerBytes, &existing); err == nil && existing.validate() == nil && samePersistentBindingIdentity(existing.Binding, marker.Binding) {
			return app.StoreResult{Durability: app.DurableChange}
		}
	}
	return result
}

func (r *Repository) createPersistentRecoveryMarkerLocked(marker persistentRecoveryMarker) error {
	if err := marker.validate(); err != nil {
		return err
	}
	result := r.ensurePersistentRecoveryMarkerLocked(marker.Binding)
	return result.Err
}

func (r *Repository) loadPersistentRecoveryMarkerLocked(sessionID operation.SessionID) (persistentRecoveryMarker, error) {
	var marker persistentRecoveryMarker
	if err := readPrivateJSON(r.persistentRecoveryPath(sessionID), maxPersistentRecoveryMarkerBytes, &marker); err != nil {
		return marker, err
	}
	if err := marker.validate(); err != nil || marker.Binding.SessionID != string(sessionID) {
		return marker, persistentStateConflict(marker.Binding, "recovery_marker_corrupt", err)
	}
	return marker, nil
}

func (r *Repository) repairPersistentBindingFromMarkerLocked(ctx context.Context, marker persistentRecoveryMarker) (persistent.Binding, error) {
	binding := marker.Binding
	operationID, err := operation.ParseID(binding.OperationID)
	if err != nil {
		return persistent.Binding{}, persistentStateConflict(binding, "recovery_marker_operation", err)
	}
	reservation, err := r.LoadOperation(ctx, operationID)
	if err != nil {
		return persistent.Binding{}, persistentStateConflict(binding, "recovery_marker_reservation_missing", err)
	}
	if err := validatePersistentReservationBinding(reservation, binding); err != nil {
		return persistent.Binding{}, persistentStateConflict(binding, "recovery_marker_reservation_mismatch", err)
	}
	result := r.writer.Create(r.persistentBindingPath(operation.SessionID(binding.SessionID)), binding)
	if result.Err == nil {
		return binding, nil
	}
	if errors.Is(result.Err, os.ErrExist) {
		current, loadErr := r.loadPersistentBindingLocked(operation.SessionID(binding.SessionID))
		if loadErr == nil && samePersistentBindingIdentity(current, binding) {
			return current, nil
		}
	}
	return persistent.Binding{}, result.Err
}

func (r *Repository) removePersistentRecoveryMarkerLocked(sessionID operation.SessionID) error {
	path := r.persistentRecoveryPath(sessionID)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return syncPrivateDir(r.persistentRecoveryDir())
}

func (r *Repository) persistentRecoveryDir() string {
	return filepath.Join(r.persistentSessionDir(), "active")
}

func (r *Repository) persistentRecoveryPath(sessionID operation.SessionID) string {
	return filepath.Join(r.persistentRecoveryDir(), string(sessionID)+".json")
}

func (r *Repository) AbandonPersistentSession(ctx context.Context, candidate persistent.Binding, incarnation, reason string) app.StoreResult {
	if err := ctx.Err(); err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if !validPersistentLossReason(reason) {
		return app.StoreResult{Durability: app.NoDurableChange, Err: persistentStateConflict(candidate, "loss_reason", nil)}
	}
	current, err := r.LoadPersistentBinding(ctx, operation.SessionID(candidate.SessionID))
	if err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if !samePersistentBindingIdentity(current, candidate) {
		return app.StoreResult{Durability: app.DurableChange, Err: persistentStateConflict(current, "loss_identity", nil)}
	}
	if existing, receiptErr := r.LoadReceipt(ctx, operation.SessionID(current.SessionID)); receiptErr == nil {
		if existing.State != session.Abandoned || existing.Outcome != session.Ambiguous || !existing.Persistent || existing.SessionID != current.SessionID || existing.OperationID != current.OperationID {
			return app.StoreResult{Durability: app.DurableChange, Err: persistentStateConflict(current, "canonical_terminal_exists", nil)}
		}
		return r.markPersistentBindingLost(ctx, current)
	} else if !errors.Is(receiptErr, ErrNotFound) {
		return app.StoreResult{Durability: app.NoDurableChange, Err: receiptErr}
	}
	if current.Lifecycle == persistent.LifecycleTerminal || current.Lifecycle == persistent.LifecycleLost {
		return app.StoreResult{Durability: app.DurableChange, Err: persistentStateConflict(current, "loss_lifecycle", nil)}
	}
	snapshot, err := r.LoadSession(ctx, operation.SessionID(current.SessionID))
	if err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	reservation, err := r.LoadOperation(ctx, operation.ID(current.OperationID))
	if err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if err := validatePersistentReservationBinding(reservation, current); err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: persistentStateConflict(current, "loss_reservation", err)}
	}
	rec := abandonedReceipt(snapshot, reservation, true, incarnation)
	rec.FailureReason = reason
	rec.OutputBytes = snapshot.OutputBytes
	published := r.PublishTerminal(ctx, rec)
	if published.Err != nil {
		return published
	}
	return r.markPersistentBindingLost(ctx, current)
}

func (r *Repository) markPersistentBindingLost(ctx context.Context, current persistent.Binding) app.StoreResult {
	if current.Lifecycle == persistent.LifecycleLost {
		return app.StoreResult{Durability: app.DurableChange}
	}
	if current.Lifecycle == persistent.LifecycleTerminal {
		return app.StoreResult{Durability: app.DurableChange, Err: persistentStateConflict(current, "loss_after_terminal", nil)}
	}
	lost := current
	lost.Lifecycle = persistent.LifecycleLost
	lost.UpdatedAt = r.now()
	if !lost.UpdatedAt.After(current.UpdatedAt) {
		lost.UpdatedAt = current.UpdatedAt.Add(time.Nanosecond)
	}
	return r.AdvancePersistentBinding(ctx, lost)
}

func validPersistentLossReason(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.' {
			continue
		}
		return false
	}
	return true
}
