//go:build darwin

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	contextcore "github.com/maemreyo/shellbeam/internal/core/contextexec"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
	"github.com/maemreyo/shellbeam/internal/core/observation"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	reprocore "github.com/maemreyo/shellbeam/internal/core/repro"
)

const h5CredentialName = "SHELLBEAM_H5_FAKE_CREDENTIAL"

func TestContextExecNativePostSecretWorkflowIsBoundedPrivateAndExactlyOnce(t *testing.T) {
	tmuxPath := requireDelegatedNativeTmux(t)
	t.Setenv("PATH", filepath.Dir(tmuxPath)+string(os.PathListSeparator)+os.Getenv("PATH"))
	if _, err := os.Stat("/bin/zsh"); err != nil {
		t.Skip("/bin/zsh unavailable")
	}

	binary := buildB1NativeBinary(t)
	stateDir, runtimeDir := b1NativeDirs(t)
	cwd := t.TempDir()
	canary := h4NativeCanary(t, "context_exec")
	variants := b1SecretVariants([]byte(canary))
	countPath := filepath.Join(cwd, "doctor-count")
	doctor := writeH5NativeDoctor(t, cwd, countPath, canary)

	var providerState delegatedNativeProviderState
	t.Cleanup(func() { killDelegatedNativeProvider(providerState) })
	daemon := startB1NativeDaemon(t, binary, stateDir, runtimeDir)
	defer daemon.hardKill(t)

	assertH5NativeCapability(t, daemon.client)
	sessionID, epoch := startH5NativeZsh(t, daemon, cwd)
	_, providerState = loadDelegatedNativeProviderIdentity(t, stateDir, sessionID)
	released := completeH5SecretExport(t, daemon, binary, stateDir, runtimeDir, sessionID, epoch, canary)

	req := ipcadapter.RequestV2{
		Action: "context.exec", ContextExecID: "ctxexec-h5-native-post-secret", SessionID: sessionID,
		AuthorityEpoch: released.AuthorityEpoch, Argv: []string{filepath.Base(doctor)}, TimeoutMS: 5000, MaxOutputBytes: 8192,
	}
	first := callH5NativeContextExec(t, daemon.client, req)
	if first.ContextExec == nil || first.ContextExec.Lifecycle != contextcore.LifecycleHelperRequested {
		t.Fatalf("first context exec=%#v", first.ContextExec)
	}
	canonical := waitH5NativeContextExecCanonical(t, daemon.client, runtimeDir, daemon.logPath, req)
	assertH5NativeCanonicalResult(t, canonical, doctor)
	assertH5NativeDerivedSurfacesNoSecret(t, daemon.client, canonical, variants)
	assertH5NativeExactlyOnceReplay(t, daemon.client, req, canonical, countPath)
	assertH5NativeConflict(t, daemon.client, req)

	responseJSON, err := json.Marshal(canonical)
	if err != nil {
		t.Fatal(err)
	}
	assertB1NoSecretVariants(t, "context.exec public response", responseJSON, variants)
	assertB1TreeNoSecret(t, stateDir, "context.exec durable state", variants, "")
	logBytes, _ := os.ReadFile(daemon.logPath)
	assertB1NoSecretVariants(t, "context.exec daemon log", logBytes, variants)
}

func assertH5NativeCapability(t *testing.T, client *ipcadapter.Client) {
	t.Helper()
	inspect := callB1Native(t, client, ipcadapter.RequestV2{Action: "inspect.server"})
	if inspect.Server == nil || inspect.Server.Features[capability.FeatureContextExec] != capability.Available || inspect.Server.ContextExec == nil {
		t.Fatalf("context exec capability unavailable: %#v", inspect.Server)
	}
	if inspect.Server.InteractiveHandoff == nil || !inspect.Server.InteractiveHandoff.Secret || !inspect.Server.InteractiveHandoff.AutomaticReadiness || !inspect.Server.InteractiveHandoff.ValidH4() {
		t.Fatalf("H4 automatic privacy capability unavailable: %#v", inspect.Server.InteractiveHandoff)
	}
	if inspect.Server.ContextExec.ResourceEnforcement != capability.Unavailable || inspect.Server.ContextExec.Hermetic != capability.Unavailable {
		t.Fatalf("context exec overclaims resource/hermetic support: %#v", inspect.Server.ContextExec)
	}
}

