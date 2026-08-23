package main

import (
	"context"
	"runtime"
	"time"

	dyldtrace "github.com/maemreyo/shellbeam/internal/adapter/inputtrace/dyld"
	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	traceapp "github.com/maemreyo/shellbeam/internal/app/inputtrace"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	trace "github.com/maemreyo/shellbeam/internal/core/inputtrace"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type inputTraceComposition struct {
	Preparer  traceapp.Preparer
	Worker    *traceapp.Worker
	Inspector *traceapp.Inspector
	Catalog   capability.Catalog
}

type inputTraceWorkspaceLookup interface {
	Inspect(context.Context, string) (workspacecore.Workspace, error)
}

type inputTraceWorkspaceResolver struct {
	workspaces inputTraceWorkspaceLookup
}

func (r inputTraceWorkspaceResolver) ResolveInputTraceWorkspace(ctx context.Context, workspaceID string) (string, error) {
	if r.workspaces == nil {
		return "", failure.New(failure.FeatureUnavailable, map[string]string{"feature": "input_trace_workspace"}, nil)
	}
	record, err := r.workspaces.Inspect(ctx, workspaceID)
	if err != nil {
		return "", err
	}
	return record.Root, nil
}

func composeInputTracing(ctx context.Context, enabled bool, stateDir string, repository *storeadapter.Repository, workspaces inputTraceWorkspaceLookup, catalog capability.Catalog) (inputTraceComposition, error) {
	composition := inputTraceComposition{Catalog: catalog.Clone()}
	if !enabled {
		return composition, nil
	}
	provider := dyldtrace.New(stateDir)
	health, err := provider.Health(ctx)
	if err != nil || !health.Available {
		return composition, nil
	}
	if repository == nil {
		return composition, nil
	}
	var workspaceResolver traceapp.WorkspaceResolver
	if workspaces != nil {
		workspaceResolver = inputTraceWorkspaceResolver{workspaces: workspaces}
	}
	materializer := traceapp.NewMaterializer(repository, provider, workspaceResolver)
	worker, err := traceapp.NewWorker(materializer, traceapp.WorkerOptions{MaxWorkers: 2, QueueDepth: trace.WorkerQueueDepth, MaxDuration: time.Minute})
	if err != nil {
		return composition, err
	}
	promoted := composition.Catalog.WithInputTracing(health.Provider, runtime.GOOS, false, trace.EffectEnvironmentAffecting, health.Coverage)
	if promoted.Features[capability.FeatureInputTracing] != capability.Available {
		_ = worker.Shutdown(context.Background())
		return composition, nil
	}
	composition.Preparer = provider
	composition.Worker = worker
	composition.Inspector = traceapp.NewInspector(repository)
	composition.Catalog = promoted
	return composition, nil
}

func (c inputTraceComposition) Close(ctx context.Context) error {
	if c.Worker == nil {
		return nil
	}
	return c.Worker.Shutdown(ctx)
}

func (a daemonActions) InspectInputTrace(ctx context.Context, request traceapp.InspectRequest) (traceapp.InspectResult, error) {
	if a.inputTrace == nil {
		return traceapp.InspectResult{}, failure.New(failure.FeatureUnavailable, map[string]string{"feature": string(capability.FeatureInputTracing)}, nil)
	}
	return a.inputTrace.Inspect(ctx, request)
}
