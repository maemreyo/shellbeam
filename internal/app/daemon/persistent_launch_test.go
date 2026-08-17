package daemon_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	persistentapp "github.com/maemreyo/shellbeam/internal/app/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	persistentcore "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func TestOrdinaryStartDoesNotCallPersistentRuntime(t *testing.T) {
	store := openPersistentLaunchStore(t)
	owner := &fakeOwner{}
	runtime := &fakePersistentRuntime{}
	svc := app.NewService(store, owner, app.Options{Incarnation: "direct", Shell: "/bin/sh", MaxQueuedInputBytes: 100, PersistentRuntime: runtime})
	view, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "direct-no-tax", Command: "true", CWD: "/", YieldMS: 100})
	if err != nil {
		t.Fatal(err)
	}
	if view.SessionID == "" || owner.starts.Load() != 1 || runtime.calls.Load() != 0 {
		t.Fatalf("view=%#v direct_starts=%d persistent_calls=%d", view, owner.starts.Load(), runtime.calls.Load())
	}
	waitForTerminal(t, svc, view.SessionID)
}

func TestPersistentStartUsesRuntimeAfterDurableReservationAndRetriesWithoutRelaunch(t *testing.T) {
	store := openPersistentLaunchStore(t)
	owner := &fakeOwner{}
	handle := &persistentFakeHandle{pid: 4242}
	runtime := &fakePersistentRuntime{launch: app.PersistentLaunch{Handle: handle, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}, PID: 4242}}
	runtime.onEnsure = func(res operation.Reservation) {
		loaded, err := store.LoadOperation(context.Background(), res.OperationID)
		if err != nil || loaded.SessionID != res.SessionID || !loaded.Persistent || loaded.SessionName != "dev-server" {
			t.Fatalf("runtime called before durable reservation: loaded=%#v err=%v", loaded, err)
		}
	}
	svc := app.NewService(store, owner, app.Options{Incarnation: "persistent", Shell: "/bin/sh", MaxQueuedInputBytes: 100, PersistentRuntime: runtime})
	req := app.StartRequest{ProtocolVersion: 2, OperationID: "persistent-launch", Command: "sleep 10", CWD: "/", Persistent: true, SessionName: "dev-server"}
	first, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if first.State != session.Running || first.SessionID == "" || runtime.calls.Load() != 1 || owner.starts.Load() != 0 {
		t.Fatalf("first=%#v persistent_calls=%d direct_starts=%d", first, runtime.calls.Load(), owner.starts.Load())
	}
	snapshot, err := store.LoadSession(context.Background(), operation.SessionID(first.SessionID))
	if err != nil || snapshot.State != session.Running {
		t.Fatalf("snapshot=%#v err=%v", snapshot, err)
	}
	resolved, err := svc.ResolveProcessSession(context.Background(), first.SessionID)
	if err != nil || !resolved.Known || !resolved.Current || resolved.PID != 4242 {
		t.Fatalf("resolved=%#v err=%v", resolved, err)
	}
	second, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if second.SessionID != first.SessionID || runtime.calls.Load() != 1 || owner.starts.Load() != 0 {
		t.Fatalf("second=%#v calls=%d direct=%d", second, runtime.calls.Load(), owner.starts.Load())
	}
	changed := req
	changed.SessionName = "other-name"
	if _, err := svc.Start(context.Background(), changed); !errors.Is(err, failure.OperationConflict) {
		t.Fatalf("changed retry err=%v", err)
	}
}

// A persistent launch that fails must not strand its reservation at Starting
// forever: nothing else will ever revisit it, since restart-time reconciliation
// defers to persistent recovery for anything marked Persistent, and recovery
// only finds sessions that got as far as a binding. Before this test's
// behavior was fixed, a failed persistent launch parked the session at
// Starting permanently, leaking one of MaxSessions capacity slots per
// failure until every slot was exhausted and every future start -- persistent
// or not -- failed with capacity_exceeded no matter how many times the daemon
// was restarted.
type provisioningFailureRuntime struct {
	store *storeadapter.Repository
}