func startH5NativeZsh(t *testing.T, daemon *b1NativeDaemon, cwd string) (string, delegated.AuthorityEpoch) {
	t.Helper()
	started := callB1NativeDaemon(t, daemon, ipcadapter.RequestV2{
		Action: "start", OperationID: "h5-task8-native-post-secret", CWD: cwd, Command: "exec /bin/zsh -f",
		SessionMode: delegated.ModeDelegatedInteractive, StdinMode: operation.StdinModeStream,
		TimeoutMode: operation.TimeoutModeUnlimited, YieldMS: 50, MaxOutputBytes: 8192,
	})
	if started.Result == nil || started.Result.Operation.SessionID == "" || started.Result.AuthorityEpoch < 1 {
		t.Fatalf("delegated zsh start=%#v", started)
	}
	time.Sleep(150 * time.Millisecond)
	return started.Result.Operation.SessionID, started.Result.AuthorityEpoch
}

func completeH5SecretExport(t *testing.T, daemon *b1NativeDaemon, binary, stateDir, runtimeDir, sessionID string, epoch delegated.AuthorityEpoch, canary string) handoff.PublicState {
	t.Helper()
	completion := handoff.Completion{Kind: handoff.CompletionEnvironmentExportedNonempty, Name: h5CredentialName}
	requested := callB1NativeDaemon(t, daemon, ipcadapter.RequestV2{
		Action: "handoff.request", HandoffID: "handoff-h5-context-exec-secret", SessionID: sessionID,
		HandoffReason: handoff.ReasonCredentialRequired, HandoffPrivacy: handoff.PrivacySecret, HandoffCompletion: &completion,
	})
	if requested.Handoff == nil || requested.Handoff.PrivacyState != handoff.PrivacyPrivate || requested.Handoff.PrivacyRelease != handoff.PrivacyReleasePending {
		t.Fatalf("secret request=%#v", requested.Handoff)
	}
	home := h4NativeAttachHome(t, stateDir, runtimeDir)
	attach := startH4NativeAttach(t, binary, home, requireDelegatedNativeTmux(t), requested.Handoff.HandoffID)
	human := waitH4NativeHandoffStatus(t, daemon.client, requested.Handoff.HandoffID, handoff.StatusHumanOwned)
	if human.AuthorityEpoch <= epoch || human.CaptureState != handoff.CapturePrivate {
		t.Fatalf("human private state=%#v", human)
	}
	command := fmt.Sprintf("export %s=%s; export PATH=\"$PWD:$PATH\"; printf 'H5_EXPORT_DONE\\n'\n", h5CredentialName, canary)
	if _, err := attach.master.Write([]byte(command)); err != nil {
		t.Fatal(err)
	}
	// Readiness hooks deliberately ignore the first prompt after installation so
	// they cannot satisfy from stale pre-handoff shell state. Synchronize on the
	// private command output before advancing to the evaluating prompt.
	waitH4NativePTYContains(t, attach.master, "H5_EXPORT_DONE")
	if _, err := attach.master.Write([]byte("printf 'H5_BOUNDARY\\n'\n")); err != nil {
		t.Fatal(err)
	}
	waitH4NativePTYContains(t, attach.master, "H5_BOUNDARY")
	return waitH5NativePrivacyReleased(t, daemon.client, runtimeDir, requested.Handoff.HandoffID)
}

func waitH5NativePrivacyReleased(t *testing.T, client *ipcadapter.Client, runtimeDir, handoffID string) handoff.PublicState {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var last *handoff.PublicState
	for time.Now().Before(deadline) {
		resp := callB1Native(t, client, ipcadapter.RequestV2{Action: "inspect.handoff", HandoffID: handoffID})
		last = resp.Handoff
		if resp.Handoff != nil && resp.Handoff.Status == handoff.StatusAgentOwned && resp.Handoff.PrivacyRelease == handoff.PrivacyReleaseProven && resp.Handoff.CaptureState == handoff.CapturePublic {
			return *resp.Handoff
		}
		time.Sleep(25 * time.Millisecond)
	}
	sockets, _ := filepath.Glob(filepath.Join(runtimeDir, ".hn_*.sock"))
	diagnostic := ""
	if raw, err := os.ReadFile(filepath.Join(filepath.Dir(runtimeDir), "daemon.log")); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(line, "SHELLBEAM_H5_READINESS_DIAG ") {
				diagnostic = line
			}
		}
	}
	t.Fatalf("handoff %s did not prove forward-only privacy release; last=%#v readiness_sockets=%v readiness=%q", handoffID, last, sockets, diagnostic)
	return handoff.PublicState{}
}

