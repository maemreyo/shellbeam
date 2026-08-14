package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	gojson "github.com/maemreyo/shellbeam/internal/adapter/structured/gojson"
	observationapp "github.com/maemreyo/shellbeam/internal/app/observation"
	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	activity "github.com/maemreyo/shellbeam/internal/core/activity"
	observation "github.com/maemreyo/shellbeam/internal/core/observation"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	structuredcore "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

const (
	structuredWorkerCount      = 2
	structuredWorkerQueueDepth = 64
	structuredWorkerMaxBytes   = 8 << 20
	structuredWorkerMaxRecords = 1024
	structuredWorkerMaxString  = 64 << 10
	structuredWorkerMaxDepth   = 16
	structuredWorkerMaxTime    = 5 * time.Second
)

type executionObservationRuntime struct {
	events     *observationapp.Service
	structured *structuredapp.Inspector
	worker     *structuredapp.Worker
	material   *observationapp.Materializer
}

func newExecutionObservationRuntime(ctx context.Context, store *storeadapter.Repository) (*executionObservationRuntime, error) {
	key, err := store.EventCursorKey(ctx)
	if err != nil {
		return nil, err
	}
	eventCodec, err := observationapp.NewCursorCodec(key)
	if err != nil {
		return nil, err
	}
	resultCodec, err := structuredapp.NewResultCursorCodec(key)
	if err != nil {
		return nil, err
	}
	materializer := observationapp.NewMaterializer(store)
	events := observationapp.NewService(store, materializer, daemonSnapshotProvider{store: store}, eventCodec)
	structured := structuredapp.NewInspector(store, resultCodec)
	binder := structuredapp.NewInputBinder(store)
	reader := daemonStructuredReader{store: store, binder: binder}
	worker, err := structuredapp.NewWorker(binder, store, reader, []structuredapp.Adapter{
		gojson.TestAdapter{}, gojson.VetAdapter{},
	}, structuredapp.WorkerOptions{
		MaxWorkers: structuredWorkerCount,
		QueueDepth: structuredWorkerQueueDepth,
		Limits: structuredapp.Limits{
			MaxBytes: structuredWorkerMaxBytes, MaxRecords: structuredWorkerMaxRecords,
			MaxStringBytes: structuredWorkerMaxString, MaxDepth: structuredWorkerMaxDepth,
			MaxDuration: structuredWorkerMaxTime,
		},
	})
	if err != nil {
		return nil, err
	}
	return &executionObservationRuntime{events: events, structured: structured, worker: worker, material: materializer}, nil
}

func (r *executionObservationRuntime) startMaterialization(ctx context.Context) {
	if r == nil || r.material == nil {
		return
	}
	go func() { _, _ = r.material.Materialize(ctx) }()
}

func (r *executionObservationRuntime) shutdown(ctx context.Context) error {
	if r == nil || r.worker == nil {
		return nil
	}
	return r.worker.Shutdown(ctx)
}

type structuredWorkerProxy struct {
	mu     sync.RWMutex
	worker *structuredapp.Worker
}

func (p *structuredWorkerProxy) bind(worker *structuredapp.Worker) {
	p.mu.Lock()
	p.worker = worker
	p.mu.Unlock()
}

func (p *structuredWorkerProxy) ScheduleTerminal(ctx context.Context, rec receipt.Receipt, adapter string) error {
	p.mu.RLock()
	worker := p.worker
	p.mu.RUnlock()
	if worker == nil {
		return structuredapp.ErrWorkerClosed
	}
	return worker.ScheduleTerminal(ctx, rec, adapter)
}

type daemonStructuredReader struct {
	store  *storeadapter.Repository
	binder *structuredapp.Binder
}

func (r daemonStructuredReader) ReadOutputRange(ctx context.Context, ref structuredcore.RawOutputRef, offset int64, max int) ([]byte, error) {
	return r.binder.ReadOutputRange(ctx, ref, offset, max)
}

