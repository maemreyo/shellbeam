//go:build darwin

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	delegatedtmux "github.com/maemreyo/shellbeam/internal/adapter/delegatedtmux"
	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

const (
	h2ManualHandoffOne = "handoff-task9-manual-one"
	h2ManualHandoffTwo = "handoff-task9-manual-two"
	h2HumanCanary      = "HUMAN_CANARY_TASK9"
	h2AgentAfterReady  = "AGENT_AFTER_READY_TASK9"
)

type h2ManualNative struct {
	store        *storeadapter.Repository
	provider     *delegatedtmux.Provider
	svc          *daemonapp.Service
	providerRoot string
	stateDir     string
}

// TestInteractiveHandoffManualNativeAcceptance proves the H2 product boundary
// with a real private tmux provider and real PTY-attached human clients. Standard
// handoff output is intentionally public; the test only requires control/event
// metadata and the agent input ledger to exclude human input bytes.
func TestInteractiveHandoffManualNativeAcceptance(t *testing.T) {
	m := newH2ManualNative(t)
	started := startH2ManualSession(t, m)
	assertH2UnsupportedPrivacyFailsBeforeHandoff(t, m, started.SessionID)

	firstPublic, firstHuman := enterH2ManualHumanOwnership(t, m, started.SessionID, h2ManualHandoffOne)
	if firstPublic.AttachArgv == nil || strings.Join(firstPublic.AttachArgv, " ") != "shellbeam session attach --handoff-id "+h2ManualHandoffOne {
		t.Fatalf("attach argv=%v", firstPublic.AttachArgv)
	}
	assertH2AgentWriteRejected(t, m.svc, started.SessionID, firstHuman.state.AuthorityEpoch)
	assertH2HumanInputSameShellAndNoAgentLedgerAdvance(t, m, started.SessionID, firstHuman, h2HumanCanary)
	ready := h2SendOOBControl(t, m.provider, firstHuman, handoff.HumanControlReady, []byte("\x1b[24~"))
	firstAgent := applyH2HumanControl(t, m.svc, firstHuman.state, ready, "hc-task9-ready-1")
	if firstAgent.State.Phase != handoff.PhaseAgentOwned || firstAgent.State.AuthorityEpoch != firstHuman.state.AuthorityEpoch+1 || firstAgent.State.AgentIngress != handoff.IngressWritable || firstAgent.State.HumanIngress != handoff.IngressFenced {
		t.Fatalf("ready result=%#v", firstAgent)
	}
	assertH2DuplicateControlReplay(t, m.svc, firstHuman.state, ready, "hc-task9-ready-1", firstAgent)
	assertH2HumanClientReadOnlyStillPresent(t, m.provider, firstHuman)

	agentWrite := daemonapp.WriteRequest{SessionID: started.SessionID, AuthorityEpoch: firstAgent.State.AuthorityEpoch, InputOffset: 0, Chars: h2AgentAfterReady + "\n"}
	written, err := m.svc.Write(t.Context(), agentWrite)
	if err != nil || written.NextInputOffset != int64(len(agentWrite.Chars)) {
		t.Fatalf("agent write=%#v err=%v", written, err)
	}
	waitH1Output(t, m.svc, started.SessionID, "H2_LINE:session_value:"+h2AgentAfterReady)

	_, secondHuman := enterH2ManualHumanOwnership(t, m, started.SessionID, h2ManualHandoffTwo)
	if secondHuman.client == firstHuman.client {
		t.Fatal("second handoff reused opaque client without explicit provider support")
	}
	assertH2HumanClientReadOnlyStillPresent(t, m.provider, firstHuman)
	stale := handoff.ControlSignal{HandoffID: h2ManualHandoffTwo, AuthorityEpoch: firstHuman.state.AuthorityEpoch, ControlID: "hc-task9-stale-ready", Kind: handoff.HumanControlReady}
	if _, err := m.svc.HandoffHumanControl(t.Context(), stale); !errors.Is(err, failure.StaleControlGeneration) {
		t.Fatalf("stale ready err=%v", err)
	}
	abortKind := h2SendOOBControl(t, m.provider, secondHuman, handoff.HumanControlAbort, []byte("\x1b[23~"))
	aborted := applyH2HumanControl(t, m.svc, secondHuman.state, abortKind, "hc-task9-abort-2")
	if aborted.State.Phase != handoff.PhaseAborted || aborted.Outcome != "aborted" {
		t.Fatalf("abort result=%#v", aborted)
	}
	assertH2DuplicateControlReplay(t, m.svc, secondHuman.state, abortKind, "hc-task9-abort-2", aborted)
	poll, err := m.svc.Poll(t.Context(), daemonapp.PollRequest{SessionID: started.SessionID, MaxOutputBytes: 8192})
	if err != nil || poll.State != session.Running {
		t.Fatalf("abort killed delegated session: %#v err=%v", poll, err)
	}
	assertH2ControlBytesDidNotReachPane(t, poll.Output)

	terminate := handoff.ControlSignal{HandoffID: h2ManualHandoffTwo, AuthorityEpoch: aborted.State.AuthorityEpoch, ControlID: "hc-task9-terminate-2", Kind: handoff.HumanControlTerminate}
	terminated, err := m.svc.HandoffHumanControl(t.Context(), terminate)
	if err != nil || terminated.Outcome != "terminated" {
		t.Fatalf("terminate=%#v err=%v", terminated, err)
	}
	terminal := waitH1Terminal(t, m.svc, started.SessionID)
	assertH2TerminalProvenance(t, terminal, len(agentWrite.Chars))
	assertH2MetadataAntiLeak(t, m, started.SessionID, h2HumanCanary)
}

