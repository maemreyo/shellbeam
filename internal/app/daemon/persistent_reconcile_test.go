package daemon_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	persistentapp "github.com/maemreyo/shellbeam/internal/app/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	persistentcore "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func TestPersistentReconciliationCanonicalizesOutputPublishesTerminalAndCleansUp(t *testing.T) {
	store := openPersistentLaunchStore(t)
	handle := newPersistentReconcileHandle([]byte("hello"))
	runtime := &persistentReconcileRuntime{store: store, handle: handle}
	worker := &recordingTelemetryWorker{store: store}
	svc := app.NewService(store, &fakeOwner{}, app.Options{
		Incarnation: "persistent-reconcile", Shell: "/bin/sh", MaxQueuedInputBytes: 100,
		PersistentRuntime: runtime, TelemetryWorker: worker,
	})
	started, err := svc.Start(context.Background(), app.StartRequest{
		ProtocolVersion: 2, OperationID: "persistent-reconcile-op", Command: "printf hello", CWD: "/",
		Persistent: true, SessionName: "persistent-reconcile", YieldMS: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var terminal app.View
	for {
		terminal, err = svc.Poll(ctx, app.PollRequest{SessionID: started.SessionID, YieldMS: 20})
		if err != nil {
			t.Fatal(err)
		}
		if terminal.State.Terminal() {
			break
		}
	}
	if terminal.State != session.Completed || terminal.Outcome != session.Success {
		t.Fatalf("terminal=%#v", terminal)
	}
	output, next, err := store.ReadOutput(context.Background(), operation.SessionID(started.SessionID), 0, 32)
	if err != nil || string(output) != "hello" || next != 5 {
		t.Fatalf("canonical output=%q next=%d err=%v", output, next, err)
	}
	rec, err := store.LoadReceipt(context.Background(), operation.SessionID(started.SessionID))
	if err != nil || rec.State != session.Completed || rec.Outcome != session.Success || rec.OutputBytes != 5 || !rec.OutputComplete {
		t.Fatalf("receipt=%#v err=%v", rec, err)
	}
	binding, err := store.LoadPersistentBinding(context.Background(), operation.SessionID(started.SessionID))
	if err != nil || binding.Lifecycle != persistentcore.LifecycleTerminal {
		t.Fatalf("binding=%#v err=%v", binding, err)
	}
	if handle.Ack() != 5 || handle.cleanups.Load() != 1 {
		t.Fatalf("ack=%d cleanups=%d", handle.Ack(), handle.cleanups.Load())
	}
	if worker.count() != 1 || !worker.durableAtSchedule.Load() {
		t.Fatalf("telemetry calls=%d durable=%v", worker.count(), worker.durableAtSchedule.Load())
	}
}

func TestPersistentReconciliationRetriesTransientControlFailure(t *testing.T) {
	store := openPersistentLaunchStore(t)
	handle := newPersistentReconcileHandle([]byte("retry"))
	handle.mu.Lock()
	handle.statusFailures = 1
	handle.terminalFailures = 1
	handle.mu.Unlock()
	runtime := &persistentReconcileRuntime{store: store, handle: handle}
	svc := app.NewService(store, &fakeOwner{}, app.Options{
		Incarnation: "persistent-retry", Shell: "/bin/sh", MaxQueuedInputBytes: 100, PersistentRuntime: runtime,
	})
	started, err := svc.Start(context.Background(), app.StartRequest{
		ProtocolVersion: 2, OperationID: "persistent-reconcile-retry", Command: "printf retry", CWD: "/",
		Persistent: true, SessionName: "persistent-reconcile-retry", YieldMS: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for {
		view, pollErr := svc.Poll(ctx, app.PollRequest{SessionID: started.SessionID, YieldMS: 20})
		if pollErr != nil {
			t.Fatalf("reconciliation did not survive transient failure: %v", pollErr)
		}
		if view.State.Terminal() {
			if view.State != session.Completed || view.Outcome != session.Success {
				t.Fatalf("terminal=%#v", view)
			}
			break
		}
	}
	binding, err := store.LoadPersistentBinding(context.Background(), operation.SessionID(started.SessionID))
	if err != nil || binding.Lifecycle != persistentcore.LifecycleTerminal {
		t.Fatalf("binding=%#v err=%v", binding, err)
	}
}

type persistentReconcileRuntime struct {
	store         *storeadapter.Repository
	handle        *persistentReconcileHandle
	seedCanonical []byte
}

func (r *persistentReconcileRuntime) Ensure(ctx context.Context, reservation operation.Reservation, _ operation.ExecutionSpec) (app.PersistentLaunch, error) {
	now := reservation.CreatedAt.UTC()
	binding := persistentcore.Binding{
		SchemaVersion: persistentcore.SchemaVersion, SessionID: string(reservation.SessionID), OperationID: string(reservation.OperationID),
		ActivityID: reservation.ActivityID, WorkspaceID: reservation.WorkspaceID, SessionName: reservation.SessionName,
		Persistent: true, Supervision: persistentcore.SupervisionPerSession, Continuity: persistentcore.ContinuityDaemonRestart,
		SupervisorGenerationID: "generation-reconcile", SupervisorEndpointRef: "endpoint-reconcile",
		Lifecycle: persistentcore.LifecycleProvisioning, CreatedAt: now, UpdatedAt: now,
	}
	stored, _, result := r.store.ReservePersistentBinding(ctx, binding)
	if result.Err != nil {
		return app.PersistentLaunch{}, result.Err
	}
	stored.Lifecycle = persistentcore.LifecycleLive
	stored.UpdatedAt = now.Add(time.Nanosecond)
	if result := r.store.AdvancePersistentBinding(ctx, stored); result.Err != nil {
		return app.PersistentLaunch{}, result.Err
	}
	r.handle.mu.Lock()
	r.handle.sessionID = string(reservation.SessionID)
	r.handle.mu.Unlock()
	if len(r.seedCanonical) > 0 {
		if _, outputResult := r.store.ReconcilePersistentOutput(ctx, reservation.SessionID, 0, r.seedCanonical); outputResult.Err != nil {
			return app.PersistentLaunch{}, outputResult.Err
		}
	}
	return app.PersistentLaunch{Handle: r.handle, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}, PID: r.handle.PID()}, nil
}

type persistentReconcileHandle struct {
	mu                 sync.Mutex
	data               []byte
	ack                int64
	sessionID          string
	cleanups           atomic.Int32
	readOnce           sync.Once
	readCalled         chan struct{}
	terminalOnce       sync.Once
	terminalCalled     chan struct{}
	terminalGeneration string
	statusFailures     int
	terminalFailures   int
	signals            atomic.Int32
}

func newPersistentReconcileHandle(data []byte) *persistentReconcileHandle {
	return &persistentReconcileHandle{data: append([]byte(nil), data...), readCalled: make(chan struct{}), terminalCalled: make(chan struct{}), terminalGeneration: "generation-reconcile"}
}
func (h *persistentReconcileHandle) PID() int           { return 4242 }
func (h *persistentReconcileHandle) Write([]byte) error { return nil }
func (h *persistentReconcileHandle) CloseStdin() error  { return nil }
func (h *persistentReconcileHandle) Signal(name string) receipt.SignalEvidence {
	h.signals.Add(1)
	return receipt.SignalEvidence{Requested: name, Attempted: true, Succeeded: true}
}
func (h *persistentReconcileHandle) Wait(context.Context) receipt.ExitEvidence {
	code := 0
	return receipt.ExitEvidence{Reaped: true, Code: &code}
}
func (h *persistentReconcileHandle) Close() error { return nil }
func (h *persistentReconcileHandle) WriteInput(_ context.Context, offset int64, data []byte, eof bool) (persistentapp.InputResult, error) {
	return persistentapp.InputResult{AcceptedBytes: len(data), NextOffset: offset + int64(len(data)), EOFDelivered: eof}, nil
}
func (h *persistentReconcileHandle) SignalWithID(_ context.Context, killID, signalName string) (persistentapp.KillResult, error) {
	return persistentapp.KillResult{KillID: killID, Signal: signalName, Attempted: true, Succeeded: true, Needed: true}, nil
}
func (h *persistentReconcileHandle) ReadOutput(_ context.Context, offset int64, max int) ([]byte, int64, int64, error) {
	h.readOnce.Do(func() { close(h.readCalled) })
	h.mu.Lock()
	defer h.mu.Unlock()
	extent := int64(len(h.data))
	if offset > extent {
		return nil, offset, extent, nil
	}
	end := offset + int64(max)
	if end > extent {
		end = extent
	}
	data := append([]byte(nil), h.data[offset:end]...)
	return data, end, extent, nil
}
func (h *persistentReconcileHandle) AcknowledgeOutput(_ context.Context, offset int64) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ack = offset
	return nil
}
func (h *persistentReconcileHandle) Status(context.Context) (persistentapp.Status, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.statusFailures > 0 {
		h.statusFailures--
		return persistentapp.Status{}, errors.New("transient supervisor status failure")
	}
	return persistentapp.Status{SessionID: h.sessionID, GenerationID: "generation-reconcile", State: session.Running, Change: 1, PID: 4242, OutputBytes: int64(len(h.data)), OutputAcknowledged: h.ack, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}}, nil
}
func (h *persistentReconcileHandle) WaitStatus(context.Context, uint64, int) (persistentapp.Status, error) {
	code := 0
	h.mu.Lock()
	defer h.mu.Unlock()
	return persistentapp.Status{SessionID: h.sessionID, GenerationID: "generation-reconcile", State: session.Completed, Outcome: session.Success, Change: 2, OutputBytes: int64(len(h.data)), OutputAcknowledged: h.ack, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}, Exit: receipt.ExitEvidence{Reaped: true, Code: &code}}, nil
}
func (h *persistentReconcileHandle) Terminal(context.Context) (persistentapp.TerminalEvidence, error) {
	h.terminalOnce.Do(func() { close(h.terminalCalled) })
	code := 0
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.terminalFailures > 0 {
		h.terminalFailures--
		return persistentapp.TerminalEvidence{}, errors.New("transient supervisor terminal failure")
	}
	return persistentapp.TerminalEvidence{SessionID: h.sessionID, GenerationID: h.terminalGeneration, State: session.Completed, Outcome: session.Success, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}, Exit: receipt.ExitEvidence{Reaped: true, Code: &code}, OutputBytes: int64(len(h.data)), OutputComplete: true}, nil
}
func (h *persistentReconcileHandle) RecoveryState(context.Context) (int64, int64, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.ack, int64(len(h.data)), nil
}
func (h *persistentReconcileHandle) Cleanup(context.Context) error {
	h.cleanups.Add(1)
	return nil
}
func (h *persistentReconcileHandle) Ack() int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.ack
}