func (r daemonStructuredReader) DescribeInput(ctx context.Context, ref structuredcore.RawOutputRef) (structuredapp.InputContext, error) {
	if r.store == nil || r.binder == nil {
		return structuredapp.InputContext{}, fmt.Errorf("structured reader unavailable")
	}
	snap, err := r.store.LoadSession(ctx, operation.SessionID(ref.SessionID))
	if err != nil {
		return structuredapp.InputContext{}, err
	}
	reservation, err := r.store.LoadOperation(ctx, operation.ID(snap.OperationID))
	if err != nil {
		return structuredapp.InputContext{}, err
	}
	return structuredapp.InputContext{OperationID: string(reservation.OperationID), RepositoryRoot: reservation.CWD}, nil
}

type daemonSnapshotProvider struct{ store *storeadapter.Repository }

func (p daemonSnapshotProvider) CaptureSnapshot(ctx context.Context, target observation.Target) (observation.Snapshot, error) {
	if p.store == nil || target.Validate() != nil {
		return observation.Snapshot{}, fmt.Errorf("snapshot unavailable")
	}
	cut, err := p.store.ObservationHighWatermark(ctx)
	if err != nil {
		return observation.Snapshot{}, err
	}
	facts, err := p.snapshotFacts(ctx, target)
	if err != nil {
		return observation.Snapshot{}, err
	}
	return observation.Snapshot{SchemaVersion: observation.SchemaVersion, Target: target, CapturedThroughSeq: cut, Facts: facts}, nil
}

func (p daemonSnapshotProvider) snapshotFacts(ctx context.Context, target observation.Target) ([]observation.SnapshotFact, error) {
	switch target.Kind {
	case observation.TargetOperation:
		return p.operationFacts(ctx, operation.ID(target.OperationID))
	case observation.TargetSession:
		return p.sessionFacts(ctx, operation.SessionID(target.SessionID))
	case observation.TargetActivity:
		record, found, err := p.store.LoadActivity(ctx, activity.ID(target.ActivityID))
		if err != nil || !found {
			return nil, firstSnapshotError(err)
		}
		return []observation.SnapshotFact{{Code: "activity_operations", Value: fmt.Sprintf("%d", len(record.Operations))}}, nil
	default:
		return nil, fmt.Errorf("bounded snapshot unavailable for target")
	}
}

func (p daemonSnapshotProvider) operationFacts(ctx context.Context, id operation.ID) ([]observation.SnapshotFact, error) {
	reservation, found, err := p.store.FindOperation(ctx, id)
	if err != nil || !found {
		return nil, firstSnapshotError(err)
	}
	facts := []observation.SnapshotFact{{Code: "operation_session", Value: string(reservation.SessionID)}}
	sessionFacts, err := p.sessionFacts(ctx, reservation.SessionID)
	if err != nil {
		return nil, err
	}
	return append(facts, sessionFacts...), nil
}

func (p daemonSnapshotProvider) sessionFacts(ctx context.Context, id operation.SessionID) ([]observation.SnapshotFact, error) {
	snap, err := p.store.LoadSession(ctx, id)
	if err != nil {
		return nil, err
	}
	facts := []observation.SnapshotFact{{Code: "session_state", Value: string(snap.State)}}
	if snap.State.Terminal() {
		rec, err := p.store.LoadReceipt(ctx, id)
		if err != nil {
			return nil, err
		}
		facts = append(facts, observation.SnapshotFact{Code: "session_outcome", Value: string(rec.Outcome)})
	}
	return facts, nil
}

func firstSnapshotError(err error) error {
	if err != nil {
		return err
	}
	return errors.New("snapshot target not found")
}

var _ structuredapp.Reader = daemonStructuredReader{}
var _ observationapp.SnapshotProvider = daemonSnapshotProvider{}
var _ interface {
	ScheduleTerminal(context.Context, receipt.Receipt, string) error
} = (*structuredWorkerProxy)(nil)
