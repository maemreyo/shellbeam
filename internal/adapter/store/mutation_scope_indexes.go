package store

import (
	"errors"
	"fmt"
	"sort"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/mutationscope"
)

func (r *Repository) loadMutationScopeIndex(path string, max int) (mutationScopeIndex, error) {
	var idx mutationScopeIndex
	err := readMutationScopeJSON(path, &idx)
	if errors.Is(err, ErrNotFound) {
		return mutationScopeIndex{SchemaVersion: mutationScopeStoreSchema}, nil
	}
	if err != nil {
		return idx, err
	}
	if idx.SchemaVersion != mutationScopeStoreSchema || len(idx.Scopes) > max {
		return idx, fmt.Errorf("invalid mutation scope index")
	}
	canonical, err := canonicalScopeIndex(idx.Scopes, max)
	if err != nil {
		return idx, err
	}
	for i := range canonical.Scopes {
		if canonical.Scopes[i].ScopeID != idx.Scopes[i].ScopeID {
			return idx, fmt.Errorf("non-canonical mutation scope index")
		}
	}
	return idx, nil
}

func activeScopesAt(scopes []core.Scope, now time.Time) []core.Scope {
	out := make([]core.Scope, 0, len(scopes))
	for _, s := range scopes {
		if now.Before(s.ExpiresAt) {
			out = append(out, s)
		}
	}
	return out
}

func upsertScope(scopes []core.Scope, want core.Scope) []core.Scope {
	out := make([]core.Scope, 0, len(scopes)+1)
	replaced := false
	for _, s := range scopes {
		if s.ScopeID == want.ScopeID {
			out = append(out, want)
			replaced = true
		} else {
			out = append(out, s)
		}
	}
	if !replaced {
		out = append(out, want)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ScopeID < out[j].ScopeID })
	return out
}

func removeScope(scopes []core.Scope, scopeID string) []core.Scope {
	out := make([]core.Scope, 0, len(scopes))
	for _, s := range scopes {
		if s.ScopeID != scopeID {
			out = append(out, s)
		}
	}
	return out
}

func (r *Repository) canAdmitMutationScopeSetLocked(want core.Scope) error {
	now := r.now().UTC()
	workspaceIdx, err := r.loadMutationScopeIndex(r.mutationScopeWorkspacePath(string(want.WorkspaceID)), r.limits.MaxMutationScopesPerWorkspace)
	if err != nil {
		return err
	}
	activityIdx, err := r.loadMutationScopeIndex(r.mutationScopeActivityPath(want.ActivityID), r.limits.MaxMutationScopesPerActivity)
	if err != nil {
		return err
	}
	workspaceActive := activeScopesAt(workspaceIdx.Scopes, now)
	activityActive := activeScopesAt(activityIdx.Scopes, now)
	if !containsScope(workspaceActive, want.ScopeID) && len(workspaceActive) >= r.limits.MaxMutationScopesPerWorkspace {
		return failure.New(failure.MutationScopeCapacityExceeded, map[string]string{"scope_id": want.ScopeID, "workspace_id": string(want.WorkspaceID), "reason": "workspace_limit"}, nil)
	}
	if !containsScope(activityActive, want.ScopeID) && len(activityActive) >= r.limits.MaxMutationScopesPerActivity {
		return failure.New(failure.MutationScopeCapacityExceeded, map[string]string{"scope_id": want.ScopeID, "activity_id": want.ActivityID, "reason": "activity_limit"}, nil)
	}
	return nil
}

func containsScope(scopes []core.Scope, id string) bool {
	for _, s := range scopes {
		if s.ScopeID == id {
			return true
		}
	}
	return false
}

func (r *Repository) applySetIndexesLocked(want core.Scope) app.StoreResult {
	now := r.now().UTC()
	wpath := r.mutationScopeWorkspacePath(string(want.WorkspaceID))
	widx, err := r.loadMutationScopeIndex(wpath, r.limits.MaxMutationScopesPerWorkspace)
	if err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	wscopes := upsertScope(activeScopesAt(widx.Scopes, now), want)
	canonical, err := canonicalScopeIndex(wscopes, r.limits.MaxMutationScopesPerWorkspace)
	if err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	first := r.writer.Replace(wpath, canonical)
	if first.Err != nil {
		return first
	}

	apath := r.mutationScopeActivityPath(want.ActivityID)
	aidx, err := r.loadMutationScopeIndex(apath, r.limits.MaxMutationScopesPerActivity)
	if err != nil {
		return app.StoreResult{Durability: app.AmbiguousChange, Err: err}
	}
	ascopes := upsertScope(activeScopesAt(aidx.Scopes, now), want)
	canonicalA, err := canonicalScopeIndex(ascopes, r.limits.MaxMutationScopesPerActivity)
	if err != nil {
		return app.StoreResult{Durability: app.AmbiguousChange, Err: err}
	}
	second := r.writer.Replace(apath, canonicalA)
	if second.Err != nil {
		second.Durability = app.AmbiguousChange
		return second
	}
	return second
}

func (r *Repository) applyReleaseIndexesLocked(identity core.ScopeIdentity) app.StoreResult {
	now := r.now().UTC()
	wpath := r.mutationScopeWorkspacePath(string(identity.WorkspaceID))
	widx, err := r.loadMutationScopeIndex(wpath, r.limits.MaxMutationScopesPerWorkspace)
	if err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	widx.Scopes = removeScope(activeScopesAt(widx.Scopes, now), identity.ScopeID)
	first := r.writer.Replace(wpath, widx)
	if first.Err != nil {
		return first
	}
	apath := r.mutationScopeActivityPath(identity.ActivityID)
	aidx, err := r.loadMutationScopeIndex(apath, r.limits.MaxMutationScopesPerActivity)
	if err != nil {
		return app.StoreResult{Durability: app.AmbiguousChange, Err: err}
	}
	aidx.Scopes = removeScope(activeScopesAt(aidx.Scopes, now), identity.ScopeID)
	second := r.writer.Replace(apath, aidx)
	if second.Err != nil {
		second.Durability = app.AmbiguousChange
		return second
	}
	return second
}
