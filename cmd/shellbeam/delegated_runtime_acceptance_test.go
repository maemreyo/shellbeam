//go:build darwin

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

type delegatedNativeProviderState struct {
	Ref                string `json:"ref"`
	SocketPath         string `json:"socket_path"`
	PaneID             string `json:"pane_id"`
	ProviderGeneration string `json:"provider_generation"`
	ServerPID          int    `json:"server_pid"`
	PanePID            int    `json:"pane_pid"`
}

func TestDelegatedRuntimeNativeDaemonHardKillReattachesExactSessionAndFailsClosedOnProviderLoss(t *testing.T) {
	tmuxPath := requireDelegatedNativeTmux(t)
	t.Setenv("PATH", filepath.Dir(tmuxPath)+string(os.PathListSeparator)+os.Getenv("PATH"))
	binary := buildB1NativeBinary(t)
	stateDir, runtimeDir := b1NativeDirs(t)
	var lastProviderState delegatedNativeProviderState
	t.Cleanup(func() { killDelegatedNativeProvider(lastProviderState) })

	first := startB1NativeDaemon(t, binary, stateDir, runtimeDir)
	sessionID, epoch, refBefore, stateBefore := startDelegatedNativeRestartSession(t, first, stateDir)
	lastProviderState = stateBefore
	first.hardKill(t)

	second := startB1NativeDaemon(t, binary, stateDir, runtimeDir)
	defer second.hardKill(t)
	stateAfter, nextOffset := assertDelegatedNativeRestartContinuation(t, second, stateDir, sessionID, epoch, refBefore, stateBefore)
	lastProviderState = stateAfter
	assertDelegatedNativeProviderLoss(t, second, stateDir, sessionID, epoch, nextOffset, stateAfter.ServerPID)
	lastProviderState.ServerPID = 0
}

func requireDelegatedNativeTmux(t *testing.T) string {
	t.Helper()
	tmuxPath := os.Getenv("SHELLBEAM_H0_TMUX")
	if tmuxPath == "" {
		t.Skip("set SHELLBEAM_H0_TMUX to run delegated native restart acceptance")
	}
	if !filepath.IsAbs(tmuxPath) {
		t.Fatalf("SHELLBEAM_H0_TMUX must be absolute: %q", tmuxPath)
	}
	return tmuxPath
}

func startDelegatedNativeRestartSession(t *testing.T, daemon *b1NativeDaemon, stateDir string) (string, delegated.AuthorityEpoch, delegated.ProviderRef, delegatedNativeProviderState) {
	t.Helper()
	inspect := callB1Native(t, daemon.client, ipcadapter.RequestV2{Action: "inspect.server"})
	if inspect.Server == nil || inspect.Server.DelegatedInteractive == nil || inspect.Server.Features["delegated_interactive"] != "available" {
		daemon.hardKill(t)
		t.Fatalf("delegated production capability unavailable: %#v", inspect.Server)
	}
	started := callB1NativeDaemon(t, daemon, ipcadapter.RequestV2{
		Action: "start", OperationID: "h1-task8-native-restart", CWD: "/tmp",
		Command:     "printf 'READY\\n'; IFS= read -r one; printf 'ONE:%s\\n' \"$one\"; IFS= read -r two; printf 'TWO:%s\\n' \"$two\"; while :; do sleep 1; done",
		SessionMode: delegated.ModeDelegatedInteractive, StdinMode: operation.StdinModeStream, TimeoutMode: operation.TimeoutModeUnlimited,
		YieldMS: 50, MaxOutputBytes: 8192,
	})
	if started.Result == nil || started.Result.Operation.SessionID == "" || started.Result.SessionMode != delegated.ModeDelegatedInteractive || started.Result.AuthorityEpoch < 1 || started.Result.Operation.State == receipt.OperationTerminal {
		daemon.hardKill(t)
		t.Fatalf("delegated start=%#v", started)
	}
	sessionID, epoch := started.Result.Operation.SessionID, started.Result.AuthorityEpoch
	waitB1NativeOutputContains(t, daemon.client, sessionID, "READY")
	firstWrite := callB1NativeDaemon(t, daemon, ipcadapter.RequestV2{Action: "write", SessionID: sessionID, AuthorityEpoch: epoch, InputOffset: 0, Chars: "alpha\n"})
	if firstWrite.View == nil || firstWrite.View.AuthorityEpoch != epoch || firstWrite.View.NextInputOffset != int64(len("alpha\n")) {
		daemon.hardKill(t)
		t.Fatalf("first write=%#v", firstWrite)
	}
	waitB1NativeOutputContains(t, daemon.client, sessionID, "ONE:alpha")
	ref, state := loadDelegatedNativeProviderIdentity(t, stateDir, sessionID)
	if state.ServerPID <= 0 || state.PanePID <= 0 || state.ProviderGeneration == "" {
		daemon.hardKill(t)
		t.Fatalf("provider state before restart=%#v", state)
	}
	return sessionID, epoch, ref, state
}

