package daemon

import (
	"context"
	"errors"
	"sort"

	mutationapp "github.com/maemreyo/shellbeam/internal/app/mutationscope"
	activitycore "github.com/maemreyo/shellbeam/internal/core/activity"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/mutationscope"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type MutationScopeStore interface {
	ListWorkspaces(context.Context) ([]workspace.Workspace, error)
	LoadMutationScope(context.Context, string) (core.Scope, bool, error)
	ListMutationScopes(context.Context, string, workspace.WorkspaceID) ([]core.Scope, error)
	LoadMutationReceipt(context.Context, string) (core.MutationReceipt, bool, error)
	CommitMutationScopeSet(context.Context, core.Scope, core.ScopeIdentity, core.MutationReceipt) StoreResult
	CommitMutationScopeRelease(context.Context, string, core.MutationReceipt) StoreResult
}

type mutationScopeStoreAdapter struct{ store MutationScopeStore }

func NewMutationScopeService(store MutationScopeStore, clock mutationapp.Clock) *mutationapp.Service {
	if store == nil {
		return nil
	}
	return mutationapp.New(mutationScopeStoreAdapter{store: store}, clock)
}

func (a mutationScopeStoreAdapter) ListWorkspaces(ctx context.Context) ([]workspace.Workspace, error) {
	return a.store.ListWorkspaces(ctx)
}
func (a mutationScopeStoreAdapter) LoadMutationScope(ctx context.Context, id string) (core.Scope, bool, error) {
	return a.store.LoadMutationScope(ctx, id)
}
func (a mutationScopeStoreAdapter) ListMutationScopes(ctx context.Context, activityID string, workspaceID workspace.WorkspaceID) ([]core.Scope, error) {
	return a.store.ListMutationScopes(ctx, activityID, workspaceID)
}
func (a mutationScopeStoreAdapter) LoadMutationReceipt(ctx context.Context, id string) (core.MutationReceipt, bool, error) {
	return a.store.LoadMutationReceipt(ctx, id)
}
func (a mutationScopeStoreAdapter) CommitMutationScopeSet(ctx context.Context, scope core.Scope, identity core.ScopeIdentity, receipt core.MutationReceipt) error {
	return mutationScopeStoreError(a.store.CommitMutationScopeSet(ctx, scope, identity, receipt))
}
func (a mutationScopeStoreAdapter) CommitMutationScopeRelease(ctx context.Context, scopeID string, receipt core.MutationReceipt) error {
	return mutationScopeStoreError(a.store.CommitMutationScopeRelease(ctx, scopeID, receipt))
}

func mutationScopeStoreError(result StoreResult) error {
	if result.Err == nil {
		return nil
	}
	var typed *failure.Failure
	if errors.As(result.Err, &typed) {
		return result.Err
	}
	if result.Durability == AmbiguousChange {
		return failure.New(failure.PersistenceAmbiguous, nil, result.Err)
	}
	return failure.New(failure.PersistenceUnavailable, nil, result.Err)
}

type MutationScopeInspector interface {
	Inspect(context.Context, mutationapp.InspectRequest) (core.InspectResult, error)
}

func InspectActivityMutationScopes(ctx context.Context, inspector MutationScopeInspector, activity activitycore.Activity) (core.InspectResult, error) {
	result := core.InspectResult{ActiveScopeLimit: core.MaxActiveScopesPerActivity, AdvisoryLimit: core.MaxAdvisories}
	if inspector == nil {
		return result, failure.New(failure.FeatureUnavailable, nil, nil)
	}
	workspaceIDs := uniqueSortedWorkspaceIDs(activity.WorkspaceIDs)
	for _, workspaceID := range workspaceIDs {
		current, err := inspector.Inspect(ctx, mutationapp.InspectRequest{WorkspaceID: workspaceID, ActivityID: string(activity.ID)})
		if err != nil {
			return core.InspectResult{}, err
		}
		result.ActiveCount += max(current.ActiveCount, len(current.ActiveScopes))
		result.AdvisoryCount += max(current.AdvisoryCount, len(current.Advisories))
		result.ScopesTruncated = result.ScopesTruncated || current.ScopesTruncated
		result.AdvisoriesTruncated = result.AdvisoriesTruncated || current.AdvisoriesTruncated
		result.ActiveScopes = append(result.ActiveScopes, current.ActiveScopes...)
		result.Advisories = append(result.Advisories, current.Advisories...)
	}
	sortActivityMutationScopes(&result)
	if len(result.ActiveScopes) > result.ActiveScopeLimit {
		result.ActiveScopes = result.ActiveScopes[:result.ActiveScopeLimit]
		result.ScopesTruncated = true
	}
	if len(result.Advisories) > result.AdvisoryLimit {
		result.Advisories = result.Advisories[:result.AdvisoryLimit]
		result.AdvisoriesTruncated = true
	}
	if result.ActiveCount > result.ActiveScopeLimit {
		result.ScopesTruncated = true
	}
	if result.AdvisoryCount > result.AdvisoryLimit {
		result.AdvisoriesTruncated = true
	}
	return result, nil
}

func uniqueSortedWorkspaceIDs(values []workspace.WorkspaceID) []workspace.WorkspaceID {
	seen := make(map[workspace.WorkspaceID]struct{}, len(values))
	out := make([]workspace.WorkspaceID, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func sortActivityMutationScopes(result *core.InspectResult) {
	sort.Slice(result.ActiveScopes, func(i, j int) bool {
		left, right := result.ActiveScopes[i], result.ActiveScopes[j]
		if left.WorkspaceID != right.WorkspaceID {
			return left.WorkspaceID < right.WorkspaceID
		}
		return left.ScopeID < right.ScopeID
	})
	sort.Slice(result.Advisories, func(i, j int) bool {
		left, right := result.Advisories[i], result.Advisories[j]
		if left.WorkspaceID != right.WorkspaceID {
			return left.WorkspaceID < right.WorkspaceID
		}
		if left.ScopeIDs[0] != right.ScopeIDs[0] {
			return left.ScopeIDs[0] < right.ScopeIDs[0]
		}
		return left.ScopeIDs[1] < right.ScopeIDs[1]
	})
}
