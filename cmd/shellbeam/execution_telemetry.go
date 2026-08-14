package main

import (
	"context"
	"sync"
	"time"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	telemetryapp "github.com/maemreyo/shellbeam/internal/app/telemetry"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

const (
	telemetryMaxSamples                 = 2048
	telemetryMetadataBytes        int64 = 16 << 20
	telemetryMaxKeys                    = 512
	telemetryMaxKeysPerRepository       = 128
	telemetryMaxSamplesPerKey           = 64
	telemetryWorkerCount                = 2
	telemetryWorkerQueueDepth           = 64
)

const (
	telemetryRetentionAge  = 30 * 24 * time.Hour
	telemetryWorkerMaxTime = 5 * time.Second
)

type executionTelemetryRuntime struct {
	service *telemetryapp.Service
	worker  *telemetryapp.Worker
}

func newExecutionTelemetryRuntime(store *storeadapter.Repository) (*executionTelemetryRuntime, error) {
	service, err := telemetryapp.New(store)
	if err != nil {
		return nil, err
	}
	worker, err := telemetryapp.NewWorkerWithService(service, telemetryapp.WorkerOptions{
		MaxWorkers: telemetryWorkerCount, QueueDepth: telemetryWorkerQueueDepth, MaxDuration: telemetryWorkerMaxTime,
	})
	if err != nil {
		return nil, err
	}
	return &executionTelemetryRuntime{service: service, worker: worker}, nil
}

func (r *executionTelemetryRuntime) shutdown(ctx context.Context) error {
	if r == nil || r.worker == nil {
		return nil
	}
	return r.worker.Shutdown(ctx)
}

type telemetryWorkerProxy struct {
	mu     sync.RWMutex
	worker *telemetryapp.Worker
}

func (p *telemetryWorkerProxy) bind(worker *telemetryapp.Worker) {
	p.mu.Lock()
	p.worker = worker
	p.mu.Unlock()
}

func (p *telemetryWorkerProxy) ScheduleTerminal(ctx context.Context, rec receipt.Receipt) error {
	p.mu.RLock()
	worker := p.worker
	p.mu.RUnlock()
	if worker == nil {
		return telemetryapp.ErrWorkerClosed
	}
	return worker.ScheduleTerminal(ctx, rec)
}