func (r *provisioningFailureRuntime) Ensure(ctx context.Context, reservation operation.Reservation, _ operation.ExecutionSpec) (app.PersistentLaunch, error) {
	binding := persistentcore.Binding{
		SchemaVersion: persistentcore.SchemaVersion, SessionID: string(reservation.SessionID), OperationID: string(reservation.OperationID),
		ActivityID: reservation.ActivityID, WorkspaceID: reservation.WorkspaceID, SessionName: reservation.SessionName,
		Persistent: true, Supervision: persistentcore.SupervisionPerSession, Continuity: persistentcore.ContinuityDaemonRestart,
		SupervisorGenerationID: "generation-start-failure", SupervisorEndpointRef: "endpoint-start-failure",
		Lifecycle: persistentcore.LifecycleProvisioning, CreatedAt: reservation.CreatedAt.UTC(), UpdatedAt: reservation.CreatedAt.UTC(),
	}
	if _, _, result := r.store.ReservePersistentBinding(ctx, binding); result.Err != nil {
		return app.PersistentLaunch{}, result.Err
	}
	return app.PersistentLaunch{}, failure.New(failure.SupervisorUnavailable, map[string]string{"reason": "injected_after_provisioning"}, nil)
}

func TestPersistentLaunchFailureAfterProvisioningClosesBindingAndRecoveryMarker(t *testing.T) {
	store, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 1, MaxSessionOutput: 1 << 20, MaxTotalState: 8 << 20, ControlReserve: 4096})
	if err != nil {
		t.Fatal(err)
	}
	runtime := &provisioningFailureRuntime{store: store}
	svc := app.NewService(store, &fakeOwner{}, app.Options{Incarnation: "persistent", Shell: "/bin/sh", MaxQueuedInputBytes: 100, PersistentRuntime: runtime})

	_, err = svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "persistent-fail-after-binding", Command: "sleep 10", CWD: "/", Persistent: true, SessionName: "fail-after-binding"})
	if !errors.Is(err, failure.SupervisorUnavailable) {
		t.Fatalf("start err=%v", err)
	}
	reservation, err := store.LoadOperation(context.Background(), "persistent-fail-after-binding")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.LoadSession(context.Background(), reservation.SessionID)
	if err != nil || snapshot.State != session.Failed {
		t.Fatalf("snapshot=%#v err=%v", snapshot, err)
	}
	stored, err := store.LoadPersistentBinding(context.Background(), reservation.SessionID)
	if err != nil || stored.Lifecycle != persistentcore.LifecycleLost {
		t.Fatalf("binding=%#v err=%v", stored, err)
	}
	candidates, err := store.ListPersistentRecoveryCandidates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range candidates {
		if candidate.SessionID == string(reservation.SessionID) {
			t.Fatalf("failed session remained recovery candidate: %#v", candidate)
		}
	}
	second, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "after-persistent-binding-fail", Command: "true", CWD: "/", YieldMS: 100})
	if err != nil {
		t.Fatalf("capacity remained occupied: %v", err)
	}
	_ = waitForTerminal(t, svc, second.SessionID)
}

