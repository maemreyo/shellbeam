package daemon_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	persistentapp "github.com/maemreyo/shellbeam/internal/app/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/observation"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	persistentcore "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func TestPersistentStartupRepairsCanonicalSpawnFailureBeforeReattach(t *testing.T) {
	store := openPersistentLaunchStore(t)
	binding := reserveStartupPersistent(t, store, "startup-stale-terminal", persistentcore.LifecycleProvisioning)
	reservation, err := store.LoadOperation(context.Background(), operation.ID(binding.OperationID))
	if err != nil {
		t.Fatal(err)
	}
	rec := receipt.Receipt{
		SchemaVersion: 4, OperationID: string(reservation.OperationID), SessionID: string(reservation.SessionID),
		RequestFingerprint: reservation.RequestFingerprint, ExecutionFingerprint: reservation.ExecutionFingerprint, ObservationBindingFingerprint: reservation.ObservationBindingFingerprint,
		DaemonIncarnation: reservation.DaemonIncarnation, ExecutionMode: string(reservation.ExecutionMode), Executable: reservation.Executable,
		State: session.Failed, Outcome: session.Failure, Shell: reservation.Shell, CWD: reservation.CWD, TimeoutMS: reservation.TimeoutMS,
		Persistent: true, SessionName: reservation.SessionName, FailureReason: "persistent_spawn_failed",
	}
	if result := store.PublishTerminal(context.Background(), rec); result.Err != nil {
		t.Fatal(result.Err)
	}

	runtime := &startupPersistentRuntime{reattachErr: errors.New("reattach must not run for canonical terminal session")}
	svc := app.NewService(store, &fakeOwner{}, app.Options{
		Incarnation: "new-daemon", Shell: "/bin/sh", MaxQueuedInputBytes: 100, PersistentRuntime: runtime,
	})
	if err := svc.ReconcilePersistentStartup(context.Background(), []persistentcore.Binding{binding}, app.PersistentStartupOptions{}); err != nil {
		t.Fatal(err)
	}
	if runtime.reattachCalls.Load() != 0 || runtime.ensureCalls.Load() != 0 {
		t.Fatalf("reattach=%d ensure=%d", runtime.reattachCalls.Load(), runtime.ensureCalls.Load())
	}
	stored, err := store.LoadPersistentBinding(context.Background(), operation.SessionID(binding.SessionID))
	if err != nil || stored.Lifecycle != persistentcore.LifecycleLost {
		t.Fatalf("binding=%#v err=%v", stored, err)
	}
	candidates, err := store.ListPersistentRecoveryCandidates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range candidates {
		if candidate.SessionID == binding.SessionID {
			t.Fatalf("terminal session remained recovery candidate: %#v", candidate)
		}
	}
}

