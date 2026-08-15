package main

import (
	"context"
	"sync"
	"time"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	evidenceapp "github.com/maemreyo/shellbeam/internal/app/evidence"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

const (
	evidenceWorkerCount         = 2
	evidenceWorkerQueueDepth    = 64
	evidenceWorkerRecoveryLimit = 64
	evidenceWorkerMaxTime       = 5 * time.Second
)

type executionEvidenceRuntime struct {
	service *evidenceapp.Service
	worker  *evidenceapp.Worker
}

func newExecutionEvidenceRuntime(store *storeadapter.Repository) (*executionEvidenceRuntime, error) {
	service := evidenceapp.NewService(store, evidenceapp.NewObserver(evidenceapp.DefaultLimits()))
	worker, err := evidenceapp.NewWorker(service, store, evidenceapp.WorkerOptions{
		MaxWorkers:    evidenceWorkerCount,
		QueueDepth:    evidenceWorkerQueueDepth,
		MaxDuration:   evidenceWorkerMaxTime,
		RecoveryLimit: evidenceWorkerRecoveryLimit,
	})
	if err != nil {
		return nil, err
	}
	return &executionEvidenceRuntime{service: service, worker: worker}, nil
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
