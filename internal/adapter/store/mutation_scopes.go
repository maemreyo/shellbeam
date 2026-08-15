package store

import (
	"context"
	"fmt"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	activity "github.com/maemreyo/shellbeam/internal/core/activity"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/mutationscope"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func (r *Repository) CommitMutationScopeSet(ctx context.Context, want core.Scope, identity core.ScopeIdentity, receipt core.MutationReceipt) app.StoreResult {
	if err := ctx.Err(); err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if err := validateSetMutation(want, identity, receipt); err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	r.mutationScopeMu.Lock()
	defer r.mutationScopeMu.Unlock()
	if result := r.reconcileMutationScopePendingLocked(ctx); result.Err != nil {
		return result
	}
	if _, replayed, result := r.replayMutationScopeClaimLocked(receipt.MutationID, want.ScopeID, receipt.RequestFingerprint); result.Err != nil || replayed {
		return result
	}
	currentIdentity, found, err := r.loadMutationScopeIdentityUnlocked(want.ScopeID)
	if err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if found && (currentIdentity.ActivityID != want.ActivityID || currentIdentity.WorkspaceID != want.WorkspaceID) {
		return app.StoreResult{Durability: app.DurableChange, Err: failure.New(failure.MutationScopeBindingConflict, map[string]string{"scope_id": want.ScopeID}, nil)}
	}
	if err := r.canAdmitMutationScopeSetLocked(want); err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	_, active, err := r.loadMutationScopeUnlocked(want.ScopeID)
	if err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	receipt.SetEffect = core.SetEffectCreated
	if active {
		receipt.SetEffect = core.SetEffectReplaced
	}
	pending := pendingForSet(want, identity, receipt)
	if result := r.createMutationScopePendingLocked(pending); result.Err != nil {
		return result
	}
	return r.applyMutationScopePendingLocked(pending)
}

func (r *Repository) CommitMutationScopeRelease(ctx context.Context, scopeID string, receipt core.MutationReceipt) app.StoreResult {
	if err := ctx.Err(); err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if receipt.ScopeID != scopeID || receipt.Result != core.ResultReleased || receipt.Validate() != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: failure.New(failure.MutationScopeInvalid, map[string]string{"scope_id": scopeID, "reason": "invalid_release"}, nil)}
	}
	r.mutationScopeMu.Lock()
	defer r.mutationScopeMu.Unlock()
	if result := r.reconcileMutationScopePendingLocked(ctx); result.Err != nil {
		return result
	}
	if _, replayed, result := r.replayMutationScopeClaimLocked(receipt.MutationID, scopeID, receipt.RequestFingerprint); result.Err != nil || replayed {
		return result
	}
	_, active, err := r.loadMutationScopeUnlocked(scopeID)
	if err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	receipt.Result = core.ResultAlreadyAbsent
	if active {
		receipt.Result = core.ResultReleased
	}
	pending := mutationScopePending{SchemaVersion: mutationScopeStoreSchema, Kind: "release", ScopeID: scopeID, Receipt: receipt}
	if result := r.createMutationScopePendingLocked(pending); result.Err != nil {
		return result
	}
	return r.applyMutationScopePendingLocked(pending)
}

func (r *Repository) LoadMutationScopeIdentity(ctx context.Context, scopeID string) (core.ScopeIdentity, bool, error) {
	if err := core.ValidateScopeID(scopeID); err != nil {
		return core.ScopeIdentity{}, false, err
	}
	if err := ctx.Err(); err != nil {
		return core.ScopeIdentity{}, false, err
	}
	r.mutationScopeMu.Lock()
	defer r.mutationScopeMu.Unlock()
	if result := r.reconcileMutationScopePendingLocked(ctx); result.Err != nil {
		return core.ScopeIdentity{}, false, result.Err
	}
	return r.loadMutationScopeIdentityUnlocked(scopeID)
}

func (r *Repository) LoadMutationScope(ctx context.Context, scopeID string) (core.Scope, bool, error) {
	if err := core.ValidateScopeID(scopeID); err != nil {
		return core.Scope{}, false, err
	}
	if err := ctx.Err(); err != nil {
		return core.Scope{}, false, err
	}
	r.mutationScopeMu.Lock()
	defer r.mutationScopeMu.Unlock()
	if result := r.reconcileMutationScopePendingLocked(ctx); result.Err != nil {
		return core.Scope{}, false, result.Err
	}
	return r.loadMutationScopeUnlocked(scopeID)
}

func (r *Repository) loadMutationScopeUnlocked(scopeID string) (core.Scope, bool, error) {
	identity, found, err := r.loadMutationScopeIdentityUnlocked(scopeID)
	if err != nil || !found {
		return core.Scope{}, false, err
	}
	idx, err := r.loadMutationScopeIndex(r.mutationScopeWorkspacePath(string(identity.WorkspaceID)), r.limits.MaxMutationScopesPerWorkspace)
	if err != nil {
		return core.Scope{}, false, err
	}
	for _, s := range idx.Scopes {
		if s.ScopeID == scopeID && r.now().UTC().Before(s.ExpiresAt) {
			return s, true, nil
		}
	}
	return core.Scope{}, false, nil
}

func (r *Repository) LoadMutationReceipt(ctx context.Context, mutationID string) (core.MutationReceipt, bool, error) {
	if err := core.ValidateMutationID(mutationID); err != nil {
		return core.MutationReceipt{}, false, err
	}
	if err := ctx.Err(); err != nil {
		return core.MutationReceipt{}, false, err
	}
	r.mutationScopeMu.Lock()
	defer r.mutationScopeMu.Unlock()
	if result := r.reconcileMutationScopePendingLocked(ctx); result.Err != nil {
		return core.MutationReceipt{}, false, result.Err
	}
	claim, found, err := r.loadMutationScopeClaimUnlocked(mutationID)
	if err != nil || !found {
		return core.MutationReceipt{}, false, err
	}
	if claim.Status != "committed" || claim.Receipt == nil {
		return core.MutationReceipt{}, false, fmt.Errorf("mutation receipt not committed")
	}
	return *claim.Receipt, true, nil
}

func (r *Repository) ListMutationScopes(ctx context.Context, activityID string, workspaceID workspace.WorkspaceID) ([]core.Scope, error) {
	if _, err := workspace.ParseWorkspaceID(string(workspaceID)); err != nil {
		return nil, err
	}
	if activityID != "" {
		if _, err := activity.ParseID(activityID); err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mutationScopeMu.Lock()
	defer r.mutationScopeMu.Unlock()
	if result := r.reconcileMutationScopePendingLocked(ctx); result.Err != nil {
		return nil, result.Err
	}
	idx, err := r.loadMutationScopeIndex(r.mutationScopeWorkspacePath(string(workspaceID)), r.limits.MaxMutationScopesPerWorkspace)
	if err != nil {
		return nil, err
	}
	active := activeScopesAt(idx.Scopes, r.now().UTC())
	if activityID == "" {
		return active, nil
	}
	out := make([]core.Scope, 0, len(active))
	for _, s := range active {
		if s.ActivityID == activityID {
			out = append(out, s)
		}
	}
	return out, nil
}
