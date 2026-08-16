package daemon_test

import (
	"context"
	"testing"
	"time"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

// TestPollingACollectedSessionIsAllOrNothing.
//
// Once retention has collected a session, a caller must not be able to see part
// of it: not a receipt without output, not output without a state. The whole
// session leaves view in one step, so what a poll gets back is empty rather
// than half-answered.
//
// What it does not yet get is a reason. An empty view says "there is nothing
// here" without distinguishing "expired" from "never existed", and telling
// those apart needs a failure code this daemon does not have. That belongs to
// the failure taxonomy rather than to retention, and adding one here would mean
// designing that vocabulary in passing.
func TestPollingACollectedSessionIsAllOrNothing(t *testing.T) {
	repository := lifecycleRepository(t)
	handle := app.NewLifecycleHandle()
	svc := lifecycleService(repository, &app.LifecycleOwner{Handle: handle})

	view, err := svc.Start(context.Background(), app.StartRequest{
		ProtocolVersion: 2, OperationID: "collected-op", Command: "true", CWD: "/tmp", YieldMS: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	handle.Reap()
	waitFor(t, "the session to finish", 5*time.Second, func() bool { return svc.LiveSessionCount() == 0 })

	// Age its durable terminal record past the window, then collect it.
	snapshot, err := repository.LoadSession(context.Background(), operation.SessionID(view.SessionID))
	if err != nil {
		t.Fatal(err)
	}
	snapshot.UpdatedAt = time.Now().UTC().Add(-48 * time.Hour)
	if got := repository.AdvanceSession(context.Background(), snapshot); got.Err != nil {
		t.Fatal(got.Err)
	}
	report, err := repository.CollectExpiredTerminals(context.Background(), storeadapter.RetentionPolicy{
		TerminalRetention: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Collected != 1 {
		t.Fatalf("collected %d sessions, want the one that expired", report.Collected)
	}

	polled, err := svc.Poll(context.Background(), app.PollRequest{SessionID: view.SessionID, MaxOutputBytes: 4096})
	if err != nil {
		// A structured refusal is an acceptable answer; a partial one is not.
		return
	}
	if polled.Receipt != nil {
		t.Fatal("a collected session still returned a receipt")
	}
	if polled.State != "" {
		t.Fatalf("a collected session still reported state %q", polled.State)
	}
	if polled.Output != "" {
		t.Fatalf("a collected session still returned output %q", polled.Output)
	}
}