func TestPersistentLaunchFailureDoesNotPublishRunningOrUseDirectOwner(t *testing.T) {
	// A capacity of exactly one makes a leaked slot observable: the second
	// Start below can only succeed if the failed launch actually released it.
	store, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 1, MaxSessionOutput: 1 << 20, MaxTotalState: 8 << 20, ControlReserve: 4096})
	if err != nil {
		t.Fatal(err)
	}
	owner := &fakeOwner{}
	runtime := &fakePersistentRuntime{err: failure.New(failure.SupervisorUnavailable, map[string]string{"reason": "readiness"}, nil)}
	svc := app.NewService(store, owner, app.Options{Incarnation: "persistent", Shell: "/bin/sh", MaxQueuedInputBytes: 100, PersistentRuntime: runtime})
	_, err = svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "persistent-fail", Command: "sleep 10", CWD: "/", Persistent: true, SessionName: "dev-server"})
	if !errors.Is(err, failure.SupervisorUnavailable) || runtime.calls.Load() != 1 || owner.starts.Load() != 0 {
		t.Fatalf("err=%v calls=%d direct=%d", err, runtime.calls.Load(), owner.starts.Load())
	}
	reservation, err := store.LoadOperation(context.Background(), "persistent-fail")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.LoadSession(context.Background(), reservation.SessionID)
	if err != nil || snapshot.State != session.Failed {
		t.Fatalf("snapshot=%#v err=%v", snapshot, err)
	}
	rec, err := store.LoadReceipt(context.Background(), reservation.SessionID)
	if err != nil || rec.State != session.Failed || rec.Outcome != session.Failure || rec.FailureReason != "persistent_spawn_failed" {
		t.Fatalf("receipt=%#v err=%v", rec, err)
	}
	// The failed launch must not just be reported terminal -- it must have
	// actually released its slot, or every symptom of the leak this test
	// guards against would still be present under the covers.
	second, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "after-persistent-fail", Command: "true", CWD: "/"})
	if err != nil {
		t.Fatalf("start after persistent failure did not free capacity: %v", err)
	}
	waitForTerminal(t, svc, second.SessionID)
}

func TestPersistentMissingControlProofFailsClosedAndShutdownDetaches(t *testing.T) {
	store := openPersistentLaunchStore(t)
	handle := &persistentFakeHandle{pid: 4242}
	runtime := &fakePersistentRuntime{launch: app.PersistentLaunch{Handle: handle, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}, PID: 4242}}
	svc := app.NewService(store, &fakeOwner{}, app.Options{Incarnation: "persistent", Shell: "/bin/sh", MaxQueuedInputBytes: 100, PersistentRuntime: runtime})
	view, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "persistent-detach", Command: "sleep 10", CWD: "/", Persistent: true, SessionName: "dev-server"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Write(context.Background(), app.WriteRequest{SessionID: view.SessionID, InputOffset: 0, Chars: "x"}); !errors.Is(err, failure.SupervisorUnavailable) {
		t.Fatalf("persistent write err=%v", err)
	}
	if _, err := svc.Kill(context.Background(), app.KillRequest{SessionID: view.SessionID, KillID: "kill-persistent", Signal: "TERM"}); !errors.Is(err, failure.SupervisorUnavailable) {
		t.Fatalf("persistent kill err=%v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := svc.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if handle.signals.Load() != 0 || handle.writes.Load() != 0 || handle.closes.Load() != 1 {
		t.Fatalf("signals=%d writes=%d closes=%d", handle.signals.Load(), handle.writes.Load(), handle.closes.Load())
	}
	resolved, err := svc.ResolveProcessSession(context.Background(), view.SessionID)
	if err != nil || !resolved.Known || resolved.Current || resolved.PID != 0 {
		t.Fatalf("post-detach resolved=%#v err=%v", resolved, err)
	}
}

func openPersistentLaunchStore(t *testing.T) *storeadapter.Repository {
	t.Helper()
	store, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 8, MaxSessionOutput: 1 << 20, MaxTotalState: 8 << 20, ControlReserve: 4096})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

type fakePersistentRuntime struct {
	calls    atomic.Int32
	launch   app.PersistentLaunch
	err      error
	onEnsure func(operation.Reservation)
}

func (f *fakePersistentRuntime) Ensure(_ context.Context, reservation operation.Reservation, _ operation.ExecutionSpec) (app.PersistentLaunch, error) {
	f.calls.Add(1)
	if f.onEnsure != nil {
		f.onEnsure(reservation)
	}
	return f.launch, f.err
}

type persistentFakeHandle struct {
	pid     int
	writes  atomic.Int32
	signals atomic.Int32
	closes  atomic.Int32
}

func (h *persistentFakeHandle) Write([]byte) error { h.writes.Add(1); return nil }
func (h *persistentFakeHandle) CloseStdin() error  { h.writes.Add(1); return nil }
func (h *persistentFakeHandle) Signal(string) receipt.SignalEvidence {
	h.signals.Add(1)
	return receipt.SignalEvidence{Attempted: true, Succeeded: true}
}
func (h *persistentFakeHandle) Wait(context.Context) receipt.ExitEvidence {
	return receipt.ExitEvidence{}
}
func (h *persistentFakeHandle) Close() error { h.closes.Add(1); return nil }
func (h *persistentFakeHandle) PID() int     { return h.pid }

