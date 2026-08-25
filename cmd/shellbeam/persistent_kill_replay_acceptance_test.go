//go:build linux || darwin

package main

import (
	"context"
	"testing"

	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	"github.com/maemreyo/shellbeam/internal/core/failure"
)

func TestB1NativePersistentKillReplaySurvivesTerminalAndDaemonRestart(t *testing.T) {
	binary := buildB1NativeBinary(t)
	stateDir, runtimeDir := b1NativeDirs(t)
	first := startB1NativeDaemon(t, binary, stateDir, runtimeDir)

	started := callB1NativeDaemon(t, first, ipcadapter.RequestV2{
		Action: "start", OperationID: "b1-native-terminal-kill-replay", CWD: "/tmp",
		Argv: []string{"/bin/sleep", "60"}, Persistent: true, SessionName: "native-terminal-kill-replay",
		YieldMS: 20, MaxOutputBytes: 4096,
	})
	if started.Result == nil || started.Result.Operation.SessionID == "" {
		t.Fatalf("persistent start=%#v", started)
	}
	sessionID := started.Result.Operation.SessionID
	firstKill := callB1Native(t, first.client, ipcadapter.RequestV2{Action: "kill", SessionID: sessionID, KillID: "b1-terminal-replay-kill", Signal: "TERM"})
	if firstKill.View == nil || firstKill.View.KillID != "b1-terminal-replay-kill" || !firstKill.View.SignalAttempt.Attempted || !firstKill.View.SignalAttempt.Succeeded {
		t.Fatalf("first kill=%#v", firstKill)
	}
	terminal := waitB1NativeTerminal(t, first.client, sessionID)
	if terminal.Receipt == nil || terminal.Receipt.State != "killed" || terminal.Receipt.Outcome != "killed" {
		t.Fatalf("terminal=%#v", terminal)
	}

	replay := callB1Native(t, first.client, ipcadapter.RequestV2{Action: "kill", SessionID: sessionID, KillID: "b1-terminal-replay-kill", Signal: "TERM"})
	if replay.View == nil || replay.View.KillID != "b1-terminal-replay-kill" || replay.View.State != "killed" || replay.View.Receipt == nil || replay.View.Receipt.State != "killed" || !replay.View.SignalAttempt.Attempted || !replay.View.SignalAttempt.Succeeded {
		t.Fatalf("post-terminal replay=%#v", replay)
	}

	conflictReq := ipcadapter.RequestV2{IPVersion: 2, Kind: "request", RequestID: "b1-terminal-replay-conflict", Action: "kill", SessionID: sessionID, KillID: "b1-terminal-replay-kill", Signal: "KILL"}
	conflict, err := first.client.CallV2(context.Background(), conflictReq)
	if err != nil || conflict.OK || conflict.Error == nil || conflict.Error.Code != string(failure.OperationMetadataConflict) {
		t.Fatalf("post-terminal conflict=%#v err=%v", conflict, err)
	}

	first.hardKill(t)
	second := startB1NativeDaemon(t, binary, stateDir, runtimeDir)
	defer second.hardKill(t)
	restarted := callB1Native(t, second.client, ipcadapter.RequestV2{Action: "kill", SessionID: sessionID, KillID: "b1-terminal-replay-kill", Signal: "TERM"})
	if restarted.View == nil || restarted.View.KillID != "b1-terminal-replay-kill" || restarted.View.State != "killed" || restarted.View.Receipt == nil || restarted.View.Receipt.State != "killed" || !restarted.View.SignalAttempt.Attempted || !restarted.View.SignalAttempt.Succeeded {
		t.Fatalf("post-restart replay=%#v", restarted)
	}
}
