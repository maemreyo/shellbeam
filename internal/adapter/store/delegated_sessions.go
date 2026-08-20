package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

const (
	maxDelegatedBindingBytes        = 16 << 10
	maxDelegatedProviderRefBytes    = 16 << 10
	maxDelegatedRecoveryMarkerBytes = 32 << 10
)

var _ app.DelegatedSessionStore = (*Repository)(nil)

type delegatedRecoveryMarker struct {
	SchemaVersion int                   `json:"schema_version"`
	Binding       delegated.Binding     `json:"binding"`
	ProviderRef   delegated.ProviderRef `json:"provider_ref"`
}

func (m delegatedRecoveryMarker) validate() error {
	if m.SchemaVersion != 1 || m.Binding.Lifecycle != delegated.LifecycleProvisioning {
		return fmt.Errorf("invalid delegated recovery marker")
	}
	if err := m.Binding.Validate(); err != nil {
		return err
	}
	if err := m.ProviderRef.Validate(); err != nil {
		return err
	}
	return validateDelegatedProviderRefBinding(m.Binding, m.ProviderRef)
}

func (r *Repository) initDelegatedSessionStore() error {
	for _, path := range []string{r.delegatedSessionDir(), r.delegatedBindingDir(), r.delegatedProviderRefDir(), r.delegatedRecoveryDir(), r.delegatedCaptureDir(), r.delegatedMutationDir()} {
		if err := ensurePrivateDir(path); err != nil {
			return fmt.Errorf("delegated sessions: %w", err)
		}
	}
	return nil
}