type h2HumanAttach struct {
	master *os.File
	client delegatedapp.ProviderClientRef
	state  handoff.State
	ref    delegated.ProviderRef
}

func newH2ManualNative(t *testing.T) *h2ManualNative {
	t.Helper()
	tmuxPath := requireH1NativeTmux(t)
	stateDir := filepath.Join(t.TempDir(), "state")
	store := openH1Store(t, stateDir)
	providerRoot := filepath.Join(stateDir, "delegated-tmux")
	provider, err := delegatedtmux.New(delegatedtmux.DarwinQualifiedConfig(providerRoot, tmuxPath))
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.Probe(t.Context()); err != nil {
		t.Fatal(err)
	}
	catalog := h1DelegatedCatalog().WithInteractiveHandoff(capability.InteractiveHandoffSupport{ManualStandard: true})
	svc := daemonapp.NewService(store, &h1ImmediateOwner{}, daemonapp.Options{
		Incarnation: "h2-manual-integration", Shell: "/bin/sh", MaxQueuedInputBytes: 1 << 16,
		DelegatedRuntime: provider, Capabilities: catalog,
	})
	return &h2ManualNative{store: store, provider: provider, svc: svc, providerRoot: providerRoot, stateDir: stateDir}
}

func startH2ManualSession(t *testing.T, m *h2ManualNative) daemonapp.View {
	t.Helper()
	command := `stty -echo; export SHELLBEAM_H2_SENTINEL=session_value; printf 'H2_READY:%s\n' "$SHELLBEAM_H2_SENTINEL"; while IFS= read -r line; do printf 'H2_LINE:%s:%s\n' "$SHELLBEAM_H2_SENTINEL" "$line"; done`
	started, err := m.svc.Start(t.Context(), daemonapp.StartRequest{
		ProtocolVersion: 2, OperationID: "h2-task9-manual-session", CWD: "/tmp", Command: command,
		SessionMode: delegated.ModeDelegatedInteractive, StdinMode: operation.StdinModeStream, TimeoutMode: operation.TimeoutModeUnlimited,
		YieldMS: 25, MaxOutputBytes: 8192,
	})
	if err != nil {
		t.Fatal(err)
	}
	if started.State != session.Running || started.AuthorityEpoch != 1 {
		t.Fatalf("started=%#v", started)
	}
	waitH1Output(t, m.svc, started.SessionID, "H2_READY:session_value")
	ref, err := m.store.LoadDelegatedProviderRef(t.Context(), operation.SessionID(started.SessionID))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.provider.Close(context.Background(), ref) })
	return started
}

