package main

import (
	"context"
	"fmt"
	"time"

	goplsadapter "github.com/maemreyo/shellbeam/internal/adapter/codeintel/gopls"
	sourcefs "github.com/maemreyo/shellbeam/internal/adapter/codeintel/sourcefs"
	appcodeintel "github.com/maemreyo/shellbeam/internal/app/codeintel"
	corecodeintel "github.com/maemreyo/shellbeam/internal/core/codeintel"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type codeProviderFactory interface {
	appcodeintel.ProviderFactory
}

type codeProviderResolver interface {
	appcodeintel.ProviderOptionsResolver
}

type daemonCodeInspector interface {
	Inspect(context.Context, appcodeintel.InspectRequest) (corecodeintel.Result, error)
}

type codeIntelligenceRuntime struct {
	Service   *appcodeintel.Service
	providers *appcodeintel.ProviderManager
	available bool
}

type codeProviderReadiness interface {
	Available() bool
}

func (r *codeIntelligenceRuntime) Available() bool {
	return r != nil && r.available
}

func composeCodeIntelligenceRuntime(
	workspaces appcodeintel.WorkspaceLookup,
	sampler appcodeintel.WorkspaceSampler,
	activities appcodeintel.ActivitySelector,
	coherence appcodeintel.CoherenceSource,
	factory codeProviderFactory,
	resolver codeProviderResolver,
) (*codeIntelligenceRuntime, error) {
	if factory == nil && resolver == nil {
		return newCodeIntelligenceRuntime(workspaces, sampler, activities, coherence)
	}
	return newCodeIntelligenceRuntimeWithProvider(workspaces, sampler, activities, coherence, factory, resolver)
}

func newCodeIntelligenceRuntime(
	workspaces appcodeintel.WorkspaceLookup,
	sampler appcodeintel.WorkspaceSampler,
	activities appcodeintel.ActivitySelector,
	coherence appcodeintel.CoherenceSource,
) (*codeIntelligenceRuntime, error) {
	factory, err := goplsadapter.NewFactory(goplsadapter.DefaultConfig())
	if err != nil {
		return nil, err
	}
	return newCodeIntelligenceRuntimeWithProvider(workspaces, sampler, activities, coherence, factory, factory)
}

func newCodeIntelligenceRuntimeWithProvider(
	workspaces appcodeintel.WorkspaceLookup,
	sampler appcodeintel.WorkspaceSampler,
	activities appcodeintel.ActivitySelector,
	coherence appcodeintel.CoherenceSource,
	factory codeProviderFactory,
	resolver codeProviderResolver,
) (*codeIntelligenceRuntime, error) {
	retention, err := appcodeintel.NewSourceStore(appcodeintel.SourceStoreConfig{
		MaxEntries: 512, MaxRetainedBytes: 16 << 20, TTL: 10 * time.Minute, MaxTombstones: 1024,
	})
	if err != nil {
		return nil, err
	}
	binder, err := sourcefs.NewBinder(retention, 8<<20)
	if err != nil {
		return nil, err
	}
	providers, err := appcodeintel.NewProviderManager(factory, resolver, appcodeintel.ProviderManagerLimits{
		MaxInstances: 4, MaxInFlight: 8, MaxInFlightPerProvider: 2,
		MaxQueueDepth: 32, QueueWait: 250 * time.Millisecond, IdleTTL: 5 * time.Minute,
		FailuresBeforeCooldown: 3, FailureWindow: time.Minute, Cooldown: 30 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	service, err := appcodeintel.NewService(workspaces, sampler, activities, binder, providers, coherence, appcodeintel.ServiceLimits{
		Delta: workspacecore.DeltaLimits{},
		Result: corecodeintel.ResultLimits{
			MaxRecords: 128, MaxResponseBytes: 1 << 20, MaxTextBytes: 64 << 10, MaxRelatedLocations: 32,
		},
		MaxSelectedSources: 128, MaxSelectedSourceBytes: 8 << 20, MaxDuration: 5 * time.Second,
	})
	if err != nil {
		_ = providers.Close()
		return nil, err
	}
	available := true
	if readiness, ok := factory.(codeProviderReadiness); ok {
		available = readiness.Available()
	}
	return &codeIntelligenceRuntime{Service: service, providers: providers, available: available}, nil
}

func (r *codeIntelligenceRuntime) Close() error {
	if r == nil || r.providers == nil {
		return nil
	}
	return r.providers.Close()
}

func (a *daemonActions) InspectCode(ctx context.Context, workspaceID, activityID string, query corecodeintel.Query) (corecodeintel.Result, error) {
	if a.code == nil {
		return corecodeintel.Result{}, fmt.Errorf("code intelligence unavailable")
	}
	result, err := a.code.Inspect(ctx, appcodeintel.InspectRequest{WorkspaceID: workspaceID, ActivityID: activityID, Query: query})
	if err != nil && appcodeintel.ErrorCode(err) == appcodeintel.CodeProviderUnavailable {
		return corecodeintel.Result{Status: corecodeintel.StatusUnavailable, Query: query}, nil
	}
	return result, err
}