func writeH5NativeDoctor(t *testing.T, cwd, countPath, canary string) string {
	t.Helper()
	path := filepath.Join(cwd, "fake-doctor")
	sourcePath := filepath.Join(t.TempDir(), "main.go")
	source := fmt.Sprintf(`package main

import (
	"fmt"
	"os"
)

func main() {
	if os.Getenv(%q) != %q {
		os.Exit(41)
	}
	for _, key := range []string{"SHELLBEAM_CONTEXT_EXEC_RUNTIME_DIR", "SHELLBEAM_CONTEXT_EXEC_SOCKET", "SHELLBEAM_CONTEXT_EXEC_CLAIM", "SHELLBEAM_CONTEXT_EXEC_GENERATION", "SHELLBEAM_CONTEXT_EXEC_LAUNCH_ID"} {
		if _, present := os.LookupEnv(key); present {
			os.Exit(44)
		}
	}
	observed, err := os.Stat(".")
	if err != nil {
		os.Exit(42)
	}
	expected, err := os.Stat(%q)
	if err != nil || !os.SameFile(observed, expected) {
		os.Exit(42)
	}
	f, err := os.OpenFile(%q, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		os.Exit(43)
	}
	if _, err := f.WriteString("run\n"); err != nil {
		_ = f.Close()
		os.Exit(43)
	}
	if err := f.Close(); err != nil {
		os.Exit(43)
	}
	fmt.Print("DOCTOR_OK\n")
}
`, h5CredentialName, canary, cwd, countPath)
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "build", "-o", path, sourcePath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build native context doctor: %v\n%s", err, out)
	}
	return path
}

func callH5NativeContextExec(t *testing.T, client *ipcadapter.Client, req ipcadapter.RequestV2) ipcadapter.ResponseV2 {
	t.Helper()
	req.IPVersion, req.Kind = 2, "request"
	req.RequestID = fmt.Sprintf("h5-context-exec-%d", time.Now().UnixNano())
	resp, err := client.CallV2(context.Background(), req)
	if err != nil || !resp.OK {
		t.Fatalf("context.exec ok=%v error=%#v err=%v", resp.OK, resp.Error, err)
	}
	return resp
}

func waitH5NativeContextExecCanonical(t *testing.T, client *ipcadapter.Client, runtimeDir, logPath string, req ipcadapter.RequestV2) contextcore.PublicState {
	t.Helper()
	deadline := time.Now().Add(12 * time.Second)
	var last *contextcore.PublicState
	for time.Now().Before(deadline) {
		resp := callH5NativeContextExec(t, client, req)
		last = resp.ContextExec
		if resp.ContextExec != nil && resp.ContextExec.Lifecycle == contextcore.LifecycleCanonicalized {
			return *resp.ContextExec
		}
		time.Sleep(25 * time.Millisecond)
	}
	sockets, _ := filepath.Glob(filepath.Join(runtimeDir, ".cx-*.sock"))
	_ = logPath
	t.Fatalf("context.exec did not canonicalize; last=%#v helper_sockets=%v", last, sockets)
	return contextcore.PublicState{}
}