func assertH2UnsupportedPrivacyFailsBeforeHandoff(t *testing.T, m *h2ManualNative, sessionID string) {
	t.Helper()
	secret := handoff.Request{HandoffID: "handoff-task9-secret-rejected", SessionID: sessionID, Reason: handoff.ReasonCredentialRequired, Privacy: handoff.PrivacySecret, Completion: handoff.Completion{Kind: handoff.CompletionManualReady}}
	if _, err := m.svc.RequestHandoff(t.Context(), secret); !errors.Is(err, failure.FeatureUnavailable) {
		t.Fatalf("secret err=%v", err)
	}
	automatic := handoff.Request{HandoffID: "handoff-task9-auto-rejected", SessionID: sessionID, Reason: handoff.ReasonCredentialRequired, Privacy: handoff.PrivacyStandard, Completion: handoff.Completion{Kind: handoff.CompletionEnvironmentExportedNonempty, Name: "TOKEN"}}
	if _, err := m.svc.RequestHandoff(t.Context(), automatic); !errors.Is(err, failure.FeatureUnavailable) {
		t.Fatalf("automatic err=%v", err)
	}
}

func enterH2ManualHumanOwnership(t *testing.T, m *h2ManualNative, sessionID, handoffID string) (handoff.PublicState, h2HumanAttach) {
	t.Helper()
	req := handoff.Request{HandoffID: handoffID, SessionID: sessionID, Reason: handoff.ReasonManualIntervention, Privacy: handoff.PrivacyStandard, Completion: handoff.Completion{Kind: handoff.CompletionManualReady}}
	public, err := m.svc.RequestHandoffPublic(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if public.Status != handoff.StatusHumanConnecting || public.AgentIngress != handoff.IngressFenced || public.HumanIngress != handoff.IngressFenced {
		t.Fatalf("public=%#v", public)
	}
	boot, err := m.svc.BootstrapLocalHuman(t.Context(), handoffID)
	if err != nil {
		t.Fatal(err)
	}
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	if err := pty.Setsize(slave, &pty.Winsize{Rows: 24, Cols: 100}); err != nil {
		t.Fatal(err)
	}
	attach, err := m.provider.AttachHuman(t.Context(), boot.ProviderRef, delegatedapp.HumanAttachSpec{Stdin: slave, Stdout: slave, Stderr: slave, Environment: append(os.Environ(), "SHELLBEAM_H2_SENTINEL=attach_value")})
	_ = slave.Close()
	if err != nil {
		_ = master.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = master.Close() })
	owned, err := m.svc.BindLocalHuman(t.Context(), handoffID, attach.ClientRef)
	if err != nil {
		t.Fatal(err)
	}
	if owned.Phase != handoff.PhaseHumanOwned || owned.ProviderOwner != delegated.OwnerHuman || owned.HumanIngress != handoff.IngressWritable {
		t.Fatalf("owned=%#v", owned)
	}
	return public, h2HumanAttach{master: master, client: attach.ClientRef, state: owned, ref: boot.ProviderRef}
}

func assertH2AgentWriteRejected(t *testing.T, svc *daemonapp.Service, sessionID string, epoch delegated.AuthorityEpoch) {
	t.Helper()
	if _, err := svc.Write(t.Context(), daemonapp.WriteRequest{SessionID: sessionID, AuthorityEpoch: epoch, InputOffset: 0, Chars: "AGENT_MUST_NOT_RUN\n"}); !errors.Is(err, failure.SessionControlNotOwned) {
		t.Fatalf("human-owned agent write err=%v", err)
	}
}

func assertH2HumanInputSameShellAndNoAgentLedgerAdvance(t *testing.T, m *h2ManualNative, sessionID string, human h2HumanAttach, canary string) {
	t.Helper()
	if _, err := human.master.Write([]byte(canary + "\n")); err != nil {
		t.Fatal(err)
	}
	view := waitH1Output(t, m.svc, sessionID, "H2_LINE:session_value:"+canary)
	if view.NextInputOffset != 0 {
		t.Fatalf("human input advanced agent input ledger: %d", view.NextInputOffset)
	}
}