func TestPersistentStartupReattachesSameSessionAndExposesCurrentPIDOnlyAfterProof(t *testing.T) {
	store := openPersistentLaunchStore(t)
	binding := reserveStartupPersistent(t, store, "startup-live", persistentcore.LifecycleLive)
	handle := newStartupLiveHandle(binding.SessionID, binding.SupervisorGenerationID, 4343)
	runtime := &startupPersistentRuntime{reattach: app.PersistentReattach{
		Handle: handle, State: session.Running, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}, PID: 4343,
	}}
	svc := app.NewService(store, &fakeOwner{}, app.Options{
		Incarnation: "new-daemon", Shell: "/bin/sh", MaxQueuedInputBytes: 100, PersistentRuntime: runtime,
	})
	if err := svc.ReconcilePersistentStartup(context.Background(), []persistentcore.Binding{binding}, app.PersistentStartupOptions{}); err != nil {
		t.Fatal(err)
	}
	if runtime.ensureCalls.Load() != 0 || runtime.reattachCalls.Load() != 1 {
		t.Fatalf("ensure=%d reattach=%d", runtime.ensureCalls.Load(), runtime.reattachCalls.Load())
	}
	obligations, eventErr := store.ListObservationObligations(context.Background(), 0, 100)
	if eventErr != nil {
		t.Fatal(eventErr)
	}
	reattachedEvents := 0
	for _, item := range obligations {
		if item.Kind == observation.EventPersistentSessionReattached && item.State == observation.ObligationCommitted {
			reattachedEvents++
		}
	}
	if reattachedEvents != 1 {
		t.Fatalf("reattached events=%d obligations=%#v", reattachedEvents, obligations)
	}
	resolved, err := svc.ResolveProcessSession(context.Background(), binding.SessionID)
	if err != nil || !resolved.Known || !resolved.Current || resolved.PID != 4343 {
		t.Fatalf("resolved=%#v err=%v", resolved, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := svc.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if handle.signals.Load() != 0 || handle.closes.Load() != 1 {
		t.Fatalf("signals=%d closes=%d", handle.signals.Load(), handle.closes.Load())
	}
	after, err := svc.ResolveProcessSession(context.Background(), binding.SessionID)
	if err != nil || !after.Known || after.Current || after.PID != 0 {
		t.Fatalf("after detach=%#v err=%v", after, err)
	}
}

func TestPersistentStartupReattachFailureBecomesCanonicalLostWithoutRelaunchOrPID(t *testing.T) {
	store := openPersistentLaunchStore(t)
	binding := reserveStartupPersistent(t, store, "startup-lost", persistentcore.LifecycleLive)
	runtime := &startupPersistentRuntime{reattachErr: failure.New(failure.SupervisorAuthFailed, map[string]string{"reason": "proof"}, nil)}
	svc := app.NewService(store, &fakeOwner{}, app.Options{
		Incarnation: "new-daemon", Shell: "/bin/sh", MaxQueuedInputBytes: 100, PersistentRuntime: runtime,
	})
	if err := svc.ReconcilePersistentStartup(context.Background(), []persistentcore.Binding{binding}, app.PersistentStartupOptions{}); err != nil {
		t.Fatal(err)
	}
	if runtime.ensureCalls.Load() != 0 || runtime.reattachCalls.Load() != 1 {
		t.Fatalf("ensure=%d reattach=%d", runtime.ensureCalls.Load(), runtime.reattachCalls.Load())
	}
	rec, err := store.LoadReceipt(context.Background(), operation.SessionID(binding.SessionID))
	if err != nil || rec.State != session.Abandoned || rec.Outcome != session.Ambiguous || rec.FailureReason != "supervisor_auth_failed" {
		t.Fatalf("receipt=%#v err=%v", rec, err)
	}
	stored, err := store.LoadPersistentBinding(context.Background(), operation.SessionID(binding.SessionID))
	if err != nil || stored.Lifecycle != persistentcore.LifecycleLost {
		t.Fatalf("binding=%#v err=%v", stored, err)
	}
	resolved, err := svc.ResolveProcessSession(context.Background(), binding.SessionID)
	if err != nil || !resolved.Known || resolved.Current || resolved.PID != 0 {
		t.Fatalf("resolved=%#v err=%v", resolved, err)
	}
}

func TestPersistentStartupTerminalRecoveryPublishesCanonicalTerminalWithoutRelaunch(t *testing.T) {
	store := openPersistentLaunchStore(t)
	binding := reserveStartupPersistent(t, store, "startup-terminal", persistentcore.LifecycleProvisioning)
	handle := newPersistentReconcileHandle([]byte("done"))
	handle.mu.Lock()
	handle.sessionID = binding.SessionID
	handle.terminalGeneration = binding.SupervisorGenerationID
	handle.mu.Unlock()
	runtime := &startupPersistentRuntime{reattach: app.PersistentReattach{
		Handle: handle, State: session.Completed, Outcome: session.Success, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true},
	}}
	svc := app.NewService(store, &fakeOwner{}, app.Options{
		Incarnation: "new-daemon", Shell: "/bin/sh", MaxQueuedInputBytes: 100, PersistentRuntime: runtime,
	})
	if err := svc.ReconcilePersistentStartup(context.Background(), []persistentcore.Binding{binding}, app.PersistentStartupOptions{}); err != nil {
		t.Fatal(err)
	}
	if runtime.ensureCalls.Load() != 0 || runtime.reattachCalls.Load() != 1 {
		t.Fatalf("ensure=%d reattach=%d", runtime.ensureCalls.Load(), runtime.reattachCalls.Load())
	}
	rec, err := store.LoadReceipt(context.Background(), operation.SessionID(binding.SessionID))
	if err != nil || rec.State != session.Completed || rec.Outcome != session.Success || rec.OutputBytes != 4 || !rec.OutputComplete {
		t.Fatalf("receipt=%#v err=%v", rec, err)
	}
	stored, err := store.LoadPersistentBinding(context.Background(), operation.SessionID(binding.SessionID))
	if err != nil || stored.Lifecycle != persistentcore.LifecycleTerminal {
		t.Fatalf("binding=%#v err=%v", stored, err)
	}
	resolved, err := svc.ResolveProcessSession(context.Background(), binding.SessionID)
	if err != nil || !resolved.Known || resolved.Current || resolved.PID != 0 {
		t.Fatalf("resolved=%#v err=%v", resolved, err)
	}
}