var _ persistentapp.RecoveryAttachment = (*persistentReconcileHandle)(nil)

func TestPersistentReconciliationOutputConflictKeepsCanonicalTruthLive(t *testing.T) {
	store := openPersistentLaunchStore(t)
	handle := newPersistentReconcileHandle([]byte("hello"))
	runtime := &persistentReconcileRuntime{store: store, handle: handle, seedCanonical: []byte("Xello")}
	worker := &recordingTelemetryWorker{store: store}
	svc := app.NewService(store, &fakeOwner{}, app.Options{Incarnation: "persistent-conflict", Shell: "/bin/sh", MaxQueuedInputBytes: 100, PersistentRuntime: runtime, TelemetryWorker: worker})
	started, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "persistent-output-conflict", Command: "printf hello", CWD: "/", Persistent: true, SessionName: "persistent-output-conflict"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-handle.readCalled:
	case <-time.After(time.Second):
		t.Fatal("reconciler did not inspect supervisor output")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var terminal app.View
	for {
		terminal, err = svc.Poll(ctx, app.PollRequest{SessionID: started.SessionID, YieldMS: 20})
		if err != nil {
			t.Fatal(err)
		}
		if terminal.State.Terminal() {
			break
		}
	}
	if terminal.State != session.Abandoned || terminal.Outcome != session.Ambiguous {
		t.Fatalf("terminal=%#v", terminal)
	}
	binding, err := store.LoadPersistentBinding(context.Background(), operation.SessionID(started.SessionID))
	if err != nil || binding.Lifecycle != persistentcore.LifecycleLost {
		t.Fatalf("binding=%#v err=%v", binding, err)
	}
	rec, err := store.LoadReceipt(context.Background(), operation.SessionID(started.SessionID))
	if err != nil || rec.State != session.Abandoned || rec.Outcome != session.Ambiguous || rec.FailureReason != "persistent_recovery_output_conflict" {
		t.Fatalf("receipt=%#v err=%v", rec, err)
	}
	if handle.cleanups.Load() != 0 || handle.signals.Load() != 0 || worker.count() != 0 {
		t.Fatalf("conflict cleanups=%d signals=%d telemetry=%d", handle.cleanups.Load(), handle.signals.Load(), worker.count())
	}
}

