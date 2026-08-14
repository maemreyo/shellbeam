package daemon_test

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	telemetryapp "github.com/maemreyo/shellbeam/internal/app/telemetry"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

type recordingTelemetryWorker struct {
	mu    sync.Mutex
	calls []receipt.Receipt
	store interface {
		LoadReceipt(context.Context, operation.SessionID) (receipt.Receipt, error)
	}
	durableAtSchedule atomic.Bool
	scheduleErr       error
}

func (w *recordingTelemetryWorker) ScheduleTerminal(ctx context.Context, rec receipt.Receipt) error {
	if w.store != nil {
		stored, err := w.store.LoadReceipt(ctx, operation.SessionID(rec.SessionID))
		w.durableAtSchedule.Store(err == nil && stored.State.Terminal() && reflect.DeepEqual(stored, rec))
	}
	w.mu.Lock()
	w.calls = append(w.calls, rec)
	w.mu.Unlock()
	return w.scheduleErr
}

func (w *recordingTelemetryWorker) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.calls)
}

type telemetryNoTaxOwner struct {
	worker *recordingTelemetryWorker
	starts atomic.Int32
}

func (o *telemetryNoTaxOwner) Start(_ context.Context, _ operation.ExecutionSpec, _ app.OutputSink) (app.ProcessHandle, receipt.SpawnEvidence, error) {
	if o.worker.count() != 0 {
		o.worker.durableAtSchedule.Store(false)
	}
	o.starts.Add(1)
	return workerHandle{}, receipt.SpawnEvidence{Attempted: true, Succeeded: true}, nil
}

type telemetrySpawnFailOwner struct {
	worker *recordingTelemetryWorker
	starts atomic.Int32
}

func (o *telemetrySpawnFailOwner) Start(_ context.Context, _ operation.ExecutionSpec, _ app.OutputSink) (app.ProcessHandle, receipt.SpawnEvidence, error) {
	if o.worker.count() != 0 {
		o.worker.durableAtSchedule.Store(false)
	}
	o.starts.Add(1)
	return nil, receipt.SpawnEvidence{Attempted: true, Succeeded: false, ErrorCode: "spawn_failed"}, errors.New("spawn failed")
}

func TestTelemetryScheduledOnlyAfterDurableTerminalAndNeverBeforeSpawn(t *testing.T) {
	store := openStructuredDaemonStore(t)
	worker := &recordingTelemetryWorker{store: store}
	owner := &telemetryNoTaxOwner{worker: worker}
	svc := app.NewService(store, owner, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100, TelemetryWorker: worker})
	started, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "telemetry-normal", Command: "true", CWD: "/", YieldMS: 100})
	if err != nil {
		t.Fatal(err)
	}
	terminal := waitStructuredTerminal(t, svc, started.SessionID)
	if terminal.Outcome != session.Success || owner.starts.Load() != 1 || worker.count() != 1 || !worker.durableAtSchedule.Load() {
		t.Fatalf("terminal=%#v starts=%d calls=%d durable=%v", terminal, owner.starts.Load(), worker.count(), worker.durableAtSchedule.Load())
	}
}

func TestTelemetryScheduledAfterDurableSpawnFailure(t *testing.T) {
	store := openStructuredDaemonStore(t)
	worker := &recordingTelemetryWorker{store: store}
	owner := &telemetrySpawnFailOwner{worker: worker}
	svc := app.NewService(store, owner, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100, TelemetryWorker: worker})
	view, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "telemetry-spawn-fail", Command: "missing", CWD: "/", YieldMS: 100})
	if err != nil {
		t.Fatal(err)
	}
	if !view.State.Terminal() {
		view = waitStructuredTerminal(t, svc, view.SessionID)
	}
	if view.Outcome != session.Failure || owner.starts.Load() != 1 || worker.count() != 1 || !worker.durableAtSchedule.Load() {
		t.Fatalf("view=%#v starts=%d calls=%d durable=%v", view, owner.starts.Load(), worker.count(), worker.durableAtSchedule.Load())
	}
}

func TestTelemetryBackpressureDoesNotRewriteTerminalTruth(t *testing.T) {
	store := openStructuredDaemonStore(t)
	worker := &recordingTelemetryWorker{store: store, scheduleErr: telemetryapp.ErrWorkerQueueFull}
	owner := &telemetryNoTaxOwner{worker: worker}
	svc := app.NewService(store, owner, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100, TelemetryWorker: worker})
	started, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "telemetry-backpressure", Command: "true", CWD: "/", YieldMS: 100})
	if err != nil {
		t.Fatal(err)
	}
	terminal := waitStructuredTerminal(t, svc, started.SessionID)
	if terminal.Outcome != session.Success {
		t.Fatalf("terminal=%#v", terminal)
	}
	rec, err := store.LoadReceipt(context.Background(), operation.SessionID(terminal.SessionID))
	if err != nil || rec.Outcome != session.Success || rec.State != session.Completed || !worker.durableAtSchedule.Load() {
		t.Fatalf("receipt=%#v durable=%v err=%v", rec, worker.durableAtSchedule.Load(), err)
	}
	if worker.count() != 1 || owner.starts.Load() != 1 {
		t.Fatalf("worker calls=%d starts=%d", worker.count(), owner.starts.Load())
	}
}

func TestTerminalReplayDoesNotRescheduleTelemetry(t *testing.T) {
	store := openStructuredDaemonStore(t)
	worker := &recordingTelemetryWorker{store: store}
	owner := &telemetryNoTaxOwner{worker: worker}
	svc := app.NewService(store, owner, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100, TelemetryWorker: worker})
	request := app.StartRequest{ProtocolVersion: 2, OperationID: "telemetry-replay", Command: "true", CWD: "/", YieldMS: 100}
	first, err := svc.Start(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	waitStructuredTerminal(t, svc, first.SessionID)
	if _, err := svc.Start(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if owner.starts.Load() != 1 || worker.count() != 1 {
		t.Fatalf("replay starts=%d telemetry=%d", owner.starts.Load(), worker.count())
	}
}