func assertH5NativeDerivedSurfacesNoSecret(t *testing.T, client *ipcadapter.Client, canonical contextcore.PublicState, variants [][]byte) {
	t.Helper()
	operationID := canonical.ChildOperationID
	for name, req := range map[string]ipcadapter.RequestV2{
		"event journal": {Action: "inspect.events", Target: &observation.Target{Kind: observation.TargetOperation, OperationID: operationID}, MaxEvents: 64},
		"evidence":      {Action: "inspect.evidence", OperationID: operationID, MaxRecords: 64},
		"telemetry":     {Action: "inspect.telemetry", OperationID: operationID, MaxSamples: 64},
	} {
		response := callB1Native(t, client, req)
		raw, err := json.Marshal(response)
		if err != nil {
			t.Fatal(err)
		}
		assertB1NoSecretVariants(t, "context.exec "+name, raw, variants)
	}

	create, err := client.CallV2(context.Background(), ipcadapter.RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "h5-context-repro-create", Action: "repro.create",
		ReproCreateID: "h5-context-repro-create", OperationID: operationID,
		CapturePolicy: &reprocore.CapturePolicy{DependentDerivations: reprocore.CaptureCurrent},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(create)
	if err != nil {
		t.Fatal(err)
	}
	assertB1NoSecretVariants(t, "context.exec repro create", raw, variants)
	if !create.OK {
		if create.Error == nil || create.Error.Code != "repro_materialization_unavailable" {
			t.Fatalf("unexpected context.exec repro response=%#v", create)
		}
		return
	}
	if create.Capsule == nil || create.Capsule.ReproID == "" {
		t.Fatalf("context.exec repro missing capsule: %#v", create)
	}
	inspected := callB1Native(t, client, ipcadapter.RequestV2{Action: "inspect.repro", ReproID: create.Capsule.ReproID})
	raw, err = json.Marshal(inspected)
	if err != nil {
		t.Fatal(err)
	}
	assertB1NoSecretVariants(t, "context.exec repro inspect", raw, variants)
}

func assertH5NativeCanonicalResult(t *testing.T, got contextcore.PublicState, doctor string) {
	t.Helper()
	if got.ChildOperationID == "" || got.ChildSessionID == "" || got.RequestedExecutable != filepath.Base(doctor) || got.ResolvedExecutable == "" {
		t.Fatalf("context exec identity=%#v", got)
	}
	requestedInfo, err := os.Stat(doctor)
	if err != nil {
		t.Fatal(err)
	}
	resolvedInfo, err := os.Stat(got.ResolvedExecutable)
	if err != nil || !os.SameFile(requestedInfo, resolvedInfo) {
		t.Fatalf("context exec resolved executable identity=%q requested=%q err=%v", got.ResolvedExecutable, doctor, err)
	}
	if got.Spawn == nil || !got.Spawn.Attempted || !got.Spawn.Succeeded || got.Exit == nil || !got.Exit.Reaped || got.Exit.Code == nil || *got.Exit.Code != 0 || got.TimedOut == nil || *got.TimedOut {
		t.Fatalf("context exec terminal evidence=%#v", got)
	}
	if got.Output == nil || got.Output.Preview != "DOCTOR_OK\n" || !got.Output.OutputComplete || got.Output.Attribution != contextcore.OutputAttributionHelperOwnedChildPipes {
		t.Fatalf("context exec output=%#v", got.Output)
	}
	if got.EvidenceAuthority != contextcore.EvidenceAuthorityContextExecChildOwnedV1 || got.EvidenceQuality != contextcore.EvidenceQualityComplete {
		t.Fatalf("context exec evidence quality=%q authority=%q", got.EvidenceQuality, got.EvidenceAuthority)
	}
}

func assertH5NativeExactlyOnceReplay(t *testing.T, client *ipcadapter.Client, req ipcadapter.RequestV2, first contextcore.PublicState, countPath string) {
	t.Helper()
	replay := callH5NativeContextExec(t, client, req)
	if replay.ContextExec == nil || replay.ContextExec.ChildOperationID != first.ChildOperationID || replay.ContextExec.ChildSessionID != first.ChildSessionID || replay.ContextExec.Output == nil || replay.ContextExec.Output.Preview != first.Output.Preview {
		t.Fatalf("context exec replay=%#v first=%#v", replay.ContextExec, first)
	}
	raw, err := os.ReadFile(countPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "run\n" {
		t.Fatalf("context exec duplicated side effect: %q", raw)
	}
}

func assertH5NativeConflict(t *testing.T, client *ipcadapter.Client, req ipcadapter.RequestV2) {
	t.Helper()
	conflict := req
	conflict.Argv = append(append([]string(nil), req.Argv...), "changed")
	conflict.IPVersion, conflict.Kind = 2, "request"
	conflict.RequestID = fmt.Sprintf("h5-context-conflict-%d", time.Now().UnixNano())
	resp, err := client.CallV2(context.Background(), conflict)
	if err != nil {
		t.Fatal(err)
	}
	if resp.OK || resp.Error == nil || resp.Error.Code != string(failure.OperationConflict) {
		t.Fatalf("changed argv under same context_exec_id=%#v", resp)
	}
	text, _ := json.Marshal(resp)
	if strings.Contains(string(text), "doctor-count") {
		t.Fatalf("conflict leaked workload detail: %s", text)
	}
}
