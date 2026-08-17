package daemon_test

import (
	"context"
	"sync"
	"testing"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	traceapp "github.com/maemreyo/shellbeam/internal/app/inputtrace"
	trace "github.com/maemreyo/shellbeam/internal/core/inputtrace"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

type daemonTraceWorker struct {
	mu    sync.Mutex
	calls []receipt.Receipt
	err   error
}

func (w *daemonTraceWorker) ScheduleTerminal(_ context.Context, rec receipt.Receipt) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls = append(w.calls, rec)
	return w.err
}
func (w *daemonTraceWorker) count() int { w.mu.Lock(); defer w.mu.Unlock(); return len(w.calls) }

func TestE27TraceWorkerOffReservationHasZeroSchedule(t *testing.T) {
	worker := &daemonTraceWorker{}
	owner := &daemonTraceOwner{}
	svc := app.NewService(openE27DaemonStore(t), owner, app.Options{Incarnation: "trace-worker-off", Shell: "/bin/sh", MaxQueuedInputBytes: 100, InputTraceWorker: worker})
	view, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "trace-worker-off", Command: "true", CWD: "/", YieldMS: 100})
	if err != nil {
		t.Fatal(err)
	}
	waitForTerminal(t, svc, view.SessionID)
	if worker.count() != 0 {
		t.Fatalf("schedules=%d", worker.count())
	}
}

func TestE27TraceWorkerTracedTerminalSchedulesOnceAfterDurablePublishAndReplayDoesNotDuplicate(t *testing.T) {
	worker := &daemonTraceWorker{}
	prepared := e27DaemonPrepared()
	preparer := &daemonTracePreparer{prepared: prepared}
	store := openE27DaemonStore(t)
	owner := &daemonTraceOwner{}
	svc := app.NewService(store, owner, app.Options{Incarnation: "trace-worker-on", Shell: "/bin/sh", MaxQueuedInputBytes: 100, InputTracePreparer: preparer, InputTraceWorker: worker})
	req := app.StartRequest{ProtocolVersion: 2, OperationID: "trace-worker-on", Command: "true", CWD: "/", YieldMS: 100, TraceMode: trace.ModeBestEffort}
	first, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	terminal := waitForTerminal(t, svc, first.SessionID)
	if worker.count() != 1 || terminal.Receipt == nil {
		t.Fatalf("schedules=%d terminal=%#v", worker.count(), terminal)
	}
	if durable, err := store.LoadReceipt(context.Background(), operation.SessionID(terminal.Receipt.SessionID)); err != nil || durable.State != terminal.Receipt.State {
		t.Fatalf("durable=%#v err=%v", durable, err)
	}
	if _, err := svc.Start(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if worker.count() != 1 {
		t.Fatalf("replay duplicated schedule=%d", worker.count())
	}
}

func TestE27TraceWorkerBackpressureNeverRewritesTerminalTruth(t *testing.T) {
	worker := &daemonTraceWorker{err: traceapp.ErrWorkerQueueFull}
	prepared := e27DaemonPrepared()
	svc := app.NewService(openE27DaemonStore(t), &daemonTraceOwner{}, app.Options{Incarnation: "trace-worker-full", Shell: "/bin/sh", MaxQueuedInputBytes: 100, InputTracePreparer: &daemonTracePreparer{prepared: prepared}, InputTraceWorker: worker})
	view, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "trace-worker-full", Command: "true", CWD: "/", YieldMS: 100, TraceMode: trace.ModeBestEffort})
	if err != nil {
		t.Fatal(err)
	}
	terminal := waitForTerminal(t, svc, view.SessionID)
	if terminal.Receipt == nil || terminal.Receipt.Outcome != "success" || worker.count() != 1 {
		t.Fatalf("terminal=%#v schedules=%d", terminal, worker.count())
	}
}
