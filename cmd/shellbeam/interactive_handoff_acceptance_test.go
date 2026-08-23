//go:build darwin

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

func TestInteractiveHandoffNativeDaemonCrashReconcilesFencedPendingAuthority(t *testing.T) {
	tmuxPath := requireDelegatedNativeTmux(t)
	t.Setenv("PATH", filepath.Dir(tmuxPath)+string(os.PathListSeparator)+os.Getenv("PATH"))
	binary := buildB1NativeBinary(t)
	stateDir, runtimeDir := b1NativeDirs(t)
	var providerState delegatedNativeProviderState
	t.Cleanup(func() { killDelegatedNativeProvider(providerState) })

	first := startB1NativeDaemon(t, binary, stateDir, runtimeDir)
	inspect := callB1Native(t, first.client, ipcadapter.RequestV2{Action: "inspect.server"})
	if inspect.Server == nil || inspect.Server.Features["interactive_handoff"] != "available" || inspect.Server.InteractiveHandoff == nil {
		first.hardKill(t)
		t.Fatalf("H2 capability unavailable: %#v", inspect.Server)
	}
	started := callB1NativeDaemon(t, first, ipcadapter.RequestV2{
		Action: "start", OperationID: "h2-task8-native-restart", CWD: "/tmp",
		Command:     "printf 'H2-READY\\n'; IFS= read -r line; printf 'UNEXPECTED:%s\\n' \"$line\"; while :; do sleep 1; done",
		SessionMode: delegated.ModeDelegatedInteractive, StdinMode: operation.StdinModeStream,
		TimeoutMode: operation.TimeoutModeUnlimited, YieldMS: 50, MaxOutputBytes: 8192,
	})
	if started.Result == nil || started.Result.Operation.SessionID == "" {
		first.hardKill(t)
		t.Fatalf("delegated start=%#v", started)
	}
	sessionID := started.Result.Operation.SessionID
	agentEpoch := started.Result.AuthorityEpoch
	waitB1NativeOutputContains(t, first.client, sessionID, "H2-READY")
	_, providerState = loadDelegatedNativeProviderIdentity(t, stateDir, sessionID)

	completion := handoff.Completion{Kind: handoff.CompletionManualReady}
	requested := callB1NativeDaemon(t, first, ipcadapter.RequestV2{
		Action: "handoff.request", HandoffID: "handoff-task8-native-restart", SessionID: sessionID,
		Reason: string(handoff.ReasonManualIntervention), HandoffPrivacy: handoff.PrivacyStandard, HandoffCompletion: &completion,
	})
	if requested.Handoff == nil || requested.Handoff.Status != handoff.StatusHumanConnecting || requested.Handoff.AuthorityEpoch != agentEpoch+1 || requested.Handoff.AgentIngress != handoff.IngressFenced || requested.Handoff.HumanIngress != handoff.IngressFenced {
		first.hardKill(t)
		t.Fatalf("requested handoff=%#v", requested.Handoff)
	}
	handoffEpoch := requested.Handoff.AuthorityEpoch
	assertNativeHandoffWriteRejected(t, first.client, sessionID, handoffEpoch, "before-restart\n")
	first.hardKill(t)

	second := startB1NativeDaemon(t, binary, stateDir, runtimeDir)
	defer second.hardKill(t)
	recovered := callB1Native(t, second.client, ipcadapter.RequestV2{Action: "inspect.handoff", HandoffID: "handoff-task8-native-restart"})
	if recovered.Handoff == nil || recovered.Handoff.Status != handoff.StatusHumanConnecting || recovered.Handoff.AuthorityEpoch != handoffEpoch || recovered.Handoff.AgentIngress != handoff.IngressFenced || recovered.Handoff.HumanIngress != handoff.IngressFenced {
		t.Fatalf("recovered handoff=%#v", recovered.Handoff)
	}
	assertNativeHandoffWriteRejected(t, second.client, sessionID, handoffEpoch, "after-restart\n")
	poll := callB1Native(t, second.client, ipcadapter.RequestV2{Action: "poll", SessionID: sessionID, MaxOutputBytes: 8192})
	if poll.Result == nil || poll.Result.Operation.State != receipt.OperationRunning {
		t.Fatalf("delegated session not running after H2 reconcile: %#v", poll.Result)
	}

	aborted := callB1Native(t, second.client, ipcadapter.RequestV2{Action: "handoff.abort", HandoffID: "handoff-task8-native-restart"})
	if aborted.Handoff == nil || aborted.Handoff.Status != handoff.StatusAborted || aborted.Handoff.FailureCode != "" {
		t.Fatalf("abort=%#v", aborted.Handoff)
	}
	poll = callB1Native(t, second.client, ipcadapter.RequestV2{Action: "poll", SessionID: sessionID, MaxOutputBytes: 8192})
	if poll.Result == nil || poll.Result.Operation.State != receipt.OperationRunning {
		t.Fatalf("abort killed delegated session: %#v", poll.Result)
	}
}

func assertNativeHandoffWriteRejected(t *testing.T, client *ipcadapter.Client, sessionID string, epoch delegated.AuthorityEpoch, chars string) {
	t.Helper()
	resp, err := client.CallV2(context.Background(), ipcadapter.RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "h2-task8-write-rejected-" + chars,
		Action: "write", SessionID: sessionID, AuthorityEpoch: epoch, InputOffset: 0, Chars: chars,
	})
	if err != nil {
		t.Fatalf("write IPC error: %v", err)
	}
	if resp.OK || resp.Error == nil {
		t.Fatalf("human-owned/pending agent write accepted: %#v", resp)
	}
}
