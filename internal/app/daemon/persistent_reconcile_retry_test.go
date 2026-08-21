package daemon_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	persistentapp "github.com/maemreyo/shellbeam/internal/app/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	persistentcore "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

// flakyReconcileHandle fails its first status wait and its first terminal read,
// then behaves normally.
//
// That pairing is the whole point. Reconciliation already tolerates a failed
// WaitStatus by falling back to reading the terminal record, so only a moment
// where both are briefly unavailable -- one supervisor round trip landing badly
// -- reaches the give-up path. On a loaded host that moment is ordinary.
type flakyReconcileHandle struct {
	*persistentReconcileHandle
	waitFailures     atomic.Int32
	terminalFailures atomic.Int32
}

func (h *flakyReconcileHandle) WaitStatus(ctx context.Context, after uint64, waitMS int) (persistentapp.Status, error) {
	if h.waitFailures.Add(-1) >= 0 {
		return persistentapp.Status{}, fmt.Errorf("supervisor wait unavailable")
	}
	return h.persistentReconcileHandle.WaitStatus(ctx, after, waitMS)
}

func (h *flakyReconcileHandle) Terminal(ctx context.Context) (persistentapp.TerminalEvidence, error) {
	if h.terminalFailures.Add(-1) >= 0 {
		return persistentapp.TerminalEvidence{}, fmt.Errorf("supervisor terminal unavailable")
	}
	return h.persistentReconcileHandle.Terminal(ctx)
}

var _ persistentapp.RecoveryAttachment = (*flakyReconcileHandle)(nil)

// flakyReconcileRuntime launches the flaky handle through the same durable
// binding bookkeeping the ordinary reconcile double performs.
type flakyReconcileRuntime struct {
	store  *storeadapter.Repository
	handle *flakyReconcileHandle
}

func (r *flakyReconcileRuntime) Ensure(ctx context.Context, reservation operation.Reservation, _ operation.ExecutionSpec) (app.PersistentLaunch, error) {
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
	return app.PersistentLaunch{Handle: r.handle, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}, PID: r.handle.PID()}, nil
}

// TestPersistentReconciliationSurvivesATransientSupervisorFailure is the
// regression test for a session that stays live forever after its process is
// already gone.
//
// Reconciliation is the only thing that moves a persistent session's terminal
// transition into durable state, and it ran once: every error path returned,
// the goroutine that owned it exited, and its error was discarded by the caller
// with no path that ever started it again. A single unlucky supervisor round
// trip therefore stranded the session as live permanently -- poll would report
// it running long after the child had exited, which is indistinguishable from a
// hung command and is what an eight second deadline in a test finally caught.
//
// The failure is rare and load-dependent, so this drives it deterministically
// rather than by repetition.
func TestPersistentReconciliationSurvivesATransientSupervisorFailure(t *testing.T) {
	store := openPersistentLaunchStore(t)
	handle := &flakyReconcileHandle{persistentReconcileHandle: newPersistentReconcileHandle([]byte("hello"))}
	handle.waitFailures.Store(1)
	handle.terminalFailures.Store(1)
	runtime := &flakyReconcileRuntime{store: store, handle: handle}
	worker := &recordingTelemetryWorker{store: store}
	svc := app.NewService(store, &fakeOwner{}, app.Options{
		Incarnation: "persistent-reconcile-retry", Shell: "/bin/sh", MaxQueuedInputBytes: 100,
		PersistentRuntime: runtime, TelemetryWorker: worker,
	})
	// The reconciler owns this session past its terminal transition, so without
	// this the goroutine keeps writing bindings into the store after the test
	// returns and races t.TempDir removal. Registered after the store helper, so
	// it runs before the directory is deleted.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := svc.Shutdown(ctx); err != nil {
			t.Errorf("shutdown left the persistent reconciler running: %v", err)
		}
	})

	started, err := svc.Start(context.Background(), app.StartRequest{
		ProtocolVersion: 2, OperationID: "persistent-retry-op", Command: "printf hello", CWD: "/",
		Persistent: true, SessionName: "persistent-retry", YieldMS: 0,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for {
		view, pollErr := svc.Poll(ctx, app.PollRequest{SessionID: started.SessionID, YieldMS: 50})
		if pollErr != nil {
			t.Fatalf("the session never reached terminal after one transient supervisor failure: %v", pollErr)
		}
		if view.State.Terminal() {
			return
		}
	}
}
