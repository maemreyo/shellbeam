package main

import (
	"context"
	"sync"
	"time"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	evidenceapp "github.com/maemreyo/shellbeam/internal/app/evidence"
	coreevidence "github.com/maemreyo/shellbeam/internal/core/evidence"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
)

const (
	evidenceWorkerCount         = 2
	evidenceWorkerQueueDepth    = 64
	evidenceWorkerRecoveryLimit = 64
	evidenceWorkerMaxTime       = 5 * time.Second
)

type executionEvidenceRuntime struct {
	service   *evidenceapp.Service
	worker    *evidenceapp.Worker
	inspector *evidenceapp.Inspector
}

type evidenceWorkspaceRegistry interface {
	ListWorkspaces(context.Context) ([]workspacecore.Workspace, error)
}

type evidenceFreshWorkspaceObserver interface {
	ObserveFresh(context.Context, string) workspacecore.FastSnapshot
}

type evidenceCurrentStateProvider struct {
	registry evidenceWorkspaceRegistry
	observer evidenceFreshWorkspaceObserver
}

func newEvidenceCurrentStateProvider(registry evidenceWorkspaceRegistry, observer evidenceFreshWorkspaceObserver) *evidenceCurrentStateProvider {
	return &evidenceCurrentStateProvider{registry: registry, observer: observer}
}

func (p *evidenceCurrentStateProvider) ObserveCurrent(ctx context.Context, record coreevidence.Record) evidenceapp.CurrentState {
	unknown := coreevidence.CurrentSource{Quality: coreevidence.SourceQualityUnknown}
	if p == nil || p.registry == nil || record.WorkspaceID == "" {
		return evidenceapp.CurrentState{Source: unknown}
	}
	workspaces, err := p.registry.ListWorkspaces(ctx)
	if err != nil {
		return evidenceapp.CurrentState{Source: unknown}
	}
	for _, workspace := range workspaces {
		if string(workspace.ID) != record.WorkspaceID || workspace.Validate() != nil {
			continue
		}
		state := evidenceapp.CurrentState{Source: unknown, WorkspaceRoot: workspace.Root}
		if p.observer == nil {
			return state
		}
		snapshot := p.observer.ObserveFresh(ctx, workspace.Root)
		if snapshot.WorkspaceID == workspace.ID && snapshot.RepositoryID == workspace.RepositoryID && snapshot.Quality == workspacecore.QualityFresh && len(snapshot.Generation) == 68 && snapshot.Generation[:4] == "gen_" {
			state.Source = coreevidence.CurrentSource{WorkspaceID: string(workspace.ID), Generation: snapshot.Generation, Quality: coreevidence.SourceQualityFast}
		}
		return state
	}
	return evidenceapp.CurrentState{Source: unknown}
}

func newExecutionEvidenceRuntime(store *storeadapter.Repository, observer evidenceFreshWorkspaceObserver) (*executionEvidenceRuntime, error) {
	artifactObserver := evidenceapp.NewObserver(evidenceapp.DefaultLimits())
	service := evidenceapp.NewService(store, artifactObserver)
	worker, err := evidenceapp.NewWorker(service, store, evidenceapp.WorkerOptions{
		MaxWorkers:    evidenceWorkerCount,
		QueueDepth:    evidenceWorkerQueueDepth,
		MaxDuration:   evidenceWorkerMaxTime,
		RecoveryLimit: evidenceWorkerRecoveryLimit,
	})
	if err != nil {
		return nil, err
	}
	key, err := store.EventCursorKey(context.Background())
	if err != nil {
		_ = worker.Shutdown(context.Background())
		return nil, err
	}
	codec, err := evidenceapp.NewCursorCodec(key)
	if err != nil {
		_ = worker.Shutdown(context.Background())
		return nil, err
	}
	current := newEvidenceCurrentStateProvider(store, observer)
	inspector := evidenceapp.NewInspector(store, current, artifactObserver, codec)
	return &executionEvidenceRuntime{service: service, worker: worker, inspector: inspector}, nil
}

func (r *executionEvidenceRuntime) startRecovery(ctx context.Context) {
	if r == nil || r.worker == nil {
		return
	}
	go func() { _ = r.worker.Recover(ctx) }()
}

func (r *executionEvidenceRuntime) shutdown(ctx context.Context) error {
	if r == nil || r.worker == nil {
		return nil
	}
	return r.worker.Shutdown(ctx)
}

type evidenceWorkerProxy struct {
	mu     sync.RWMutex
	worker *evidenceapp.Worker
}

func (p *evidenceWorkerProxy) bind(worker *evidenceapp.Worker) {
	p.mu.Lock()
	p.worker = worker
	p.mu.Unlock()
}

func (p *evidenceWorkerProxy) ScheduleTerminal(ctx context.Context, rec receipt.Receipt) error {
	p.mu.RLock()
	worker := p.worker
	p.mu.RUnlock()
	if worker == nil {
		return context.Canceled
	}
	return worker.ScheduleTerminal(ctx, rec)
}

var _ interface {
	ScheduleTerminal(context.Context, receipt.Receipt) error
} = (*evidenceWorkerProxy)(nil)
