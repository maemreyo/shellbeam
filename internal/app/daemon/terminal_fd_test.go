//go:build linux || darwin

package daemon_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	processadapter "github.com/maemreyo/shellbeam/internal/adapter/process"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func openPipeFDs(t *testing.T) int {
	t.Helper()
	out, err := exec.Command("lsof", "-p", strconv.Itoa(os.Getpid())).Output()
	if err != nil {
		t.Skipf("lsof unavailable: %v", err)
	}
	return strings.Count(string(out), "PIPE")
}

// TestFinishedSessionsReturnTheirDescriptorsWithoutBeingAsked is the original
// leak, measured on real processes.
//
// These sessions ask for streaming stdin, so the write end is deliberately not
// closed at spawn: the only thing that can give it back is terminal handling.
// Before this slice nothing did, and a daemon accumulated one descriptor per
// finished command -- three thousand of them, against no live children -- until
// it was restarted.
func TestFinishedSessionsReturnTheirDescriptorsWithoutBeingAsked(t *testing.T) {
	const sessions = 40
	svc := lifecycleService(lifecycleRepository(t), processadapter.Owner{})

	// Warm up once so first-use allocations are not counted as a leak.
	runOrdinarySession(t, svc, "warmup", operation.StdinModeClosed)
	before := openPipeFDs(t)

	for i := 0; i < sessions; i++ {
		runOrdinarySession(t, svc, fmt.Sprintf("fd-op-%03d", i), operation.StdinModeStream)
	}
	waitFor(t, "the live set to drain", 15*time.Second, func() bool { return svc.LiveSessionCount() == 0 })

	after := openPipeFDs(t)
	t.Logf("pipe fds: before=%d after_%d_streaming_sessions=%d", before, sessions, after)
	// One descriptor per finished session is the leak this closes; anything
	// approaching that count means terminal handling is not releasing them.
	if after-before >= sessions {
		t.Fatalf("%d finished sessions retained %d pipe descriptors", sessions, after-before)
	}
}

// runOrdinarySession starts a command that exits on its own and waits for its
// terminal receipt.
func runOrdinarySession(t *testing.T, svc *app.Service, operationID string, stdin operation.StdinMode) {
	t.Helper()
	view, err := svc.Start(context.Background(), app.StartRequest{
		ProtocolVersion: 2, OperationID: operationID, Command: "true", CWD: "/tmp",
		StdinMode: stdin, YieldMS: 5, MaxOutputBytes: 64,
	})
	if err != nil {
		t.Fatalf("start %s: %v", operationID, err)
	}
	waitFor(t, "session "+operationID+" to finish", 15*time.Second, func() bool {
		polled, pollErr := svc.Poll(context.Background(), app.PollRequest{SessionID: view.SessionID, MaxOutputBytes: 64})
		return pollErr == nil && polled.State.Terminal()
	})
}

// TestStreamingSessionsAreStillWritableBeforeTheyFinish keeps the release from
// arriving early: the descriptor has to survive as long as the child does.
func TestStreamingSessionsAreStillWritableBeforeTheyFinish(t *testing.T) {
	svc := lifecycleService(lifecycleRepository(t), processadapter.Owner{})
	view, err := svc.Start(context.Background(), app.StartRequest{
		ProtocolVersion: 2, OperationID: "writable-before-terminal", Command: "cat", CWD: "/tmp",
		StdinMode: operation.StdinModeStream, YieldMS: 5, MaxOutputBytes: 64,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Write(context.Background(), app.WriteRequest{
		SessionID: view.SessionID, Chars: "still open\n",
	}); err != nil {
		t.Fatalf("a running streaming session refused input: %v", err)
	}
	if _, err := svc.Write(context.Background(), app.WriteRequest{
		SessionID: view.SessionID, EOF: true, InputOffset: int64(len("still open\n")),
	}); err != nil {
		t.Fatalf("end of input: %v", err)
	}
	waitFor(t, "the session to finish", 15*time.Second, func() bool { return svc.LiveSessionCount() == 0 })
}
