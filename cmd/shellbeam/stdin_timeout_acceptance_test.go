//go:build linux || darwin

package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

func startV2(t *testing.T, client *ipcadapter.Client, req ipcadapter.RequestV2) ipcadapter.ResponseV2 {
	t.Helper()
	req.IPVersion, req.Kind, req.Action = 2, "request", "start"
	if req.RequestID == "" {
		req.RequestID = req.OperationID
	}
	response, err := client.CallV2(context.Background(), req)
	if err != nil {
		t.Fatalf("start %s: %v", req.OperationID, err)
	}
	return response
}

func awaitTerminal(t *testing.T, client *ipcadapter.Client, sessionID string, within time.Duration) *receipt.Result {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		response, err := client.CallV2(context.Background(), ipcadapter.RequestV2{
			IPVersion: 2, Kind: "request", RequestID: "poll-" + sessionID, Action: "poll",
			SessionID: sessionID, MaxOutputBytes: 4096,
		})
		if err == nil && response.Result != nil && response.Result.Operation.State == receipt.OperationTerminal {
			return response.Result
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("session %s did not reach terminal within %s", sessionID, within)
	return nil
}

// TestAccidentalStdinWaitersDoNotConsumeCapacity is the acceptance test for the
// failure that started this work.
//
// Four commands that only wait for input -- the exact shapes an agent produced
// while editing files -- used to take all four session slots and never give
// them back, because stdin stayed open and an omitted timeout meant "run
// forever". The daemon then reported capacity_exceeded as though it were busy,
// while doing nothing at all.
func TestAccidentalStdinWaitersDoNotConsumeCapacity(t *testing.T) {
	stateDir, runtimeDir := a1RuntimeDirs(t)
	client := runA1Daemon(t, stateDir, runtimeDir)

	waiters := []string{
		"cat > " + t.TempDir() + "/one",
		"python3 -",
		"read line",
		"cat",
	}
	for i, command := range waiters {
		operationID := fmt.Sprintf("stdin-waiter-%02d", i)
		started := startV2(t, client, ipcadapter.RequestV2{
			OperationID: operationID, CWD: "/tmp", Command: command,
			YieldMS: 50, MaxOutputBytes: 4096,
		})
		if !started.OK || started.Result == nil {
			t.Fatalf("%s did not start: %#v", operationID, started.Error)
		}
		result := awaitTerminal(t, client, started.Result.Operation.SessionID, 20*time.Second)
		if result.Receipt == nil {
			t.Fatalf("%s produced no receipt", operationID)
		}
		// The receipt has to say policy ended the input, not the child.
		if result.Receipt.StdinMode != string(operation.StdinModeClosed) {
			t.Fatalf("%s receipt stdin_mode = %q, want %q",
				operationID, result.Receipt.StdinMode, operation.StdinModeClosed)
		}
	}

	// Capacity is only proven returned if the daemon still admits work.
	after := startV2(t, client, ipcadapter.RequestV2{
		OperationID: "after-the-waiters", CWD: "/tmp", Command: "true",
		YieldMS: 2000, MaxOutputBytes: 4096,
	})
	if !after.OK || after.Result == nil {
		t.Fatalf("daemon would not admit work after four stdin waiters: %#v", after.Error)
	}
	awaitTerminal(t, client, after.Result.Operation.SessionID, 20*time.Second)
}

// TestOmittedTimeoutBecomesTheOrdinaryBound proves the second half: even a
// command that ignores its input entirely is no longer unbounded by omission.
func TestOmittedTimeoutBecomesTheOrdinaryBound(t *testing.T) {
	stateDir, runtimeDir := a1RuntimeDirs(t)
	client := runA1Daemon(t, stateDir, runtimeDir)

	started := startV2(t, client, ipcadapter.RequestV2{
		OperationID: "omitted-timeout", CWD: "/tmp", Command: "true",
		YieldMS: 2000, MaxOutputBytes: 4096,
	})
	if !started.OK || started.Result == nil {
		t.Fatalf("start: %#v", started.Error)
	}
	result := awaitTerminal(t, client, started.Result.Operation.SessionID, 20*time.Second)
	if result.Receipt.TimeoutMS <= 0 {
		t.Fatalf("omitted timeout left the session unbounded: timeout_ms = %d", result.Receipt.TimeoutMS)
	}
	if result.Receipt.TimeoutSource != "default" {
		t.Fatalf("receipt timeout_source = %q, want %q", result.Receipt.TimeoutSource, "default")
	}
}

// TestExplicitFiniteTimeoutIsReportedAsRequested keeps an explicit bound
// distinguishable from the supplied one.
func TestExplicitFiniteTimeoutIsReportedAsRequested(t *testing.T) {
	stateDir, runtimeDir := a1RuntimeDirs(t)
	client := runA1Daemon(t, stateDir, runtimeDir)

	started := startV2(t, client, ipcadapter.RequestV2{
		OperationID: "explicit-timeout", CWD: "/tmp", Command: "true",
		TimeoutMS: 15000, YieldMS: 2000, MaxOutputBytes: 4096,
	})
	if !started.OK || started.Result == nil {
		t.Fatalf("start: %#v", started.Error)
	}
	result := awaitTerminal(t, client, started.Result.Operation.SessionID, 20*time.Second)
	if result.Receipt.TimeoutMS != 15000 {
		t.Fatalf("receipt timeout_ms = %d, want the requested 15000", result.Receipt.TimeoutMS)
	}
	if result.Receipt.TimeoutSource != "requested" {
		t.Fatalf("receipt timeout_source = %q, want %q", result.Receipt.TimeoutSource, "requested")
	}
}

// TestStreamingSessionStillAcceptsInput keeps the explicit mode usable: this is
// how an agent is meant to write a file now.
func TestStreamingSessionStillAcceptsInput(t *testing.T) {
	stateDir, runtimeDir := a1RuntimeDirs(t)
	client := runA1Daemon(t, stateDir, runtimeDir)
	target := t.TempDir() + "/streamed"

	started := startV2(t, client, ipcadapter.RequestV2{
		OperationID: "streaming-write", CWD: "/tmp", Command: "cat > " + target,
		StdinMode: operation.StdinModeStream, YieldMS: 50, MaxOutputBytes: 4096,
	})
	if !started.OK || started.Result == nil {
		t.Fatalf("start: %#v", started.Error)
	}
	sessionID := started.Result.Operation.SessionID
	for _, request := range []ipcadapter.RequestV2{
		{Action: "write", SessionID: sessionID, Chars: "streamed\n"},
		{Action: "write", SessionID: sessionID, EOF: true, InputOffset: int64(len("streamed\n"))},
	} {
		request.IPVersion, request.Kind, request.RequestID = 2, "request", "write-"+sessionID
		response, err := client.CallV2(context.Background(), request)
		if err != nil || !response.OK {
			t.Fatalf("write to a streaming session: %v %#v", err, response.Error)
		}
	}
	awaitTerminal(t, client, sessionID, 20*time.Second)
}

// TestUnlimitedIsRefusedForOrdinaryWork keeps the unbounded state behind an
// explicit declaration, which is what stops it being reached by accident.
func TestUnlimitedIsRefusedForOrdinaryWork(t *testing.T) {
	stateDir, runtimeDir := a1RuntimeDirs(t)
	client := runA1Daemon(t, stateDir, runtimeDir)

	response := startV2(t, client, ipcadapter.RequestV2{
		OperationID: "ordinary-unlimited", CWD: "/tmp", Command: "true",
		TimeoutMode: operation.TimeoutModeUnlimited, YieldMS: 50, MaxOutputBytes: 4096,
	})
	if response.OK {
		t.Fatal("an ordinary command was granted an unbounded timeout")
	}
}
