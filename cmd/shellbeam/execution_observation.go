package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	gojson "github.com/maemreyo/shellbeam/internal/adapter/structured/gojson"
	jestjson "github.com/maemreyo/shellbeam/internal/adapter/structured/jestjson"
	pytestjunit "github.com/maemreyo/shellbeam/internal/adapter/structured/pytestjunit"
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	observationapp "github.com/maemreyo/shellbeam/internal/app/observation"
	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	activity "github.com/maemreyo/shellbeam/internal/core/activity"
	observation "github.com/maemreyo/shellbeam/internal/core/observation"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	structuredcore "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

const (
	structuredWorkerCount                   = 2
	structuredWorkerQueueDepth              = 64
	structuredWorkerMaxBytes                = 16 << 20
	structuredWorkerMaxRecords              = 8192
	structuredWorkerMaxString               = 64 << 10
	structuredWorkerMaxDepth                = 16
	structuredWorkerMaxTime                 = 5 * time.Second
	materializationErrorRetryPeriod         = time.Minute
	observationTransitionRetryInitialPeriod = time.Second
	observationTransitionRetryMaximumPeriod = time.Minute
)

type observationTransitionRetrier interface {
	RetryObservationTransitions(context.Context) (int, error)
}

type executionObservationRuntime struct {
	events                 *observationapp.Service
	structured             *structuredapp.Inspector
	worker                 *structuredapp.Worker
	capture                *executionStructuredCaptureRuntime
	material               observationapp.MaterializerPort
	wakeups                <-chan struct{}
	transitionRetries      observationTransitionRetrier
	transitionRetryWakeups <-chan struct{}
	after                  func(time.Duration) <-chan time.Time
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
	rawReader := daemonStructuredReader{store: store, binder: binder}
	reader, err := structuredapp.NewArtifactReader(rawReader, store)
	if err != nil {
		return nil, err
	}
	worker, err := structuredapp.NewWorker(binder, store, reader, []structuredapp.Adapter{
		gojson.TestAdapter{}, gojson.VetAdapter{}, pytestjunit.Adapter{}, jestjson.Adapter{},
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
	capture, err := newExecutionStructuredCaptureRuntime(store, worker)
	if err != nil {
		_ = worker.Shutdown(context.Background())
		return nil, err
	}
	if err := worker.RecoverArtifacts(ctx, store); err != nil {
		_ = worker.Shutdown(context.Background())
		return nil, err
	}
	return &executionObservationRuntime{events: events, structured: structured, worker: worker, capture: capture, material: materializer, wakeups: store.ObservationWakeups(), transitionRetries: store, transitionRetryWakeups: store.ObservationTransitionRetryWakeups(), after: time.After}, nil
}

func (r *executionObservationRuntime) startMaterialization(ctx context.Context) {
	if r == nil || r.material == nil {
		return
	}
	go r.materializationLoop(ctx)
}

func (r *executionObservationRuntime) materializationLoop(ctx context.Context) {
	transitionRetryDelay := observationTransitionRetryInitialPeriod
	for {
		pendingTransitions := 0
		if r.transitionRetries != nil {
			pendingTransitions, _ = r.transitionRetries.RetryObservationTransitions(ctx)
		}
		_, materializeErr := r.material.Materialize(ctx)
		if ctx.Err() != nil {
			return
		}

		after := r.after
		if after == nil {
			after = time.After
		}
		if pendingTransitions > 0 {
			retry := after(transitionRetryDelay)
			transitionRetryDelay = nextObservationTransitionRetryDelay(transitionRetryDelay)
			select {
			case <-ctx.Done():
				return
			case <-retry:
			}
			continue
		}

		transitionRetryDelay = observationTransitionRetryInitialPeriod
		var retry <-chan time.Time
		if materializeErr != nil {
			retry = after(materializationErrorRetryPeriod)
		}
		select {
		case <-ctx.Done():
			return
		case <-r.wakeups:
		case <-r.transitionRetryWakeups:
		case <-retry:
		}
	}
}

func nextObservationTransitionRetryDelay(current time.Duration) time.Duration {
	if current >= observationTransitionRetryMaximumPeriod/2 {
		return observationTransitionRetryMaximumPeriod
	}
	return current * 2
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

type structuredCaptureProxy struct {
	mu      sync.RWMutex
	runtime *executionStructuredCaptureRuntime
}

func (p *structuredCaptureProxy) bind(runtime *executionStructuredCaptureRuntime) {
	p.mu.Lock()
	p.runtime = runtime
	p.mu.Unlock()
}
func (p *structuredCaptureProxy) PrepareStructuredCapture(ctx context.Context, req daemonapp.StructuredCapturePrepareRequest) (daemonapp.StructuredCapturePreparation, error) {
	p.mu.RLock()
	runtime := p.runtime
	p.mu.RUnlock()
	if runtime == nil {
		return daemonapp.StructuredCapturePreparation{}, fmt.Errorf("structured capture runtime unavailable")
	}
	return runtime.PrepareStructuredCapture(ctx, req)
}
func (p *structuredCaptureProxy) AbortStructuredCapture(ctx context.Context, id operation.ID, sessionID operation.SessionID) error {
	p.mu.RLock()
	runtime := p.runtime
	p.mu.RUnlock()
	if runtime == nil {
		return nil
	}
	return runtime.AbortStructuredCapture(ctx, id, sessionID)
}
func (p *structuredCaptureProxy) AcquireTerminal(ctx context.Context, reservation operation.Reservation) structuredapp.TerminalCaptureResult {
	p.mu.RLock()
	runtime := p.runtime
	p.mu.RUnlock()
	if runtime == nil {
		return structuredapp.TerminalCaptureResult{State: structuredapp.TerminalCaptureUnavailable, CaptureAuthorityID: reservation.StructuredCaptureDigest, DiagnosticCode: "artifact_capture_unavailable"}
	}
	return runtime.AcquireTerminal(ctx, reservation)
}
func (p *structuredCaptureProxy) ScheduleTerminal(ctx context.Context, rec receipt.Receipt, result structuredapp.TerminalCaptureResult) error {
	p.mu.RLock()
	runtime := p.runtime
	p.mu.RUnlock()
	if runtime == nil {
		_ = result.Close()
		return fmt.Errorf("structured capture runtime unavailable")
	}
	return runtime.ScheduleTerminal(ctx, rec, result)
}

type daemonStructuredReader struct {
	store  *storeadapter.Repository
	binder *structuredapp.Binder
}

func (r daemonStructuredReader) ReadInputRange(ctx context.Context, ref structuredcore.StructuredInputRef, offset int64, max int) ([]byte, error) {
	return r.binder.ReadInputRange(ctx, ref, offset, max)
}

func (r daemonStructuredReader) DescribeInput(ctx context.Context, ref structuredcore.StructuredInputRef) (structuredapp.InputContext, error) {
	if r.store == nil || r.binder == nil {
		return structuredapp.InputContext{}, fmt.Errorf("structured reader unavailable")
	}
	raw, ok := ref.Raw()
	if !ok {
		return structuredapp.InputContext{}, fmt.Errorf("daemon raw structured reader requires raw output")
	}
	snap, err := r.store.LoadSession(ctx, operation.SessionID(raw.SessionID))
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
