package interactivehandoff

import (
	"testing"
	"time"

	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
)

func TestWaitUsesEventNotificationAndReturnsHumanOwned(t *testing.T) {
	store, runtime, fencer, svc, _, req := fixture(t)
	// This test exercises concurrent Wait + attach behavior, not call ordering.
	// Disable the shared diagnostic slice so the test harness itself does not
	// race across the waiter and attach goroutines. Ordering tests keep it on.
	store.calls = nil
	runtime.calls = nil
	fencer.calls = nil
	if _, err := svc.Request(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	result := make(chan WaitResult, 1)
	errs := make(chan error, 1)
	go func() {
		got, err := svc.Wait(t.Context(), WaitRequest{HandoffID: req.HandoffID, Yield: time.Second})
		result <- got
		errs <- err
	}()
	time.Sleep(10 * time.Millisecond)
	if _, err := svc.AttachLocalHuman(t.Context(), req.HandoffID, delegatedapp.HumanAttachSpec{}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-result:
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
		if got.TimedOut || got.State.Phase != handoff.PhaseHumanOwned {
			t.Fatalf("wait=%#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("event-driven wait did not wake")
	}
}

func TestWaitTimeoutReturnsCurrentState(t *testing.T) {
	_, _, _, svc, _, req := fixture(t)
	state, err := svc.Request(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.Wait(t.Context(), WaitRequest{HandoffID: req.HandoffID, Yield: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if !got.TimedOut || got.State != state {
		t.Fatalf("wait=%#v state=%#v", got, state)
	}
}
