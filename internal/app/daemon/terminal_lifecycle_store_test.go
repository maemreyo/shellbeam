package daemon_test

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

// gatedStore holds terminal publication open, standing in for a store that is
// slow, retrying, or briefly unavailable. It is the only way to observe the
// window in which the two halves of terminal handling are separable.
type gatedStore struct {
	*storeadapter.Repository
	release   chan struct{}
	attempted chan struct{}
	once      sync.Once
}

func (g *gatedStore) PublishTerminal(ctx context.Context, rec receipt.Receipt) app.StoreResult {
	g.once.Do(func() { close(g.attempted) })
	<-g.release
	return g.Repository.PublishTerminal(ctx, rec)
}

func lifecycleRepository(t *testing.T) *storeadapter.Repository {
	t.Helper()
	repository, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{
		MaxSessions: 4, MaxSessionOutput: 1 << 20, MaxTotalState: 1 << 30, ControlReserve: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	return repository
}

func lifecycleService(store app.Store, owner app.ProcessOwner) *app.Service {
	return app.NewService(store, owner, app.Options{
		Incarnation: "lifecycle", Shell: "/bin/sh", MaxQueuedInputBytes: 1024,
		DefaultTimeoutMS: 600000, MaxTimeoutMS: 86400000,
	})
}

func waitFor(t *testing.T, what string, within time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestProcessResourcesAreReleasedBeforeDurabilityAndEvictionAfter is the
// ordering this slice exists to fix, asserted at the one moment the two can be
// told apart: while publication is blocked.
//
// A child's descriptors are dead weight the instant it is reaped, and
// publication retries a failing store for as long as it takes -- so holding
// them across that wait is what turned a stalled store into a descriptor leak.
// The live session is the opposite: it represents the session while it is
// finalizing, and evicting it early would leave a poll reading a terminal state
// from the store with no receipt behind it yet.
func TestProcessResourcesAreReleasedBeforeDurabilityAndEvictionAfter(t *testing.T) {
	handle := app.NewLifecycleHandle()
	gate := &gatedStore{Repository: lifecycleRepository(t), release: make(chan struct{}), attempted: make(chan struct{})}
	svc := lifecycleService(gate, &app.LifecycleOwner{Handle: handle})

	if _, err := svc.Start(context.Background(), app.StartRequest{
		ProtocolVersion: 2, OperationID: "lifecycle-op", Command: "true", CWD: "/tmp", YieldMS: 10,
	}); err != nil {
		t.Fatal(err)
	}
	handle.Reap()

	// Publication is now blocked. The child is gone, so its descriptors must
	// already be back...
	select {
	case <-handle.Closed():
	case <-time.After(5 * time.Second):
		t.Fatal("process resources were still held while terminal publication was blocked")
	}
	<-gate.attempted
	// ...but the session is still finalizing, so it must still be represented.
	if got := svc.LiveSessionCount(); got != 1 {
		t.Fatalf("live sessions while finalizing = %d, want the session still represented", got)
	}

	close(gate.release)
	waitFor(t, "eviction after durable publication", 5*time.Second, func() bool { return svc.LiveSessionCount() == 0 })
}

// TestTerminalSessionIsEvictedAndStillReadable: eviction must not cost a caller
// the answer, because a poll after it falls through to the store.
func TestTerminalSessionIsEvictedAndStillReadable(t *testing.T) {
	handle := app.NewLifecycleHandle()
	svc := lifecycleService(lifecycleRepository(t), &app.LifecycleOwner{Handle: handle})

	view, err := svc.Start(context.Background(), app.StartRequest{
		ProtocolVersion: 2, OperationID: "readable-op", Command: "true", CWD: "/tmp", YieldMS: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	handle.Reap()
	waitFor(t, "eviction", 5*time.Second, func() bool { return svc.LiveSessionCount() == 0 })

	polled, err := svc.Poll(context.Background(), app.PollRequest{SessionID: view.SessionID, MaxOutputBytes: 4096})
	if err != nil {
		t.Fatalf("poll after eviction: %v", err)
	}
	if !polled.State.Terminal() {
		t.Fatalf("state after eviction = %q, want terminal", polled.State)
	}
	if polled.Receipt == nil {
		t.Fatal("poll after eviction returned no receipt; the store is the authority once the session is gone")
	}
	if polled.Receipt.SessionID != view.SessionID {
		t.Fatalf("receipt session = %q, want %q", polled.Receipt.SessionID, view.SessionID)
	}
}

// TestConcurrentPollsAcrossTheTransitionNeverSeeAGap keeps the handover from the
// live session to the store from having a hole in it.
func TestConcurrentPollsAcrossTheTransitionNeverSeeAGap(t *testing.T) {
	handle := app.NewLifecycleHandle()
	svc := lifecycleService(lifecycleRepository(t), &app.LifecycleOwner{Handle: handle})

	view, err := svc.Start(context.Background(), app.StartRequest{
		ProtocolVersion: 2, OperationID: "concurrent-op", Command: "true", CWD: "/tmp", YieldMS: 10,
	})
	if err != nil {
		t.Fatal(err)
	}

	stop := make(chan struct{})
	failures := make(chan error, 64)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if _, pollErr := svc.Poll(context.Background(), app.PollRequest{
					SessionID: view.SessionID, MaxOutputBytes: 4096,
				}); pollErr != nil {
					select {
					case failures <- pollErr:
					default:
					}
					return
				}
			}
		}()
	}
	handle.Reap()
	waitFor(t, "eviction", 5*time.Second, func() bool { return svc.LiveSessionCount() == 0 })
	close(stop)
	wg.Wait()
	close(failures)
	for pollErr := range failures {
		t.Fatalf("a poll across the finalizing to terminal transition failed: %v", pollErr)
	}
}

// TestManyOrdinarySessionsLeaveNothingLive is the shape of the original leak:
// the live set grew with history rather than with work in flight.
func TestManyOrdinarySessionsLeaveNothingLive(t *testing.T) {
	const sessions = 300
	svc := lifecycleService(lifecycleRepository(t), nil)
	peak := 0
	for i := 0; i < sessions; i++ {
		handle := app.NewLifecycleHandle()
		svc.SetOwner(&app.LifecycleOwner{Handle: handle})
		view, err := svc.Start(context.Background(), app.StartRequest{
			ProtocolVersion: 2, OperationID: fmt.Sprintf("soak-op-%04d", i), Command: "true", CWD: "/tmp", YieldMS: 5,
		})
		if err != nil {
			t.Fatalf("start %d: %v", i, err)
		}
		handle.Reap()
		waitFor(t, "session to finish", 5*time.Second, func() bool {
			polled, pollErr := svc.Poll(context.Background(), app.PollRequest{SessionID: view.SessionID, MaxOutputBytes: 16})
			return pollErr == nil && polled.State.Terminal()
		})
		if live := svc.LiveSessionCount(); live > peak {
			peak = live
		}
	}
	waitFor(t, "the live set to drain", 10*time.Second, func() bool { return svc.LiveSessionCount() == 0 })
	// Bounded by work in flight, not by how much work has been done.
	if peak > 4 {
		t.Fatalf("live sessions peaked at %d across %d finished commands", peak, sessions)
	}
}

// TestCapacityReturnsAfterOrdinarySessionsFinish. The limit is four, so the
// full capacity is only demonstrably back if four fresh sessions can all be
// admitted at once after a long sequence has come and gone.
func TestCapacityReturnsAfterOrdinarySessionsFinish(t *testing.T) {
	svc := lifecycleService(lifecycleRepository(t), nil)
	for i := 0; i < 12; i++ {
		handle := app.NewLifecycleHandle()
		svc.SetOwner(&app.LifecycleOwner{Handle: handle})
		view, err := svc.Start(context.Background(), app.StartRequest{
			ProtocolVersion: 2, OperationID: fmt.Sprintf("capacity-op-%02d", i), Command: "true", CWD: "/tmp", YieldMS: 5,
		})
		if err != nil {
			t.Fatalf("start %d: %v", i, err)
		}
		handle.Reap()
		waitFor(t, "session to finish", 5*time.Second, func() bool {
			polled, pollErr := svc.Poll(context.Background(), app.PollRequest{SessionID: view.SessionID, MaxOutputBytes: 16})
			return pollErr == nil && polled.State.Terminal()
		})
	}

	held := make([]*app.LifecycleHandle, 0, 4)
	for i := 0; i < 4; i++ {
		handle := app.NewLifecycleHandle()
		svc.SetOwner(&app.LifecycleOwner{Handle: handle})
		if _, err := svc.Start(context.Background(), app.StartRequest{
			ProtocolVersion: 2, OperationID: fmt.Sprintf("capacity-refill-%02d", i), Command: "true", CWD: "/tmp", YieldMS: 5,
		}); err != nil {
			t.Fatalf("only %d of four slots came back: %v", i, err)
		}
		held = append(held, handle)
	}
	for _, handle := range held {
		handle.Reap()
	}
	waitFor(t, "the live set to drain", 10*time.Second, func() bool { return svc.LiveSessionCount() == 0 })
}
