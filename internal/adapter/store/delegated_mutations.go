package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

const maxDelegatedMutationRecordBytes = 16 << 10

func (r *Repository) LookupDelegatedMutation(ctx context.Context, id delegated.MutationIdentity) (delegated.MutationRecord, bool, error) {
	if err := ctx.Err(); err != nil {
		return delegated.MutationRecord{}, false, err
	}
	if err := id.Validate(); err != nil {
		return delegated.MutationRecord{}, false, err
	}
	r.delegatedSessionMu.Lock()
	defer r.delegatedSessionMu.Unlock()
	rec, err := r.loadDelegatedMutationLocked(id)
	if errors.Is(err, ErrNotFound) {
		return delegated.MutationRecord{}, false, nil
	}
	return rec, err == nil, err
}

func (r *Repository) ReserveDelegatedMutation(ctx context.Context, id delegated.MutationIdentity) (delegated.MutationRecord, bool, app.StoreResult) {
	if err := ctx.Err(); err != nil {
		return delegated.MutationRecord{}, false, app.StoreResult{Err: err}
	}
	if err := id.Validate(); err != nil {
		return delegated.MutationRecord{}, false, app.StoreResult{Err: failure.New(failure.InvalidInput, map[string]string{"field": "mutation"}, err)}
	}
	sid := operation.SessionID(id.SessionID)
	r.delegatedSessionMu.Lock()
	defer r.delegatedSessionMu.Unlock()
	if _, err := r.loadDelegatedBindingLocked(sid); err != nil {
		return delegated.MutationRecord{}, false, app.StoreResult{Err: err}
	}
	if existing, err := r.loadDelegatedMutationLocked(id); err == nil {
		if existing.Identity != id {
			return existing, false, app.StoreResult{Durability: app.DurableChange, Err: failure.New(failure.OperationConflict, nil, nil)}
		}
		return existing, false, app.StoreResult{Durability: app.DurableChange}
	} else if !errors.Is(err, ErrNotFound) {
		return delegated.MutationRecord{}, false, app.StoreResult{Err: err}
	}
	dir := r.delegatedSessionMutationDir(sid)
	if err := ensurePrivateDir(dir); err != nil {
		return delegated.MutationRecord{}, false, app.StoreResult{Err: err}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return delegated.MutationRecord{}, false, app.StoreResult{Err: err}
	}
	if len(entries) >= r.limits.MaxDelegatedMutationRecords {
		return delegated.MutationRecord{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: failure.New(failure.CapacityExceeded, map[string]string{"active": fmt.Sprint(len(entries)), "limit": fmt.Sprint(r.limits.MaxDelegatedMutationRecords)}, nil)}
	}
	now := r.now()
	rec := delegated.MutationRecord{SchemaVersion: delegated.MutationRecordSchemaVersion, Identity: id, State: delegated.MutationReserved, CreatedAt: now, UpdatedAt: now}
	result := r.writer.Create(r.delegatedMutationPath(id), rec)
	if result.Err == nil {
		return rec, true, result
	}
	if errors.Is(result.Err, os.ErrExist) {
		existing, err := r.loadDelegatedMutationLocked(id)
		if err == nil && existing.Identity == id {
			return existing, false, app.StoreResult{Durability: app.DurableChange}
		}
	}
	return delegated.MutationRecord{}, false, result
}

func (r *Repository) CompleteDelegatedMutation(ctx context.Context, id delegated.MutationIdentity, state delegated.MutationState, outcome string) (delegated.MutationRecord, app.StoreResult) {
	if err := ctx.Err(); err != nil {
		return delegated.MutationRecord{}, app.StoreResult{Err: err}
	}
	if err := state.Validate(); err != nil || state == delegated.MutationReserved {
		return delegated.MutationRecord{}, app.StoreResult{Err: failure.New(failure.InvalidInput, map[string]string{"field": "mutation_state"}, err)}
	}
	r.delegatedSessionMu.Lock()
	defer r.delegatedSessionMu.Unlock()
	current, err := r.loadDelegatedMutationLocked(id)
	if err != nil {
		return delegated.MutationRecord{}, app.StoreResult{Err: err}
	}
	if current.Identity != id {
		return current, app.StoreResult{Durability: app.DurableChange, Err: failure.New(failure.OperationConflict, nil, nil)}
	}
	if current.State == state && current.Outcome == outcome {
		return current, app.StoreResult{Durability: app.DurableChange}
	}
	if !validDelegatedMutationTransition(current.State, state) {
		return current, app.StoreResult{Durability: app.DurableChange, Err: failure.New(failure.OperationConflict, nil, nil)}
	}
	next := current
	next.State = state
	next.Outcome = outcome
	next.UpdatedAt = r.now()
	if err := next.Validate(); err != nil {
		return current, app.StoreResult{Err: err}
	}
	result := r.writer.Replace(r.delegatedMutationPath(id), next)
	if result.Err != nil {
		return current, result
	}
	return next, result
}

