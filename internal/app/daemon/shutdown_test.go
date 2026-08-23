package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/session"
)

func TestAgentExecutionA1ShutdownDoesNotWaitFullGraceForFinalizingSession(t *testing.T) {
	live := &liveSession{state: session.Finalizing, done: make(chan struct{})}
	service := &Service{
		options: Options{TerminationGrace: time.Second},
		live:    map[string]*liveSession{"s": live},
	}
	go func() {
		time.Sleep(25 * time.Millisecond)
		live.mu.Lock()
		live.state = session.Completed
		live.mu.Unlock()
		close(live.done)
	}()

	started := time.Now()
	if err := service.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed >= 250*time.Millisecond {
		t.Fatalf("shutdown waited for termination grace after finalization completed: %s", elapsed)
	}
}

// TestShutdownWaitsForTheReconcilerOfAnAlreadyTerminalPersistentSession is the
// regression test for a daemon that returns from Shutdown while durable writes
// are still in flight.
//
// A persistent session's reconciler deliberately outlives its session's
// terminal transition: post-terminal convergence -- a binding update -- is
// retried through the same owner, on a backoff, until it succeeds. Shutdown
// collected only non-terminal sessions and returned as soon as that set was
// empty, and the reconcile context is rooted at context.Background() with
// live.persistentCancel as its only cancellation path. Nothing therefore ever
// cancelled or awaited the reconciler of a session that had already gone
// terminal, so the daemon could release its ownership lease and exit with
// binding writes still outstanding -- which is exactly what the lease exists to
// prevent, because the next daemon may then acquire it and write the same
// durable state concurrently.
func TestShutdownWaitsForTheReconcilerOfAnAlreadyTerminalPersistentSession(t *testing.T) {
	reconcileCtx, cancelReconcile := context.WithCancel(context.Background())
	defer cancelReconcile()
	reconcileDone := make(chan struct{})
	// Stands in for the post-terminal convergence loop, which returns only once
	// its context is cancelled, exactly as runPersistentReconciliation does.
	go func() {
		<-reconcileCtx.Done()
		close(reconcileDone)
	}()

	sessionDone := make(chan struct{})
	close(sessionDone)
	live := &liveSession{
		sessionID:               "s",
		state:                   session.Completed,
		persistent:              true,
		done:                    sessionDone,
		persistentCancel:        cancelReconcile,
		persistentReconcileDone: reconcileDone,
	}
	service := &Service{
		options: Options{TerminationGrace: time.Second},
		live:    map[string]*liveSession{"s": live},
	}

	if err := service.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	select {
	case <-reconcileDone:
	default:
		t.Fatal("Shutdown returned while the reconciler of an already terminal persistent session was still writing")
	}
}
