package daemon_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func typedFailure(t *testing.T, err error) *failure.Failure {
	t.Helper()
	var typed *failure.Failure
	if !errors.As(err, &typed) {
		t.Fatalf("error %v is not structured", err)
	}
	return typed
}

// TestCapacityRefusalSaysWhatTheCapacityIs.
//
// A bare capacity_exceeded left a caller unable to tell a daemon that is
// genuinely busy from one whose slots had leaked -- which is exactly the state
// this store spent days in, three of four slots held by sessions waiting on
// input that was never coming. Saying four of four are in use, and roughly when
// to look again, is the difference between waiting and asking for a restart.
func TestCapacityRefusalSaysWhatTheCapacityIs(t *testing.T) {
	svc := lifecycleService(lifecycleRepository(t), nil)
	held := make([]*app.LifecycleHandle, 0, 4)
	for i := 0; i < 4; i++ {
		handle := app.NewLifecycleHandle()
		svc.SetOwner(&app.LifecycleOwner{Handle: handle})
		if _, err := svc.Start(context.Background(), app.StartRequest{
			ProtocolVersion: 2, OperationID: fmt.Sprintf("occupy-%02d", i), Command: "true", CWD: "/tmp", YieldMS: 5,
		}); err != nil {
			t.Fatalf("start %d: %v", i, err)
		}
		held = append(held, handle)
	}
	defer func() {
		for _, handle := range held {
			handle.Reap()
		}
		// Let them finish before the test's temporary state directory is
		// removed; otherwise cleanup races the sessions still writing to it.
		waitFor(t, "the occupying sessions to finish", 10*time.Second, func() bool {
			return svc.LiveSessionCount() == 0
		})
	}()

	handle := app.NewLifecycleHandle()
	svc.SetOwner(&app.LifecycleOwner{Handle: handle})
	_, err := svc.Start(context.Background(), app.StartRequest{
		ProtocolVersion: 2, OperationID: "one-too-many", Command: "true", CWD: "/tmp", YieldMS: 5,
	})
	if err == nil {
		t.Fatal("a fifth session was admitted against a limit of four")
	}
	typed := typedFailure(t, err)
	if typed.Code != failure.CapacityExceeded {
		t.Fatalf("refusal code = %q", typed.Code)
	}
	if !typed.Retryable {
		t.Fatal("a capacity refusal was reported as permanent")
	}
	if typed.Details["active"] != "4" || typed.Details["limit"] != "4" {
		t.Fatalf("refusal did not say what the capacity is: %#v", typed.Details)
	}
	if typed.Details["retry_after_ms"] == "" {
		t.Fatalf("refusal gave no hint when to look again: %#v", typed.Details)
	}
}

// TestPollingAVanishedSessionIsRefusedRatherThanAnswered. Retention collects
// history, so a session a caller remembers may simply be gone. An empty view
// would leave it to guess whether the id was wrong or the record expired.
func TestPollingAVanishedSessionIsRefusedRatherThanAnswered(t *testing.T) {
	repository := lifecycleRepository(t)
	handle := app.NewLifecycleHandle()
	svc := lifecycleService(repository, &app.LifecycleOwner{Handle: handle})

	view, err := svc.Start(context.Background(), app.StartRequest{
		ProtocolVersion: 2, OperationID: "vanishing-op", Command: "true", CWD: "/tmp", YieldMS: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	handle.Reap()
	waitFor(t, "the session to finish", 5*time.Second, func() bool { return svc.LiveSessionCount() == 0 })

	snapshot, err := repository.LoadSession(context.Background(), operation.SessionID(view.SessionID))
	if err != nil {
		t.Fatal(err)
	}
	snapshot.UpdatedAt = time.Now().UTC().Add(-48 * time.Hour)
	if got := repository.AdvanceSession(context.Background(), snapshot); got.Err != nil {
		t.Fatal(got.Err)
	}
	if _, err := repository.CollectExpiredTerminals(context.Background(), storeadapter.RetentionPolicy{
		TerminalRetention: time.Hour,
	}); err != nil {
		t.Fatal(err)
	}

	_, err = svc.Poll(context.Background(), app.PollRequest{SessionID: view.SessionID, MaxOutputBytes: 4096})
	if err == nil {
		t.Fatal("polling a collected session returned an answer instead of refusing")
	}
	if typed := typedFailure(t, err); typed.Code != failure.SessionNotFound {
		t.Fatalf("refusal code = %q, want %q", typed.Code, failure.SessionNotFound)
	}
}

// TestReceiptsSayWhoChoseTheStdinMode, so a caller can tell an input it closed
// from one it never opened.
func TestReceiptsSayWhoChoseTheStdinMode(t *testing.T) {
	for name, request := range map[string]app.StartRequest{
		"policy chose":  {ProtocolVersion: 2, OperationID: "stdin-default", Command: "true", CWD: "/tmp", YieldMS: 5},
		"caller chose":  {ProtocolVersion: 2, OperationID: "stdin-explicit", Command: "true", CWD: "/tmp", YieldMS: 5, StdinMode: operation.StdinModeStream},
		"legacy caller": {ProtocolVersion: 1, OperationID: "stdin-legacy", Command: "true", CWD: "/tmp", YieldMS: 5},
	} {
		want := map[string]string{"policy chose": "default", "caller chose": "requested", "legacy caller": "legacy"}[name]
		t.Run(name, func(t *testing.T) {
			handle := app.NewLifecycleHandle()
			svc := lifecycleService(lifecycleRepository(t), &app.LifecycleOwner{Handle: handle})
			view, err := svc.Start(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			handle.Reap()
			waitFor(t, "the session to finish", 5*time.Second, func() bool { return svc.LiveSessionCount() == 0 })

			polled, err := svc.Poll(context.Background(), app.PollRequest{SessionID: view.SessionID, MaxOutputBytes: 64})
			if err != nil {
				t.Fatal(err)
			}
			if polled.Receipt == nil {
				t.Fatal("no receipt")
			}
			if polled.Receipt.StdinModeSource != want {
				t.Fatalf("stdin_mode_source = %q, want %q", polled.Receipt.StdinModeSource, want)
			}
		})
	}
}
