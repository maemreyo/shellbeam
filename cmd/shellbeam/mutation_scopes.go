package main

import (
	"context"

	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	mutationapp "github.com/maemreyo/shellbeam/internal/app/mutationscope"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/mutationscope"
)

type mutationScopeCoordinator interface {
	Set(context.Context, mutationapp.SetRequest) (mutationapp.MutationResult, error)
	Release(context.Context, mutationapp.ReleaseRequest) (mutationapp.MutationResult, error)
	Inspect(context.Context, mutationapp.InspectRequest) (core.InspectResult, error)
}

func mutationScopeCatalog(base capability.Catalog) capability.Catalog {
	return base.WithMutationScopes(
		core.MaxActiveScopesPerActivity,
		core.MaxActiveScopesPerWorkspace,
		core.MaxPathsPerScope,
		core.MaxSelectorBytes,
		core.MaxAdvisories,
		core.DefaultTTL.Milliseconds(),
		core.MaxTTL.Milliseconds(),
	)
}

func (a *daemonActions) SetMutationScope(ctx context.Context, request mutationapp.SetRequest) (mutationapp.MutationResult, error) {
	if a == nil || a.mutationScopes == nil {
		return mutationapp.MutationResult{}, failure.New(failure.FeatureUnavailable, nil, nil)
	}
	return a.mutationScopes.Set(ctx, request)
}

func (a *daemonActions) ReleaseMutationScope(ctx context.Context, request mutationapp.ReleaseRequest) (mutationapp.MutationResult, error) {
	if a == nil || a.mutationScopes == nil {
		return mutationapp.MutationResult{}, failure.New(failure.FeatureUnavailable, nil, nil)
	}
	return a.mutationScopes.Release(ctx, request)
}

func (a *daemonActions) InspectMutationScopes(ctx context.Context, request mutationapp.InspectRequest) (core.InspectResult, error) {
	if a == nil || a.mutationScopes == nil {
		return core.InspectResult{}, failure.New(failure.FeatureUnavailable, nil, nil)
	}
	return a.mutationScopes.Inspect(ctx, request)
}

func (a *daemonActions) InspectActivityMutationScopes(ctx context.Context, activityID string) (core.InspectResult, error) {
	if a == nil || a.mutationScopes == nil || a.activity == nil {
		return core.InspectResult{}, failure.New(failure.FeatureUnavailable, nil, nil)
	}
	activity, err := a.activity.Inspect(ctx, activityID)
	if err != nil {
		return core.InspectResult{}, err
	}
	return daemonapp.InspectActivityMutationScopes(ctx, a.mutationScopes, activity)
}