func h2SendOOBControl(t *testing.T, provider *delegatedtmux.Provider, human h2HumanAttach, want handoff.HumanControlKind, key []byte) handoff.HumanControlKind {
	t.Helper()
	spec := delegatedapp.HumanControlSpec{HandoffID: human.state.HandoffID, AuthorityEpoch: human.state.AuthorityEpoch}
	result := make(chan struct {
		kind handoff.HumanControlKind
		err  error
	}, 1)
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
	defer cancel()
	go func() {
		kind, err := provider.WaitWritableHumanControl(ctx, human.ref, human.client, spec)
		result <- struct {
			kind handoff.HumanControlKind
			err  error
		}{kind, err}
	}()
	if _, err := human.master.Write(key); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-result:
		if got.err != nil || got.kind != want {
			t.Fatalf("control=%q err=%v want=%q", got.kind, got.err, want)
		}
		return got.kind
	case <-time.After(4 * time.Second):
		t.Fatalf("control %q not observed", want)
		return ""
	}
}

func applyH2HumanControl(t *testing.T, svc *daemonapp.Service, state handoff.State, kind handoff.HumanControlKind, controlID string) handoffappControlResult {
	t.Helper()
	got, err := svc.HandoffHumanControl(t.Context(), handoff.ControlSignal{HandoffID: state.HandoffID, AuthorityEpoch: state.AuthorityEpoch, ControlID: controlID, Kind: kind})
	if err != nil {
		t.Fatal(err)
	}
	return handoffappControlResult{State: got.State, Outcome: got.Outcome}
}

type handoffappControlResult struct {
	State   handoff.State
	Outcome string
}

func assertH2DuplicateControlReplay(t *testing.T, svc *daemonapp.Service, state handoff.State, kind handoff.HumanControlKind, controlID string, want handoffappControlResult) {
	t.Helper()
	got := applyH2HumanControl(t, svc, state, kind, controlID)
	if !reflect.DeepEqual(got.State, want.State) || got.Outcome != want.Outcome {
		t.Fatalf("duplicate control=%#v want=%#v", got, want)
	}
}

func assertH2HumanClientReadOnlyStillPresent(t *testing.T, provider *delegatedtmux.Provider, human h2HumanAttach) {
	t.Helper()
	obs, err := provider.InspectHumanClient(t.Context(), human.ref, human.client)
	if err != nil {
		t.Fatal(err)
	}
	if !obs.Present || !obs.ReadOnly || obs.ObservedOwner != delegated.OwnerNone {
		t.Fatalf("human client=%#v", obs)
	}
}

func assertH2ControlBytesDidNotReachPane(t *testing.T, output string) {
	t.Helper()
	for _, seq := range []string{"\x1b[23~", "\x1b[24~"} {
		if strings.Contains(output, seq) {
			t.Fatalf("OOB control reached pane: %q", output)
		}
	}
}

func assertH2TerminalProvenance(t *testing.T, terminal daemonapp.View, agentInputBytes int) {
	t.Helper()
	if terminal.Receipt == nil {
		t.Fatal("terminal receipt missing")
	}
	if terminal.Receipt.InputAuthorityProvenance != receipt.InputAuthorityHumanWriteGranted {
		t.Fatalf("provenance=%q", terminal.Receipt.InputAuthorityProvenance)
	}
	if terminal.Receipt.InputAcceptedBytes != int64(agentInputBytes) || terminal.Receipt.InputDeliveredBytes != int64(agentInputBytes) {
		t.Fatalf("agent input accounting accepted=%d delivered=%d want=%d", terminal.Receipt.InputAcceptedBytes, terminal.Receipt.InputDeliveredBytes, agentInputBytes)
	}
}

func assertH2MetadataAntiLeak(t *testing.T, m *h2ManualNative, sessionID, canary string) {
	t.Helper()
	provenance, err := m.store.LoadInputAuthorityProvenance(t.Context(), operation.SessionID(sessionID))
	if err != nil || provenance != receipt.InputAuthorityHumanWriteGranted {
		t.Fatalf("durable provenance=%q err=%v", provenance, err)
	}
	items, err := m.store.ListObservationObligations(t.Context(), 0, 512)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(canary)) {
		t.Fatalf("human canary leaked into control/event metadata: %s", raw)
	}
	for _, root := range []string{
		filepath.Join(m.stateDir, "interactive-handoffs"),
		filepath.Join(m.stateDir, "delegated-sessions", "provenance"),
	} {
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			raw, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			if bytes.Contains(raw, []byte(canary)) {
				t.Fatalf("human canary leaked into durable handoff metadata %s: %s", path, raw)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
