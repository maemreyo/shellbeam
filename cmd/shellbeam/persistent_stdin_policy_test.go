//go:build linux || darwin

package main

import (
	"context"
	"testing"
	"time"

	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

// Persistent sessions exist to be written to and to outlive a single call, so
// neither default this slice introduced may reach them: closing their input
// would contradict the request that created them, and an ordinary ten-minute
// bound would kill the long-lived shell an agent is holding on purpose.

func startPersistent(t *testing.T, client *ipcadapter.Client, operationID, command, name string, extra func(*ipcadapter.RequestV2)) ipcadapter.ResponseV2 {
	t.Helper()
	request := ipcadapter.RequestV2{
		IPVersion: 2, Kind: "request", RequestID: operationID, Action: "start",
		OperationID: operationID, CWD: "/tmp", Command: command,
		Persistent: true, SessionName: name, YieldMS: 100, MaxOutputBytes: 4096,
	}
	if extra != nil {
		extra(&request)
	}
	response, err := client.CallV2(context.Background(), request)
	if err != nil {
		t.Fatalf("start persistent %s: %v", operationID, err)
	}
	return response
}

// TestPersistentSessionKeepsWritableStdinByDefault: the session must accept
// input without having to name stdin_mode, because that is what persistent
// means.
func TestPersistentSessionKeepsWritableStdinByDefault(t *testing.T) {
	stateDir, runtimes := ownershipDirs(t, "run-a")
	daemon := launchDaemon(t, stateDir, runtimes[0])
	if !daemon.serving(t) {
		t.Fatalf("daemon never served: %s", daemon.output(t))
	}

	started := startPersistent(t, daemon.client, "persistent-stdin", "cat", "stdin-default", nil)
	if !started.OK || started.Result == nil {
		t.Fatalf("persistent start: %#v", started.Error)
	}
	sessionID := started.Result.Operation.SessionID

	write, err := daemon.client.CallV2(context.Background(), ipcadapter.RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "persistent-write", Action: "write",
		SessionID: sessionID, Chars: "hello\n",
	})
	if err != nil || !write.OK {
		t.Fatalf("persistent session refused input it was created to receive: %v %#v", err, write.Error)
	}
}

// TestPersistentSessionIsNotBoundByTheOrdinaryDefault. The bound is asserted
// from the receipt rather than by waiting it out: ten minutes of test time
// would prove the same thing far more slowly.
func TestPersistentSessionIsNotBoundByTheOrdinaryDefault(t *testing.T) {
	stateDir, runtimes := ownershipDirs(t, "run-a")
	daemon := launchDaemon(t, stateDir, runtimes[0])
	if !daemon.serving(t) {
		t.Fatalf("daemon never served: %s", daemon.output(t))
	}

	started := startPersistent(t, daemon.client, "persistent-timeout", "sleep 30", "timeout-default", nil)
	if !started.OK || started.Result == nil {
		t.Fatalf("persistent start: %#v", started.Error)
	}
	sessionID := started.Result.Operation.SessionID

	kill, err := daemon.client.CallV2(context.Background(), ipcadapter.RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "persistent-kill", Action: "kill",
		SessionID: sessionID, KillID: "persistent-timeout-cleanup", Signal: "KILL",
	})
	if err != nil || !kill.OK {
		t.Fatalf("kill: %v %#v", err, kill.Error)
	}
	result := awaitTerminal(t, daemon.client, sessionID, 20*time.Second)
	if result.Receipt == nil {
		t.Fatal("no receipt")
	}
	if result.Receipt.TimeoutMS != 0 {
		t.Fatalf("persistent session carried the ordinary bound %d", result.Receipt.TimeoutMS)
	}
	if result.Receipt.TimeoutSource != "unlimited" {
		t.Fatalf("receipt timeout_source = %q, want %q", result.Receipt.TimeoutSource, "unlimited")
	}
	if result.Receipt.StdinMode != string(operation.StdinModeStream) {
		t.Fatalf("receipt stdin_mode = %q, want %q", result.Receipt.StdinMode, operation.StdinModeStream)
	}
}

// TestPersistentSessionAcceptsAnExplicitBound keeps the default from becoming a
// rule: a caller may still bound long-lived work.
func TestPersistentSessionAcceptsAnExplicitBound(t *testing.T) {
	stateDir, runtimes := ownershipDirs(t, "run-a")
	daemon := launchDaemon(t, stateDir, runtimes[0])
	if !daemon.serving(t) {
		t.Fatalf("daemon never served: %s", daemon.output(t))
	}

	started := startPersistent(t, daemon.client, "persistent-bounded", "sleep 30", "bounded",
		func(r *ipcadapter.RequestV2) { r.TimeoutMS = 20000 })
	if !started.OK || started.Result == nil {
		t.Fatalf("persistent start: %#v", started.Error)
	}
	sessionID := started.Result.Operation.SessionID

	if _, err := daemon.client.CallV2(context.Background(), ipcadapter.RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "persistent-bounded-kill", Action: "kill",
		SessionID: sessionID, KillID: "persistent-bounded-cleanup", Signal: "KILL",
	}); err != nil {
		t.Fatal(err)
	}
	result := awaitTerminal(t, daemon.client, sessionID, 20*time.Second)
	if result.Receipt.TimeoutMS != 20000 {
		t.Fatalf("receipt timeout_ms = %d, want the requested 20000", result.Receipt.TimeoutMS)
	}
	if result.Receipt.TimeoutSource != "requested" {
		t.Fatalf("receipt timeout_source = %q, want %q", result.Receipt.TimeoutSource, "requested")
	}
}