func reserveStartupPersistent(t *testing.T, store *storeadapter.Repository, suffix string, lifecycle persistentcore.Lifecycle) persistentcore.Binding {
	t.Helper()
	now := time.Date(2026, 8, 16, 2, 0, 0, 0, time.UTC)
	sessionID := "persistent-" + suffix
	operationID := "persistent-" + suffix + "-op"
	reservation := operation.Reservation{
		SchemaVersion: 4, OperationID: operation.ID(operationID), SessionID: operation.SessionID(sessionID),
		RequestFingerprint: strings.Repeat("a", 64), ExecutionFingerprint: strings.Repeat("b", 64), ObservationBindingFingerprint: strings.Repeat("c", 64),
		ExecutionMode: operation.ExecutionModeShell, Executable: "/bin/sh", Command: "sleep 10", CWD: "/tmp", Shell: "/bin/sh",
		Persistent: true, SessionName: suffix, DaemonIncarnation: "old-daemon", CreatedAt: now,
	}
	if _, created, result := store.ReserveOperation(context.Background(), reservation); result.Err != nil || !created {
		t.Fatalf("reserve operation created=%v result=%#v", created, result)
	}
	binding := persistentcore.Binding{
		SchemaVersion: persistentcore.SchemaVersion, SessionID: sessionID, OperationID: operationID, SessionName: suffix,
		Persistent: true, Supervision: persistentcore.SupervisionPerSession, Continuity: persistentcore.ContinuityDaemonRestart,
		SupervisorGenerationID: "generation-reconcile", SupervisorEndpointRef: "endpoint-" + suffix,
		Lifecycle: persistentcore.LifecycleProvisioning, CreatedAt: now, UpdatedAt: now,
	}
	stored, created, result := store.ReservePersistentBinding(context.Background(), binding)
	if result.Err != nil || !created {
		t.Fatalf("reserve binding created=%v result=%#v", created, result)
	}
	if lifecycle == persistentcore.LifecycleLive {
		stored.Lifecycle = persistentcore.LifecycleLive
		stored.UpdatedAt = now.Add(time.Nanosecond)
		if result := store.AdvancePersistentBinding(context.Background(), stored); result.Err != nil {
			t.Fatal(result.Err)
		}
		return stored
	}
	return stored
}

type startupPersistentRuntime struct {
	ensureCalls   atomic.Int32
	reattachCalls atomic.Int32
	reattach      app.PersistentReattach
	reattachErr   error
}

func (r *startupPersistentRuntime) Ensure(context.Context, operation.Reservation, operation.ExecutionSpec) (app.PersistentLaunch, error) {
	r.ensureCalls.Add(1)
	return app.PersistentLaunch{}, errors.New("startup must not launch")
}

func (r *startupPersistentRuntime) Reattach(context.Context, persistentcore.Binding) (app.PersistentReattach, error) {
	r.reattachCalls.Add(1)
	return r.reattach, r.reattachErr
}

type startupLiveHandle struct {
	sessionID  string
	generation string
	pid        int
	closes     atomic.Int32
	signals    atomic.Int32
}

func newStartupLiveHandle(sessionID, generation string, pid int) *startupLiveHandle {
	return &startupLiveHandle{sessionID: sessionID, generation: generation, pid: pid}
}