func (r *Repository) loadDelegatedMutationLocked(id delegated.MutationIdentity) (delegated.MutationRecord, error) {
	var rec delegated.MutationRecord
	if err := readPrivateJSON(r.delegatedMutationPath(id), maxDelegatedMutationRecordBytes, &rec); err != nil {
		return rec, err
	}
	if err := rec.Validate(); err != nil {
		return rec, err
	}
	if !sameDelegatedMutationLogicalIdentity(rec.Identity, id) {
		return rec, fmt.Errorf("delegated mutation logical identity mismatch")
	}
	return rec, nil
}

func sameDelegatedMutationLogicalIdentity(a, b delegated.MutationIdentity) bool {
	return a.SessionID == b.SessionID && a.Epoch == b.Epoch && a.Kind == b.Kind && a.IdempotencyID == b.IdempotencyID && a.Offset == b.Offset
}
func validDelegatedMutationTransition(from, to delegated.MutationState) bool {
	switch from {
	case delegated.MutationReserved:
		return to == delegated.MutationDelivered || to == delegated.MutationCompleted || to == delegated.MutationFailed || to == delegated.MutationOutcomeUnknown
	case delegated.MutationDelivered:
		return to == delegated.MutationCompleted || to == delegated.MutationFailed || to == delegated.MutationOutcomeUnknown
	case delegated.MutationOutcomeUnknown:
		return to == delegated.MutationDelivered || to == delegated.MutationCompleted || to == delegated.MutationFailed
	default:
		return false
	}
}

func (r *Repository) LoadDelegatedRecoveryState(ctx context.Context, sid operation.SessionID) (app.DelegatedRecoveryState, error) {
	if err := ctx.Err(); err != nil {
		return app.DelegatedRecoveryState{}, err
	}
	if _, err := operation.ParseSessionID(string(sid)); err != nil {
		return app.DelegatedRecoveryState{}, err
	}
	r.delegatedSessionMu.Lock()
	defer r.delegatedSessionMu.Unlock()
	binding, err := r.loadDelegatedBindingLocked(sid)
	if err != nil {
		return app.DelegatedRecoveryState{}, err
	}
	dir := r.delegatedSessionMutationDir(sid)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return app.DelegatedRecoveryState{}, nil
	}
	if err != nil {
		return app.DelegatedRecoveryState{}, delegatedRecoveryBlocked(binding, "mutation_ledger_read", err)
	}
	writes := make([]delegated.MutationIdentity, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			return app.DelegatedRecoveryState{}, delegatedRecoveryBlocked(binding, "mutation_ledger_corrupt", fmt.Errorf("unexpected mutation directory"))
		}
		var rec delegated.MutationRecord
		path := filepath.Join(dir, entry.Name())
		if err := readPrivateJSON(path, maxDelegatedMutationRecordBytes, &rec); err != nil {
			return app.DelegatedRecoveryState{}, delegatedRecoveryBlocked(binding, "mutation_ledger_corrupt", err)
		}
		if err := rec.Validate(); err != nil || rec.Identity.SessionID != string(sid) || entry.Name() != delegatedMutationKey(rec.Identity)+".json" {
			return app.DelegatedRecoveryState{}, delegatedRecoveryBlocked(binding, "mutation_ledger_corrupt", err)
		}
		switch rec.State {
		case delegated.MutationReserved, delegated.MutationDelivered, delegated.MutationOutcomeUnknown:
			return app.DelegatedRecoveryState{}, delegatedRecoveryBlocked(binding, "mutation_unresolved", nil)
		case delegated.MutationFailed:
			continue
		case delegated.MutationCompleted:
			if rec.Identity.Kind == delegated.MutationWrite {
				writes = append(writes, rec.Identity)
			}
		default:
			return app.DelegatedRecoveryState{}, delegatedRecoveryBlocked(binding, "mutation_ledger_corrupt", nil)
		}
	}
	sort.Slice(writes, func(i, j int) bool {
		if writes[i].Offset == writes[j].Offset {
			return writes[i].NextOffset < writes[j].NextOffset
		}
		return writes[i].Offset < writes[j].Offset
	})
	next := int64(0)
	for _, id := range writes {
		if id.Offset != next {
			return app.DelegatedRecoveryState{}, delegatedRecoveryBlocked(binding, "mutation_write_gap", nil)
		}
		next = id.NextOffset
	}
	return app.DelegatedRecoveryState{NextInputOffset: next}, nil
}

func (r *Repository) DelegatedOutputBytes(ctx context.Context, sid operation.SessionID) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if _, err := operation.ParseSessionID(string(sid)); err != nil {
		return 0, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return outputSize(filepath.Join(r.root, "sessions", string(sid), "output.log"))
}

func delegatedRecoveryBlocked(binding delegated.Binding, reason string, cause error) error {
	return failure.New(failure.DelegatedReconcileBlocked, map[string]string{
		"session_id":    binding.SessionID,
		"provider_id":   binding.ProviderID,
		"current_epoch": fmt.Sprint(binding.AuthorityEpoch),
		"reason":        reason,
	}, cause)
}