func TestPersistentReconciliationRejectsWrongTerminalGenerationWithoutCleanup(t *testing.T) {
	store := openPersistentLaunchStore(t)
	handle := newPersistentReconcileHandle(nil)
	handle.terminalGeneration = "wrong-generation"
	runtime := &persistentReconcileRuntime{store: store, handle: handle}
	svc := app.NewService(store, &fakeOwner{}, app.Options{Incarnation: "persistent-terminal-conflict", Shell: "/bin/sh", MaxQueuedInputBytes: 100, PersistentRuntime: runtime})
	started, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "persistent-terminal-conflict", Command: "true", CWD: "/", Persistent: true, SessionName: "persistent-terminal-conflict"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-handle.terminalCalled:
	case <-time.After(time.Second):
		t.Fatal("reconciler did not inspect terminal evidence")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var terminal app.View
	for {
		terminal, err = svc.Poll(ctx, app.PollRequest{SessionID: started.SessionID, YieldMS: 20})
		if err != nil {
			t.Fatal(err)
		}
		if terminal.State.Terminal() {
			break
		}
	}
	if terminal.State != session.Abandoned || terminal.Outcome != session.Ambiguous {
		t.Fatalf("terminal=%#v", terminal)
	}
	binding, err := store.LoadPersistentBinding(context.Background(), operation.SessionID(started.SessionID))
	if err != nil || binding.Lifecycle != persistentcore.LifecycleLost {
		t.Fatalf("binding=%#v err=%v", binding, err)
	}
	rec, err := store.LoadReceipt(context.Background(), operation.SessionID(started.SessionID))
	if err != nil || rec.State != session.Abandoned || rec.Outcome != session.Ambiguous || rec.FailureReason != "supervisor_state_conflict_terminal_identity" {
		t.Fatalf("receipt=%#v err=%v", rec, err)
	}
	if handle.cleanups.Load() != 0 || handle.signals.Load() != 0 {
		t.Fatalf("wrong generation cleanup=%d signals=%d", handle.cleanups.Load(), handle.signals.Load())
	}
}
