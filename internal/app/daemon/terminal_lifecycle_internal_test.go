package daemon

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

// A session holds two kinds of resource, and they are given back at different
// moments. The child's descriptors are dead weight the instant it is reaped, so
// they go before the receipt is made durable -- publication retries a failing
// store indefinitely, and a dead child's stdin must not be held open for the
// duration. The live session itself is the opposite: it represents the session
// while it is finalizing, so it survives until the terminal receipt is durable,
// or a poll falling through to the store would find a terminal state with
// nothing behind it.

// LiveSessionCount reports how many sessions the daemon still represents in
// memory. It exists for tests: the live set is the thing terminal lifecycle
// handling is supposed to drain, and there is no other way to observe it.
func (s *Service) LiveSessionCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.live)
}

// LifecycleHandle is a process handle whose reaping and closing tests drive.
type LifecycleHandle struct {
	closes  atomic.Int32
	exit    chan receipt.ExitEvidence
	closed  chan struct{}
	oneShot sync.Once
}

func NewLifecycleHandle() *LifecycleHandle {
	return &LifecycleHandle{exit: make(chan receipt.ExitEvidence, 1), closed: make(chan struct{})}
}

func (h *LifecycleHandle) Write([]byte) error { return nil }
func (h *LifecycleHandle) CloseStdin() error  { return nil }
func (h *LifecycleHandle) Signal(string) receipt.SignalEvidence {
	return receipt.SignalEvidence{Attempted: true, Succeeded: true}
}
func (h *LifecycleHandle) Wait(ctx context.Context) receipt.ExitEvidence {
	select {
	case e := <-h.exit:
		return e
	case <-ctx.Done():
		return receipt.ExitEvidence{}
	}
}
func (h *LifecycleHandle) Close() error {
	h.closes.Add(1)
	h.oneShot.Do(func() { close(h.closed) })
	return nil
}

// Reap makes the child exit.
func (h *LifecycleHandle) Reap() {
	zero := 0
	h.exit <- receipt.ExitEvidence{Reaped: true, Code: &zero}
}

// Closed is signalled once the handle's resources have been released.
func (h *LifecycleHandle) Closed() <-chan struct{} { return h.closed }

// Closes counts how often release was attempted.
func (h *LifecycleHandle) Closes() int32 { return h.closes.Load() }

// LifecycleOwner hands out one prepared handle.
type LifecycleOwner struct{ Handle *LifecycleHandle }

func (o *LifecycleOwner) Start(context.Context, operation.ExecutionSpec, OutputSink) (ProcessHandle, receipt.SpawnEvidence, error) {
	return o.Handle, receipt.SpawnEvidence{Attempted: true, Succeeded: true}, nil
}

// SetOwner swaps the process owner between sessions.
func (s *Service) SetOwner(owner ProcessOwner) { s.owner = owner }

// TestPersistentSessionsAreNotEvictedByOrdinaryTerminalHandling: a persistent
// session stays addressable after a terminal receipt, because its lifecycle
// continues past it.
func TestPersistentSessionsAreNotEvictedByOrdinaryTerminalHandling(t *testing.T) {
	s := &Service{live: map[string]*liveSession{}}
	live := &liveSession{sessionID: "persistent-session", persistent: true, state: session.Completed}
	s.put(live)
	s.evictTerminated(live)
	if s.LiveSessionCount() != 1 {
		t.Fatal("a persistent session was evicted by ordinary terminal handling")
	}

	ordinary := &liveSession{sessionID: "ordinary-session", state: session.Completed}
	s.put(ordinary)
	s.evictTerminated(ordinary)
	if s.get("ordinary-session") != nil {
		t.Fatal("an ordinary terminal session was left live")
	}
	if s.get("persistent-session") == nil {
		t.Fatal("evicting an ordinary session removed the persistent one")
	}
}

// TestReleasingProcessResourcesIsIdempotentAndSkipsPersistent.
func TestReleasingProcessResourcesIsIdempotentAndSkipsPersistent(t *testing.T) {
	s := &Service{live: map[string]*liveSession{}}
	handle := NewLifecycleHandle()
	ordinary := &liveSession{sessionID: "ordinary", handle: handle}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); s.releaseProcessResources(ordinary) }()
	}
	wg.Wait()
	if handle.Closes() < 1 {
		t.Fatal("process resources were never released")
	}

	persistentHandle := NewLifecycleHandle()
	persistent := &liveSession{sessionID: "persistent", handle: persistentHandle, persistent: true}
	s.releaseProcessResources(persistent)
	if persistentHandle.Closes() != 0 {
		t.Fatal("a persistent session's handle was closed by ordinary terminal handling")
	}
}

// TestReleasingWithoutAHandleIsSafe covers the spawn-failure shape, which never
// had one.
func TestReleasingWithoutAHandleIsSafe(t *testing.T) {
	s := &Service{live: map[string]*liveSession{}}
	s.releaseProcessResources(&liveSession{sessionID: "never-spawned"})
}