func assertDelegatedNativeRestartContinuation(t *testing.T, daemon *b1NativeDaemon, stateDir, sessionID string, epoch delegated.AuthorityEpoch, refBefore delegated.ProviderRef, stateBefore delegatedNativeProviderState) (delegatedNativeProviderState, int64) {
	t.Helper()
	restarted := callB1Native(t, daemon.client, ipcadapter.RequestV2{Action: "poll", SessionID: sessionID, MaxOutputBytes: 8192})
	if restarted.Result == nil || restarted.Result.Operation.SessionID != sessionID || restarted.Result.Operation.State != receipt.OperationRunning || restarted.Result.SessionMode != delegated.ModeDelegatedInteractive || restarted.Result.AuthorityEpoch != epoch {
		t.Fatalf("restarted=%#v", restarted)
	}
	refAfter, stateAfter := loadDelegatedNativeProviderIdentity(t, stateDir, sessionID)
	if refAfter != refBefore || stateAfter.Ref != stateBefore.Ref || stateAfter.SocketPath != stateBefore.SocketPath || stateAfter.PaneID != stateBefore.PaneID || stateAfter.ProviderGeneration != stateBefore.ProviderGeneration || stateAfter.ServerPID != stateBefore.ServerPID || stateAfter.PanePID != stateBefore.PanePID {
		t.Fatalf("provider identity changed across daemon restart:\nbefore_ref=%#v\nafter_ref=%#v\nbefore_state=%#v\nafter_state=%#v", refBefore, refAfter, stateBefore, stateAfter)
	}
	next := int64(len("alpha\n"))
	continuedWrite := callB1Native(t, daemon.client, ipcadapter.RequestV2{Action: "write", SessionID: sessionID, AuthorityEpoch: epoch, InputOffset: next, Chars: "beta\n"})
	if continuedWrite.View == nil || continuedWrite.View.AuthorityEpoch != epoch || continuedWrite.View.NextInputOffset != next+int64(len("beta\n")) {
		t.Fatalf("continued write=%#v", continuedWrite)
	}
	continued := waitB1NativeOutputContains(t, daemon.client, sessionID, "TWO:beta")
	if !strings.Contains(continued, "ONE:alpha") {
		t.Fatalf("retained output lost across restart: %q", continued)
	}
	return stateAfter, continuedWrite.View.NextInputOffset
}

func assertDelegatedNativeProviderLoss(t *testing.T, daemon *b1NativeDaemon, stateDir, sessionID string, epoch delegated.AuthorityEpoch, nextOffset int64, serverPID int) {
	t.Helper()
	provider, err := os.FindProcess(serverPID)
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	terminal := waitB1NativeTerminal(t, daemon.client, sessionID)
	if terminal.Receipt == nil || terminal.Receipt.State != "abandoned" || terminal.Receipt.Outcome != "ambiguous" || terminal.Receipt.FailureReason != "provider_lost" || terminal.Receipt.OutputComplete || terminal.Receipt.CaptureQuality != receipt.CaptureIncomplete {
		t.Fatalf("provider-loss terminal=%#v", terminal)
	}
	if !containsCaptureReason(terminal.Receipt.CaptureReasons, receipt.CaptureReasonProviderLost) || !containsCaptureReason(terminal.Receipt.CaptureReasons, receipt.CaptureReasonTransportGap) {
		t.Fatalf("provider-loss capture reasons=%v", terminal.Receipt.CaptureReasons)
	}
	postLoss, callErr := daemon.client.CallV2(context.Background(), ipcadapter.RequestV2{IPVersion: 2, Kind: "request", RequestID: "delegated-post-loss-write", Action: "write", SessionID: sessionID, AuthorityEpoch: epoch, InputOffset: nextOffset, Chars: "should-not-deliver\n"})
	if callErr != nil {
		t.Fatalf("post-loss IPC call: %v", callErr)
	}
	if postLoss.OK || postLoss.Error == nil {
		t.Fatalf("post-loss write unexpectedly accepted: %#v", postLoss)
	}
	assertNoDelegatedProviderState(t, stateDir)
}

func killDelegatedNativeProvider(state delegatedNativeProviderState) {
	if state.ServerPID <= 0 {
		return
	}
	if process, err := os.FindProcess(state.ServerPID); err == nil {
		_ = process.Signal(syscall.SIGKILL)
	}
}

func loadDelegatedNativeProviderIdentity(t *testing.T, stateDir, sessionID string) (delegated.ProviderRef, delegatedNativeProviderState) {
	t.Helper()
	refPath := filepath.Join(stateDir, "delegated-sessions", "provider-refs", sessionID+".json")
	raw, err := os.ReadFile(refPath)
	if err != nil {
		t.Fatal(err)
	}
	var ref delegated.ProviderRef
	if err := json.Unmarshal(raw, &ref); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(stateDir, "delegated-tmux", "provider-state", ref.Ref+".json")
	raw, err = os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state delegatedNativeProviderState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	return ref, state
}

func containsCaptureReason(values []receipt.CaptureReason, want receipt.CaptureReason) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func assertNoDelegatedProviderState(t *testing.T, stateDir string) {
	t.Helper()
	dir := filepath.Join(stateDir, "delegated-tmux", "provider-state")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) || (err == nil && len(entries) == 0) {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	entries, _ := os.ReadDir(dir)
	t.Fatalf("delegated provider state recreated/preserved after canonical provider loss: %v", entries)
}