func TestPersistentReadinessAmbiguityDoesNotClaimSpawnFailedObservation(t *testing.T) {
	base := openPersistentLaunchStore(t)
	store := &persistentObservationStore{Repository: base}
	runtime := &fakePersistentRuntime{err: failure.New(failure.SupervisorUnavailable, map[string]string{"reason": "readiness"}, nil)}
	svc := app.NewService(store, &fakeOwner{}, app.Options{Incarnation: "persistent", Shell: "/bin/sh", MaxQueuedInputBytes: 100, PersistentRuntime: runtime})
	_, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "persistent-ambiguous-observation", Command: "sleep 10", CWD: "/", Persistent: true, SessionName: "ambiguous-observation"})
	if !errors.Is(err, failure.SupervisorUnavailable) {
		t.Fatalf("start err=%v", err)
	}
	if store.prepares.Load() != 0 || store.aborts.Load() != 0 || store.commits.Load() != 0 {
		t.Fatalf("ambiguous readiness observation prepare=%d abort=%d commit=%d", store.prepares.Load(), store.aborts.Load(), store.commits.Load())
	}
}

type persistentObservationStore struct {
	*storeadapter.Repository
	prepares atomic.Int32
	commits  atomic.Int32
	aborts   atomic.Int32
}

func (s *persistentObservationStore) PrepareProcessStartedObservation(context.Context, string, string) app.StoreResult {
	s.prepares.Add(1)
	return app.StoreResult{Durability: app.DurableChange, ObservationSeq: 101}
}

func (s *persistentObservationStore) CommitObservationSequence(context.Context, uint64) app.StoreResult {
	s.commits.Add(1)
	return app.StoreResult{Durability: app.DurableChange}
}

func (s *persistentObservationStore) AbortObservationSequence(context.Context, uint64, string) app.StoreResult {
	s.aborts.Add(1)
	return app.StoreResult{Durability: app.DurableChange}
}

func TestPersistentWriteAndKillPreserveCallerMutationIdentity(t *testing.T) {
	store := openPersistentLaunchStore(t)
	handle := &persistentControlFakeHandle{persistentFakeHandle: persistentFakeHandle{pid: 4242}}
	runtime := &fakePersistentRuntime{launch: app.PersistentLaunch{Handle: handle, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}, PID: 4242}}
	svc := app.NewService(store, &fakeOwner{}, app.Options{Incarnation: "persistent", Shell: "/bin/sh", MaxQueuedInputBytes: 100, PersistentRuntime: runtime})
	view, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "persistent-control", Command: "cat", CWD: "/", Persistent: true, SessionName: "control"})
	if err != nil {
		t.Fatal(err)
	}
	written, err := svc.Write(context.Background(), app.WriteRequest{SessionID: view.SessionID, InputOffset: 7, Chars: "abc"})
	if err != nil || written.AcceptedInputBytes != 3 || written.NextInputOffset != 10 || handle.lastInputOffset != 7 || handle.lastInput != "abc" {
		t.Fatalf("written=%#v offset=%d input=%q err=%v", written, handle.lastInputOffset, handle.lastInput, err)
	}
	killed, err := svc.Kill(context.Background(), app.KillRequest{SessionID: view.SessionID, KillID: "caller-kill-7", Signal: "TERM"})
	if err != nil || killed.KillID != "caller-kill-7" || killed.Signal != "TERM" || handle.lastKillID != "caller-kill-7" || handle.lastSignal != "TERM" {
		t.Fatalf("killed=%#v kill_id=%q signal=%q err=%v", killed, handle.lastKillID, handle.lastSignal, err)
	}
}