func (r *Repository) ReserveDelegatedBinding(ctx context.Context, want delegated.Binding, ref delegated.ProviderRef) (delegated.Binding, bool, app.StoreResult) {
	if err := ctx.Err(); err != nil {
		return delegated.Binding{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if err := want.Validate(); err != nil || want.Lifecycle != delegated.LifecycleProvisioning {
		return delegated.Binding{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: delegatedStateConflict(want, "invalid_binding", err)}
	}
	if err := ref.Validate(); err != nil {
		return delegated.Binding{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: delegatedStateConflict(want, "invalid_provider_ref", err)}
	}
	if err := validateDelegatedProviderRefBinding(want, ref); err != nil {
		return delegated.Binding{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: failure.New(failure.DelegatedProviderMismatch, map[string]string{
			"session_id": want.SessionID, "provider_id": ref.ProviderID, "provider_version": fmt.Sprint(ref.ProviderVersion),
			"expected_provider_id": want.ProviderID, "expected_provider_version": fmt.Sprint(want.ProviderVersion),
		}, err)}
	}
	sid, err := operation.ParseSessionID(want.SessionID)
	if err != nil {
		return delegated.Binding{}, false, app.StoreResult{Err: err}
	}
	opID, err := operation.ParseID(want.OperationID)
	if err != nil {
		return delegated.Binding{}, false, app.StoreResult{Err: err}
	}

	r.delegatedSessionMu.Lock()
	defer r.delegatedSessionMu.Unlock()
	reservation, err := r.LoadOperation(ctx, opID)
	if err != nil {
		return delegated.Binding{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: delegatedStateConflict(want, "reservation_missing", err)}
	}
	if err := validateDelegatedReservationBinding(reservation, want); err != nil {
		return delegated.Binding{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: delegatedStateConflict(want, "reservation_mismatch", err)}
	}
	if existing, loadErr := r.loadDelegatedBindingLocked(sid); loadErr == nil && !reflect.DeepEqual(existing, want) {
		return existing, false, app.StoreResult{Durability: app.DurableChange, Err: delegatedBindingConflict(existing, want)}
	} else if loadErr != nil && !errors.Is(loadErr, ErrNotFound) {
		return delegated.Binding{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: loadErr}
	}

	marker := delegatedRecoveryMarker{SchemaVersion: 1, Binding: want, ProviderRef: ref}
	priorDurability := app.NoDurableChange
	markerResult := r.ensureDelegatedRecoveryMarkerLocked(marker)
	if markerResult.Err != nil {
		return delegated.Binding{}, false, markerResult
	}
	priorDurability = strongerDelegatedDurability(priorDurability, markerResult.Durability)
	refResult := r.ensureDelegatedProviderRefLocked(ref)
	if refResult.Err != nil {
		refResult.Durability = strongerDelegatedDurability(priorDurability, refResult.Durability)
		return delegated.Binding{}, false, refResult
	}
	priorDurability = strongerDelegatedDurability(priorDurability, refResult.Durability)

	path := r.delegatedBindingPath(sid)
	var existing delegated.Binding
	if err := readPrivateJSON(path, maxDelegatedBindingBytes, &existing); err == nil {
		if existing.Validate() != nil {
			return existing, false, app.StoreResult{Durability: app.DurableChange, Err: delegatedStateConflict(want, "binding_corrupt", nil)}
		}
		if reflect.DeepEqual(existing, want) {
			return existing, false, app.StoreResult{Durability: app.DurableChange}
		}
		return existing, false, app.StoreResult{Durability: app.DurableChange, Err: delegatedBindingConflict(existing, want)}
	} else if !errors.Is(err, ErrNotFound) {
		return existing, false, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	result := r.writer.Create(path, want)
	if result.Err == nil {
		return want, true, result
	}
	result.Durability = strongerDelegatedDurability(priorDurability, result.Durability)
	if errors.Is(result.Err, os.ErrExist) {
		if err := readPrivateJSON(path, maxDelegatedBindingBytes, &existing); err == nil && reflect.DeepEqual(existing, want) {
			return existing, false, app.StoreResult{Durability: app.DurableChange}
		}
	}
	return delegated.Binding{}, false, result
}

func (r *Repository) AdvanceDelegatedBinding(ctx context.Context, want delegated.Binding) app.StoreResult {
	if err := ctx.Err(); err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if err := want.Validate(); err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: delegatedStateConflict(want, "invalid_binding", err)}
	}
	sid, err := operation.ParseSessionID(want.SessionID)
	if err != nil {
		return app.StoreResult{Err: err}
	}
	r.delegatedSessionMu.Lock()
	defer r.delegatedSessionMu.Unlock()
	existing, err := r.loadDelegatedBindingLocked(sid)
	if err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if !sameDelegatedBindingIdentity(existing, want) {
		return app.StoreResult{Durability: app.DurableChange, Err: delegatedBindingConflict(existing, want)}
	}
	if reflect.DeepEqual(existing, want) {
		return app.StoreResult{Durability: app.DurableChange}
	}
	ownerChangedWithoutRotation := want.DesiredOwner != existing.DesiredOwner && want.AuthorityEpoch <= existing.AuthorityEpoch
	if !want.UpdatedAt.After(existing.UpdatedAt) || want.AuthorityEpoch < existing.AuthorityEpoch || ownerChangedWithoutRotation || !validDelegatedLifecycleTransition(existing.Lifecycle, want.Lifecycle) {
		return app.StoreResult{Durability: app.DurableChange, Err: delegatedStateConflict(want, "lifecycle_or_epoch_conflict", nil)}
	}
	result := r.writer.Replace(r.delegatedBindingPath(sid), want)
	if result.Err != nil {
		return result
	}
	if want.Lifecycle == delegated.LifecycleTerminal || want.Lifecycle == delegated.LifecycleLost {
		if err := r.removeDelegatedRecoveryMarkerLocked(sid); err != nil {
			return app.StoreResult{Durability: app.DurableChange, Err: err}
		}
	}
	return result
}

func (r *Repository) LoadDelegatedBinding(ctx context.Context, sid operation.SessionID) (delegated.Binding, error) {
	if err := ctx.Err(); err != nil {
		return delegated.Binding{}, err
	}
	if _, err := operation.ParseSessionID(string(sid)); err != nil {
		return delegated.Binding{}, err
	}
	r.delegatedSessionMu.Lock()
	defer r.delegatedSessionMu.Unlock()
	return r.loadDelegatedBindingLocked(sid)
}

func (r *Repository) loadDelegatedBindingLocked(sid operation.SessionID) (delegated.Binding, error) {
	var out delegated.Binding
	if err := readPrivateJSON(r.delegatedBindingPath(sid), maxDelegatedBindingBytes, &out); err != nil {
		return out, err
	}
	if err := out.Validate(); err != nil || out.SessionID != string(sid) {
		return out, delegatedStateConflict(out, "binding_corrupt", err)
	}
	return out, nil
}

func (r *Repository) LoadDelegatedProviderRef(ctx context.Context, sid operation.SessionID) (delegated.ProviderRef, error) {
	if err := ctx.Err(); err != nil {
		return delegated.ProviderRef{}, err
	}
	if _, err := operation.ParseSessionID(string(sid)); err != nil {
		return delegated.ProviderRef{}, err
	}
	r.delegatedSessionMu.Lock()
	defer r.delegatedSessionMu.Unlock()
	return r.loadDelegatedProviderRefLocked(sid)
}

func (r *Repository) loadDelegatedProviderRefLocked(sid operation.SessionID) (delegated.ProviderRef, error) {
	var out delegated.ProviderRef
	if err := readPrivateJSON(r.delegatedProviderRefPath(sid), maxDelegatedProviderRefBytes, &out); err != nil {
		return out, err
	}
	if err := out.Validate(); err != nil || out.SessionID != string(sid) {
		return out, fmt.Errorf("invalid delegated provider ref")
	}
	return out, nil
}

func (r *Repository) ensureDelegatedProviderRefLocked(ref delegated.ProviderRef) app.StoreResult {
	path := r.delegatedProviderRefPath(operation.SessionID(ref.SessionID))
	var existing delegated.ProviderRef
	if err := readPrivateJSON(path, maxDelegatedProviderRefBytes, &existing); err == nil {
		if reflect.DeepEqual(existing, ref) {
			return app.StoreResult{Durability: app.DurableChange}
		}
		return app.StoreResult{Durability: app.DurableChange, Err: failure.New(failure.DelegatedProviderMismatch, map[string]string{"session_id": ref.SessionID, "provider_id": ref.ProviderID}, nil)}
	} else if !errors.Is(err, ErrNotFound) {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	result := r.writer.Create(path, ref)
	if errors.Is(result.Err, os.ErrExist) {
		return r.ensureDelegatedProviderRefLocked(ref)
	}
	return result
}

func strongerDelegatedDurability(a, b app.Durability) app.Durability {
	if a == app.AmbiguousChange || b == app.AmbiguousChange {
		return app.AmbiguousChange
	}
	if a == app.DurableChange || b == app.DurableChange {
		return app.DurableChange
	}
	return app.NoDurableChange
}

func sameDelegatedBindingIdentity(a, b delegated.Binding) bool {
	return a.SchemaVersion == b.SchemaVersion && a.SessionID == b.SessionID && a.OperationID == b.OperationID && a.SessionName == b.SessionName && a.SessionMode == b.SessionMode && a.ProviderID == b.ProviderID && a.ProviderVersion == b.ProviderVersion && a.CreatedAt.Equal(b.CreatedAt)
}

func validDelegatedLifecycleTransition(from, to delegated.Lifecycle) bool {
	if from == to {
		return true
	}
	switch from {
	case delegated.LifecycleProvisioning:
		return to == delegated.LifecycleLive || to == delegated.LifecycleTerminal || to == delegated.LifecycleLost
	case delegated.LifecycleLive:
		return to == delegated.LifecycleTerminal || to == delegated.LifecycleLost
	default:
		return false
	}
}

func validateDelegatedReservationBinding(r operation.Reservation, b delegated.Binding) error {
	if r.SchemaVersion != 5 || r.SessionMode != delegated.ModeDelegatedInteractive || string(r.SessionID) != b.SessionID || string(r.OperationID) != b.OperationID || r.SessionName != b.SessionName || r.AuthorityEpoch != b.AuthorityEpoch {
		return fmt.Errorf("delegated reservation binding mismatch")
	}
	return nil
}

func validateDelegatedProviderRefBinding(b delegated.Binding, ref delegated.ProviderRef) error {
	if b.SessionID != ref.SessionID || b.ProviderID != ref.ProviderID || b.ProviderVersion != ref.ProviderVersion || !b.CreatedAt.Equal(ref.CreatedAt) {
		return fmt.Errorf("delegated provider ref binding mismatch")
	}
	return nil
}

func delegatedStateConflict(b delegated.Binding, reason string, cause error) error {
	return failure.New(failure.DelegatedReconcileBlocked, map[string]string{"session_id": b.SessionID, "provider_id": b.ProviderID, "current_epoch": fmt.Sprint(b.AuthorityEpoch), "reason": reason}, cause)
}

func delegatedBindingConflict(existing, want delegated.Binding) error {
	if existing.ProviderID != want.ProviderID || existing.ProviderVersion != want.ProviderVersion {
		return failure.New(failure.DelegatedProviderMismatch, map[string]string{"session_id": existing.SessionID, "provider_id": want.ProviderID, "provider_version": fmt.Sprint(want.ProviderVersion), "expected_provider_id": existing.ProviderID, "expected_provider_version": fmt.Sprint(existing.ProviderVersion)}, nil)
	}
	return delegatedStateConflict(want, "binding_conflict", nil)
}
