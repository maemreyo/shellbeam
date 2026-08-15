package store

import (
	"context"
	"errors"
	"fmt"
	"os"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/mutationscope"
)

func (r *Repository) loadMutationScopePendingLocked() (mutationScopePending, bool, error) {
	var pending mutationScopePending
	err := readMutationScopeJSON(r.mutationScopePendingPath(), &pending)
	if errors.Is(err, ErrNotFound) {
		return pending, false, nil
	}
	if err != nil {
		return pending, false, err
	}
	if err := pending.validate(); err != nil {
		return pending, false, err
	}
	return pending, true, nil
}

func (r *Repository) loadMutationScopeClaimUnlocked(mutationID string) (mutationScopeClaim, bool, error) {
	var claim mutationScopeClaim
	err := readMutationScopeJSON(r.mutationScopeClaimPath(mutationID), &claim)
	if errors.Is(err, ErrNotFound) {
		return claim, false, nil
	}
	if err != nil {
		return claim, false, err
	}
	if claim.MutationID != mutationID {
		return claim, false, fmt.Errorf("mutation scope claim identity mismatch")
	}
	if err := claim.validate(); err != nil {
		return claim, false, err
	}
	return claim, true, nil
}

func (r *Repository) replayMutationScopeClaimLocked(mutationID, scopeID, fingerprint string) (core.MutationReceipt, bool, app.StoreResult) {
	claim, found, err := r.loadMutationScopeClaimUnlocked(mutationID)
	if err != nil {
		return core.MutationReceipt{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if !found {
		return core.MutationReceipt{}, false, app.StoreResult{}
	}
	if claim.ScopeID != scopeID || claim.RequestFingerprint != fingerprint {
		return core.MutationReceipt{}, true, app.StoreResult{Durability: app.DurableChange, Err: failure.New(failure.MutationMetadataConflict, map[string]string{"mutation_id": mutationID, "scope_id": scopeID}, nil)}
	}
	if claim.Status == "committed" {
		return *claim.Receipt, true, app.StoreResult{Durability: app.DurableChange}
	}
	result := r.reconcileMutationScopePendingLocked(context.Background())
	if result.Err != nil {
		return core.MutationReceipt{}, true, result
	}
	committed, ok, err := r.loadMutationScopeClaimUnlocked(mutationID)
	if err != nil || !ok || committed.Status != "committed" {
		if err == nil {
			err = fmt.Errorf("mutation claim not committed after reconcile")
		}
		return core.MutationReceipt{}, true, app.StoreResult{Durability: app.AmbiguousChange, Err: err}
	}
	return *committed.Receipt, true, app.StoreResult{Durability: app.DurableChange}
}

func (r *Repository) createMutationScopePendingLocked(pending mutationScopePending) app.StoreResult {
	result := r.writer.Create(r.mutationScopePendingPath(), pending)
	if result.Err == nil {
		return result
	}
	if errors.Is(result.Err, os.ErrExist) {
		current, found, err := r.loadMutationScopePendingLocked()
		if err == nil && found && samePending(current, pending) {
			return app.StoreResult{Durability: app.DurableChange}
		}
		if err == nil {
			err = fmt.Errorf("mutation scope pending conflict")
		}
		return app.StoreResult{Durability: app.AmbiguousChange, Err: err}
	}
	return result
}

func (r *Repository) ensureMutationScopeClaimPreparedLocked(pending mutationScopePending) app.StoreResult {
	claim, found, err := r.loadMutationScopeClaimUnlocked(pending.Receipt.MutationID)
	if err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if found {
		if claim.ScopeID != pending.ScopeID || claim.RequestFingerprint != pending.Receipt.RequestFingerprint {
			return app.StoreResult{Durability: app.DurableChange, Err: failure.New(failure.MutationMetadataConflict, map[string]string{"mutation_id": pending.Receipt.MutationID, "scope_id": pending.ScopeID}, nil)}
		}
		if claim.Status == "committed" {
			return app.StoreResult{Durability: app.DurableChange}
		}
		if claim.Pending == nil || !samePending(*claim.Pending, pending) {
			return app.StoreResult{Durability: app.AmbiguousChange, Err: fmt.Errorf("prepared mutation claim mismatch")}
		}
		return app.StoreResult{Durability: app.DurableChange}
	}
	prepared := mutationScopeClaim{SchemaVersion: mutationScopeStoreSchema, MutationID: pending.Receipt.MutationID, ScopeID: pending.ScopeID, RequestFingerprint: pending.Receipt.RequestFingerprint, Status: "prepared", Pending: &pending}
	return r.writer.Create(r.mutationScopeClaimPath(prepared.MutationID), prepared)
}

func (r *Repository) commitMutationScopeClaimLocked(pending mutationScopePending) app.StoreResult {
	receipt := pending.Receipt
	committed := mutationScopeClaim{SchemaVersion: mutationScopeStoreSchema, MutationID: receipt.MutationID, ScopeID: pending.ScopeID, RequestFingerprint: receipt.RequestFingerprint, Status: "committed", Receipt: &receipt}
	return r.writer.Replace(r.mutationScopeClaimPath(receipt.MutationID), committed)
}

func (r *Repository) ensureMutationScopeIdentityLocked(want core.ScopeIdentity) app.StoreResult {
	current, found, err := r.loadMutationScopeIdentityUnlocked(want.ScopeID)
	if err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if found {
		if current.ActivityID != want.ActivityID || current.WorkspaceID != want.WorkspaceID {
			return app.StoreResult{Durability: app.DurableChange, Err: failure.New(failure.MutationScopeBindingConflict, map[string]string{"scope_id": want.ScopeID}, nil)}
		}
		return app.StoreResult{Durability: app.DurableChange}
	}
	return r.writer.Create(r.mutationScopeIdentityPath(want.ScopeID), want)
}

func (r *Repository) applyMutationScopePendingLocked(pending mutationScopePending) app.StoreResult {
	if result := r.ensureMutationScopeClaimPreparedLocked(pending); result.Err != nil {
		return result
	}
	if pending.Kind == "set" {
		if result := r.ensureMutationScopeIdentityLocked(*pending.Identity); result.Err != nil {
			return result
		}
		if result := r.applySetIndexesLocked(*pending.Scope); result.Err != nil {
			return result
		}
	} else if pending.Receipt.Result == core.ResultReleased {
		identity, found, err := r.loadMutationScopeIdentityUnlocked(pending.ScopeID)
		if err != nil {
			return app.StoreResult{Durability: app.NoDurableChange, Err: err}
		}
		if found {
			if result := r.applyReleaseIndexesLocked(identity); result.Err != nil {
				return result
			}
		}
	}
	result := r.commitMutationScopeClaimLocked(pending)
	if result.Err != nil {
		return withObservationSeq(result, pending.ObservationSeq)
	}
	if proofResult := r.ensureMutationScopeObservationProofLocked(pending); proofResult.Err != nil {
		return withObservationSeq(proofResult, pending.ObservationSeq)
	}
	r.removeMutationScopePendingBestEffort()
	r.finishMutationScopeObservation(pending.ObservationSeq)
	return withObservationSeq(result, pending.ObservationSeq)
}

func (r *Repository) reconcileMutationScopePending(ctx context.Context) app.StoreResult {
	if err := ctx.Err(); err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	r.mutationScopeMu.Lock()
	defer r.mutationScopeMu.Unlock()
	return r.reconcileMutationScopePendingLocked(ctx)
}

func (r *Repository) reconcileMutationScopePendingLocked(_ context.Context) app.StoreResult {
	pending, found, err := r.loadMutationScopePendingLocked()
	if err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if !found {
		return app.StoreResult{Durability: app.DurableChange}
	}
	return r.applyMutationScopePendingLocked(pending)
}

func (r *Repository) removeMutationScopePendingBestEffort() {
	path := r.mutationScopePendingPath()
	if err := os.Remove(path); err == nil {
		_ = r.writer.syncParent("mutation_scope_pending_remove", r.root+"/mutation-scopes").Err
	}
}

func validateSetMutation(scopeValue core.Scope, identity core.ScopeIdentity, receipt core.MutationReceipt) error {
	if err := scopeValue.Validate(); err != nil {
		return err
	}
	if err := identity.Validate(); err != nil {
		return err
	}
	if receipt.Result != core.ResultSet || receipt.SetEffect != "" {
		return failure.New(failure.MutationScopeInvalid, map[string]string{"scope_id": scopeValue.ScopeID, "reason": "invalid_set_intent"}, nil)
	}
	validatedReceipt := receipt
	validatedReceipt.SetEffect = core.SetEffectCreated
	if err := validatedReceipt.Validate(); err != nil {
		return err
	}
	if receipt.ScopeID != scopeValue.ScopeID || identity.ScopeID != scopeValue.ScopeID || identity.ActivityID != scopeValue.ActivityID || identity.WorkspaceID != scopeValue.WorkspaceID || receipt.MutationID != scopeValue.RevisionID || !receipt.ExpiresAt.Equal(scopeValue.ExpiresAt) {
		return failure.New(failure.MutationScopeInvalid, map[string]string{"scope_id": scopeValue.ScopeID, "reason": "inconsistent_set"}, nil)
	}
	return nil
}

func pendingForSet(scopeValue core.Scope, identity core.ScopeIdentity, receipt core.MutationReceipt) mutationScopePending {
	return mutationScopePending{SchemaVersion: mutationScopeStoreSchema, Kind: "set", ScopeID: scopeValue.ScopeID, Scope: &scopeValue, Identity: &identity, Receipt: receipt}
}