func (h *startupLiveHandle) PID() int           { return h.pid }
func (h *startupLiveHandle) Write([]byte) error { return nil }
func (h *startupLiveHandle) CloseStdin() error  { return nil }
func (h *startupLiveHandle) Signal(name string) receipt.SignalEvidence {
	h.signals.Add(1)
	return receipt.SignalEvidence{Requested: name, Attempted: true, Succeeded: true}
}
func (h *startupLiveHandle) Wait(context.Context) receipt.ExitEvidence { return receipt.ExitEvidence{} }
func (h *startupLiveHandle) Close() error                              { h.closes.Add(1); return nil }
func (h *startupLiveHandle) WriteInput(_ context.Context, offset int64, data []byte, eof bool) (persistentapp.InputResult, error) {
	return persistentapp.InputResult{AcceptedBytes: len(data), NextOffset: offset + int64(len(data)), EOFDelivered: eof}, nil
}
func (h *startupLiveHandle) SignalWithID(_ context.Context, killID, signalName string) (persistentapp.KillResult, error) {
	h.signals.Add(1)
	return persistentapp.KillResult{KillID: killID, Signal: signalName, Attempted: true, Succeeded: true, Needed: true}, nil
}
func (h *startupLiveHandle) ReadOutput(context.Context, int64, int) ([]byte, int64, int64, error) {
	return nil, 0, 0, nil
}
func (h *startupLiveHandle) AcknowledgeOutput(context.Context, int64) error { return nil }
func (h *startupLiveHandle) Status(context.Context) (persistentapp.Status, error) {
	return persistentapp.Status{
		SessionID: h.sessionID, GenerationID: h.generation, State: session.Running, PID: h.pid,
		Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true},
	}, nil
}
func (h *startupLiveHandle) WaitStatus(ctx context.Context, _ uint64, _ int) (persistentapp.Status, error) {
	<-ctx.Done()
	return persistentapp.Status{}, ctx.Err()
}
func (h *startupLiveHandle) Terminal(context.Context) (persistentapp.TerminalEvidence, error) {
	return persistentapp.TerminalEvidence{}, failure.New(failure.SupervisorUnavailable, map[string]string{"reason": "still_running"}, nil)
}
func (h *startupLiveHandle) RecoveryState(context.Context) (int64, int64, error) { return 0, 0, nil }
func (h *startupLiveHandle) Cleanup(context.Context) error                       { return nil }

var _ persistentapp.RecoveryAttachment = (*startupLiveHandle)(nil)

func TestPersistentStartupBoundsConcurrencyAndClassifiesBudgetRemainderLost(t *testing.T) {
	store := openPersistentLaunchStore(t)
	bindings := make([]persistentcore.Binding, 0, 4)
	for i := 0; i < 4; i++ {
		bindings = append(bindings, reserveStartupPersistent(t, store, "startup-budget-"+string(rune('a'+i)), persistentcore.LifecycleLive))
	}
	runtime := &blockingStartupRuntime{}
	svc := app.NewService(store, &fakeOwner{}, app.Options{
		Incarnation: "new-daemon", Shell: "/bin/sh", MaxQueuedInputBytes: 100, PersistentRuntime: runtime,
	})
	started := time.Now()
	err := svc.ReconcilePersistentStartup(context.Background(), bindings, app.PersistentStartupOptions{
		PerSession: 10 * time.Millisecond, MaxConcurrency: 2, TotalBudget: 18 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("bounded startup reconciliation exceeded test budget")
	}
	if runtime.ensureCalls.Load() != 0 || runtime.maxActive.Load() > 2 {
		t.Fatalf("ensure=%d max_active=%d", runtime.ensureCalls.Load(), runtime.maxActive.Load())
	}
	for _, binding := range bindings {
		stored, loadErr := store.LoadPersistentBinding(context.Background(), operation.SessionID(binding.SessionID))
		if loadErr != nil || stored.Lifecycle != persistentcore.LifecycleLost {
			t.Fatalf("binding %s=%#v err=%v", binding.SessionID, stored, loadErr)
		}
		rec, loadErr := store.LoadReceipt(context.Background(), operation.SessionID(binding.SessionID))
		if loadErr != nil || rec.State != session.Abandoned || rec.Outcome != session.Ambiguous {
			t.Fatalf("receipt %s=%#v err=%v", binding.SessionID, rec, loadErr)
		}
	}
}

type blockingStartupRuntime struct {
	ensureCalls atomic.Int32
	active      atomic.Int32
	maxActive   atomic.Int32
}

func (r *blockingStartupRuntime) Ensure(context.Context, operation.Reservation, operation.ExecutionSpec) (app.PersistentLaunch, error) {
	r.ensureCalls.Add(1)
	return app.PersistentLaunch{}, errors.New("startup must not launch")
}

func (r *blockingStartupRuntime) Reattach(ctx context.Context, _ persistentcore.Binding) (app.PersistentReattach, error) {
	active := r.active.Add(1)
	for {
		max := r.maxActive.Load()
		if active <= max || r.maxActive.CompareAndSwap(max, active) {
			break
		}
	}
	defer r.active.Add(-1)
	<-ctx.Done()
	return app.PersistentReattach{}, ctx.Err()
}
