package daemon_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

type structuredSchedule struct {
	receipt receipt.Receipt
	adapter string
}
type recordingStructuredWorker struct {
	mu                sync.Mutex
	calls             []structuredSchedule
	atSpawn           atomic.Int32
	store             *storeadapter.Repository
	durableAtSchedule atomic.Bool
	scheduleErr       error
}

func (w *recordingStructuredWorker) ScheduleTerminal(ctx context.Context, rec receipt.Receipt, adapter string) error {
	if w.store != nil {
		stored, err := w.store.LoadReceipt(ctx, operation.SessionID(rec.SessionID))
		w.durableAtSchedule.Store(err == nil && stored.State.Terminal() && stored.SessionID == rec.SessionID)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls = append(w.calls, structuredSchedule{rec, adapter})
	return w.scheduleErr
}
func (w *recordingStructuredWorker) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.calls)
}

type noTaxOwner struct {
	worker *recordingStructuredWorker
	starts atomic.Int32
}

func (o *noTaxOwner) Start(_ context.Context, _ operation.ExecutionSpec, _ app.OutputSink) (app.ProcessHandle, receipt.SpawnEvidence, error) {
	o.worker.atSpawn.Store(int32(o.worker.count()))
	o.starts.Add(1)
	return workerHandle{}, receipt.SpawnEvidence{Attempted: true, Succeeded: true}, nil
}

type workerHandle struct{}

func (workerHandle) Write([]byte) error { return nil }
func (workerHandle) CloseStdin() error  { return nil }
func (workerHandle) Signal(string) receipt.SignalEvidence {
	return receipt.SignalEvidence{Attempted: true, Succeeded: true}
}
func (workerHandle) Wait(context.Context) receipt.ExitEvidence {
	code := 0
	return receipt.ExitEvidence{Reaped: true, Code: &code}
}
func (workerHandle) Close() error { return nil }

func TestStructuredAdapterRetryMetadataConflictNeverRespawns(t *testing.T) {
	store := openStructuredDaemonStore(t)
	worker := &recordingStructuredWorker{store: store}
	owner := &noTaxOwner{worker: worker}
	svc := app.NewService(store, owner, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100, StructuredWorker: worker})
	first := app.StartRequest{ProtocolVersion: 2, OperationID: "structured-retry", Argv: []string{"go", "test", "-json", "./..."}, CWD: "/", StructuredAdapter: "go-test-json", YieldMS: 100}
	started, err := svc.Start(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	waitStructuredTerminal(t, svc, started.SessionID)
	replay := first
	replay.YieldMS = 999
	replay.MaxOutputBytes = 1
	if _, err := svc.Start(context.Background(), replay); err != nil {
		t.Fatalf("response-controls replay: %v", err)
	}
	changed := first
	changed.StructuredAdapter = "go-vet-json"
	if _, err := svc.Start(context.Background(), changed); !errors.Is(err, failure.OperationMetadataConflict) {
		t.Fatalf("adapter conflict=%v", err)
	}
	if owner.starts.Load() != 1 {
		t.Fatalf("starts=%d", owner.starts.Load())
	}
	stored, err := store.LoadOperation(context.Background(), "structured-retry")
	if err != nil {
		t.Fatal(err)
	}
	if stored.StructuredAdapter != "go-test-json" {
		t.Fatalf("stored adapter=%q", stored.StructuredAdapter)
	}
}

func TestStructuredDirectArgvSelectionIsDeterministicAndNoTaxBeforeSpawn(t *testing.T) {
	store := openStructuredDaemonStore(t)
	worker := &recordingStructuredWorker{store: store}
	owner := &noTaxOwner{worker: worker}
	svc := app.NewService(store, owner, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100, StructuredWorker: worker})
	started, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "structured-auto", Argv: []string{"go", "test", "-json", "./..."}, CWD: "/", YieldMS: 100})
	if err != nil {
		t.Fatal(err)
	}
	terminal := waitStructuredTerminal(t, svc, started.SessionID)
	if owner.worker.atSpawn.Load() != 0 {
		t.Fatalf("worker ran before child spawn: %d", owner.worker.atSpawn.Load())
	}
	if worker.count() != 1 {
		t.Fatalf("worker calls=%d", worker.count())
	}
	if !worker.durableAtSchedule.Load() {
		t.Fatal("structured worker scheduled before terminal receipt was durable")
	}
	worker.mu.Lock()
	call := worker.calls[0]
	worker.mu.Unlock()
	if call.adapter != "go-test-json" || call.receipt.SessionID != terminal.SessionID {
		t.Fatalf("call=%#v", call)
	}
	stored, _ := store.LoadOperation(context.Background(), "structured-auto")
	if stored.StructuredAdapter != "go-test-json" {
		t.Fatalf("auto stored=%q", stored.StructuredAdapter)
	}
}