func TestPersistentControlWithoutAuthenticatedAttachmentNeverFallsBackToGenericHandle(t *testing.T) {
	store := openPersistentLaunchStore(t)
	handle := &persistentFakeHandle{pid: 4242}
	runtime := &fakePersistentRuntime{launch: app.PersistentLaunch{Handle: handle, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}, PID: 4242}}
	svc := app.NewService(store, &fakeOwner{}, app.Options{Incarnation: "persistent", Shell: "/bin/sh", MaxQueuedInputBytes: 100, PersistentRuntime: runtime})
	view, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "persistent-no-control-proof", Command: "cat", CWD: "/", Persistent: true, SessionName: "no-control-proof"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Write(context.Background(), app.WriteRequest{SessionID: view.SessionID, InputOffset: 0, Chars: "x"}); !errors.Is(err, failure.SupervisorUnavailable) {
		t.Fatalf("write without control proof err=%v", err)
	}
	if _, err := svc.Kill(context.Background(), app.KillRequest{SessionID: view.SessionID, KillID: "caller-kill", Signal: "TERM"}); !errors.Is(err, failure.SupervisorUnavailable) {
		t.Fatalf("kill without control proof err=%v", err)
	}
	if handle.writes.Load() != 0 || handle.signals.Load() != 0 {
		t.Fatalf("generic fallback writes=%d signals=%d", handle.writes.Load(), handle.signals.Load())
	}
}

type persistentControlFakeHandle struct {
	persistentFakeHandle
	lastInputOffset int64
	lastInput       string
	lastKillID      string
	lastSignal      string
}

func (h *persistentControlFakeHandle) WriteInput(_ context.Context, offset int64, data []byte, eof bool) (persistentapp.InputResult, error) {
	h.lastInputOffset, h.lastInput = offset, string(data)
	if eof {
		return persistentapp.InputResult{NextOffset: offset, EOFDelivered: true}, nil
	}
	return persistentapp.InputResult{AcceptedBytes: len(data), NextOffset: offset + int64(len(data))}, nil
}
func (h *persistentControlFakeHandle) SignalWithID(_ context.Context, killID, signalName string) (persistentapp.KillResult, error) {
	h.lastKillID, h.lastSignal = killID, signalName
	return persistentapp.KillResult{KillID: killID, Signal: signalName, Attempted: true, Succeeded: true, Needed: true}, nil
}
func (h *persistentControlFakeHandle) ReadOutput(context.Context, int64, int) ([]byte, int64, int64, error) {
	return nil, 0, 0, nil
}
func (h *persistentControlFakeHandle) AcknowledgeOutput(context.Context, int64) error { return nil }
func (h *persistentControlFakeHandle) Status(context.Context) (persistentapp.Status, error) {
	return persistentapp.Status{}, nil
}
func (h *persistentControlFakeHandle) WaitStatus(context.Context, uint64, int) (persistentapp.Status, error) {
	return persistentapp.Status{}, nil
}
func (h *persistentControlFakeHandle) Terminal(context.Context) (persistentapp.TerminalEvidence, error) {
	return persistentapp.TerminalEvidence{}, nil
}

type countingPersistentRuntime struct{ calls atomic.Int32 }

func (r *countingPersistentRuntime) Ensure(context.Context, operation.Reservation, operation.ExecutionSpec) (app.PersistentLaunch, error) {
	r.calls.Add(1)
	return app.PersistentLaunch{}, errors.New("ordinary path touched persistent runtime")
}

func TestOrdinaryStartDoesNotTouchPersistentRuntime(t *testing.T) {
	store := openPersistentLaunchStore(t)
	runtime := &countingPersistentRuntime{}
	svc := app.NewService(store, &fakeOwner{}, app.Options{Incarnation: "ordinary-zero-tax", Shell: "/bin/sh", MaxQueuedInputBytes: 100, PersistentRuntime: runtime})
	view, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "ordinary-zero-tax-op", Command: "true", CWD: "/tmp", YieldMS: 0})
	if err != nil {
		t.Fatal(err)
	}
	if view.SessionID == "" {
		t.Fatalf("ordinary start=%#v", view)
	}
	if runtime.calls.Load() != 0 {
		t.Fatalf("ordinary start persistent runtime calls=%d", runtime.calls.Load())
	}
	waitForTerminal(t, svc, view.SessionID)
}