func TestOrdinaryStartHasNoStructuredWorkerTax(t *testing.T) {
	store := openStructuredDaemonStore(t)
	worker := &recordingStructuredWorker{store: store}
	owner := &noTaxOwner{worker: worker}
	svc := app.NewService(store, owner, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100, StructuredWorker: worker})
	started, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "ordinary-no-tax", Command: "true", CWD: "/", YieldMS: 100})
	if err != nil {
		t.Fatal(err)
	}
	waitStructuredTerminal(t, svc, started.SessionID)
	if worker.count() != 0 || worker.atSpawn.Load() != 0 {
		t.Fatalf("worker activity count=%d atSpawn=%d", worker.count(), worker.atSpawn.Load())
	}
}

func openStructuredDaemonStore(t *testing.T) *storeadapter.Repository {
	t.Helper()
	r, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 8, MaxSessionOutput: 1 << 20, MaxTotalState: 16 << 20, ControlReserve: 1024})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func waitStructuredTerminal(t *testing.T, svc *app.Service, sid string) app.View {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for {
		v, err := svc.Poll(ctx, app.PollRequest{SessionID: sid, YieldMS: 20})
		if err != nil {
			t.Fatal(err)
		}
		if v.State.Terminal() {
			return v
		}
	}
}

func TestStructuredWorkerBackpressureDoesNotChangeTerminalTruth(t *testing.T) {
	store := openStructuredDaemonStore(t)
	worker := &recordingStructuredWorker{store: store, scheduleErr: structuredapp.ErrWorkerQueueFull}
	owner := &noTaxOwner{worker: worker}
	svc := app.NewService(store, owner, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100, StructuredWorker: worker})
	started, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "structured-backpressure", Command: "true", CWD: "/", StructuredAdapter: "go-test-json", YieldMS: 100})
	if err != nil {
		t.Fatal(err)
	}
	terminal := waitStructuredTerminal(t, svc, started.SessionID)
	if terminal.Outcome != session.Success {
		t.Fatalf("terminal=%#v", terminal)
	}
	rec, err := store.LoadReceipt(context.Background(), operation.SessionID(terminal.SessionID))
	if err != nil || rec.Outcome != session.Success || rec.State != session.Completed {
		t.Fatalf("receipt=%#v err=%v", rec, err)
	}
	if owner.starts.Load() != 1 || worker.count() != 1 || !worker.durableAtSchedule.Load() {
		t.Fatalf("starts=%d worker=%d durable=%v", owner.starts.Load(), worker.count(), worker.durableAtSchedule.Load())
	}
}

func TestUnsupportedStructuredAdapterWarnsAndExecutesWithoutParser(t *testing.T) {
	store := openStructuredDaemonStore(t)
	worker := &recordingStructuredWorker{store: store}
	owner := &noTaxOwner{worker: worker}
	svc := app.NewService(store, owner, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100, StructuredWorker: worker})
	request := app.StartRequest{ProtocolVersion: 2, OperationID: "structured-unsupported", Command: "true", CWD: "/", StructuredAdapter: "junit", YieldMS: 100}
	view, err := svc.Start(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	view = waitStructuredTerminal(t, svc, view.SessionID)
	// Poll does not carry start-only advisory, so replay the same start to observe availability metadata.
	replay, err := svc.Start(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if owner.starts.Load() != 1 || worker.count() != 0 {
		t.Fatalf("starts=%d worker=%d", owner.starts.Load(), worker.count())
	}
	if len(replay.Advisories) != 1 || replay.Advisories[0].Code != "structured_adapter_unsupported" || replay.Advisories[0].CauseFingerprint == "" {
		t.Fatalf("advisories=%#v", replay.Advisories)
	}
	stored, err := store.LoadOperation(context.Background(), "structured-unsupported")
	if err != nil || stored.StructuredAdapter != "junit" {
		t.Fatalf("stored=%#v err=%v", stored, err)
	}
	changed := request
	changed.StructuredAdapter = "go-test-json"
	if _, err := svc.Start(context.Background(), changed); !errors.Is(err, failure.OperationMetadataConflict) {
		t.Fatalf("changed adapter=%v", err)
	}
}
