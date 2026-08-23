package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	handoffapp "github.com/maemreyo/shellbeam/internal/app/interactivehandoff"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
)

type sessionAttachFakeIPC struct {
	responses []ipcadapter.HandoffLocalResponse
	requests  []ipcadapter.HandoffLocalRequest
	onControl func()
}

func (f *sessionAttachFakeIPC) CallHandoffLocal(_ context.Context, req ipcadapter.HandoffLocalRequest) (ipcadapter.HandoffLocalResponse, error) {
	f.requests = append(f.requests, req)
	if len(f.responses) == 0 {
		return ipcadapter.HandoffLocalResponse{}, fmt.Errorf("unexpected local handoff call")
	}
	out := f.responses[0]
	f.responses = f.responses[1:]
	if req.Action == ipcadapter.HandoffLocalControl && f.onControl != nil {
		f.onControl()
	}
	return out, nil
}

type sessionAttachFakeProvider struct {
	identity delegated.ProviderIdentity
	attach   delegatedapp.HumanAttachResult
	control  handoff.HumanControlKind
	ref      delegated.ProviderRef
	spec     delegatedapp.HumanAttachSpec
	waitSpec delegatedapp.HumanControlSpec
	waitRef  delegated.ProviderRef
	waitCli  delegatedapp.ProviderClientRef
}

func (f *sessionAttachFakeProvider) Identity() delegated.ProviderIdentity { return f.identity }
func (f *sessionAttachFakeProvider) AttachHuman(_ context.Context, ref delegated.ProviderRef, spec delegatedapp.HumanAttachSpec) (delegatedapp.HumanAttachResult, error) {
	f.ref, f.spec = ref, spec
	return f.attach, nil
}
func (f *sessionAttachFakeProvider) WaitWritableHumanControl(_ context.Context, ref delegated.ProviderRef, client delegatedapp.ProviderClientRef, spec delegatedapp.HumanControlSpec) (handoff.HumanControlKind, error) {
	f.waitRef, f.waitCli, f.waitSpec = ref, client, spec
	return f.control, nil
}

func TestSessionAttachUsesOpaqueBootstrapLocalStreamsAndGenerationBoundControl(t *testing.T) {
	now := time.Date(2026, 8, 19, 9, 30, 0, 0, time.UTC)
	identity := delegated.ProviderIdentity{ID: "tmux_control_mode", Version: 1}
	connecting := handoff.State{SchemaVersion: handoff.StateSchemaVersion, HandoffID: "handoff_cli_1", SessionID: "session_cli_1", Phase: handoff.PhaseHumanConnecting, AuthorityEpoch: 7, DesiredOwner: delegated.OwnerHuman, ProviderOwner: delegated.OwnerAgent, AgentIngress: handoff.IngressFenced, HumanIngress: handoff.IngressFenced, TransferBoundary: handoff.TransferBoundary{Kind: handoff.BoundaryProviderOrdered, Established: true}, PrivacyState: handoff.PrivacyStateStandard, PrivacyRelease: handoff.PrivacyReleaseNotRequired, CaptureState: handoff.CapturePublic, ProviderGeneration: "generation_cli"}
	owned := connecting
	owned.Phase = handoff.PhaseHumanOwned
	owned.ProviderOwner = delegated.OwnerHuman
	owned.HumanIngress = handoff.IngressWritable
	owned.HumanClient = &handoff.HumanClientRef{Ref: "hclient_cli_1"}
	agent := owned
	agent.Phase = handoff.PhaseAgentOwned
	agent.AuthorityEpoch = 8
	agent.DesiredOwner = delegated.OwnerAgent
	agent.ProviderOwner = delegated.OwnerAgent
	agent.AgentIngress = handoff.IngressWritable
	agent.HumanIngress = handoff.IngressFenced
	ref := delegated.ProviderRef{SchemaVersion: delegated.ProviderRefSchemaVersion, SessionID: connecting.SessionID, ProviderID: identity.ID, ProviderVersion: identity.Version, Ref: "dtmux_cli_ref", CreatedAt: now, UpdatedAt: now}

	done := make(chan error, 1)
	clientRef := delegatedapp.ProviderClientRef{Ref: "hclient_cli_1"}
	provider := &sessionAttachFakeProvider{identity: identity, attach: delegatedapp.HumanAttachResult{ClientRef: clientRef, ObservedOwner: delegated.OwnerNone, Done: done}, control: handoff.HumanControlReady}
	ipc := &sessionAttachFakeIPC{responses: []ipcadapter.HandoffLocalResponse{
		{LocalVersion: 1, Kind: "response", RequestID: "bootstrap-1", Action: ipcadapter.HandoffLocalBootstrap, OK: true, Bootstrap: &handoffapp.LocalBootstrap{HandoffID: connecting.HandoffID, State: connecting, ProviderRef: ref}},
		{LocalVersion: 1, Kind: "response", RequestID: "bind-1", Action: ipcadapter.HandoffLocalBind, OK: true, State: &owned},
		{LocalVersion: 1, Kind: "response", RequestID: "control-1", Action: ipcadapter.HandoffLocalControl, OK: true, Control: &handoffapp.ControlResult{State: agent, Outcome: "ready"}},
	}}
	ipc.onControl = func() { done <- nil }
	stdin := strings.NewReader("human raw bytes\n")
	var stdout, stderr bytes.Buffer
	ids := []string{"bootstrap-1", "bind-1", "control-1"}
	deps := sessionAttachDeps{
		caller: ipc, provider: provider,
		stdin: stdin, stdout: &stdout, stderr: &stderr,
		environ: []string{"TERM=xterm-test", "SHELLBEAM_ATTACH_SENTINEL=local"},
		newID:   func() string { v := ids[0]; ids = ids[1:]; return v },
	}
	if err := runSessionAttachWith(t.Context(), connecting.HandoffID, deps); err != nil {
		t.Fatal(err)
	}
	if provider.ref != ref || provider.spec.Stdin != io.Reader(stdin) || provider.spec.Stdout != io.Writer(&stdout) || provider.spec.Stderr != io.Writer(&stderr) {
		t.Fatalf("local attach did not receive exact ref/streams: ref=%#v spec=%#v", provider.ref, provider.spec)
	}
	if len(provider.spec.Environment) != 1 || provider.spec.Environment[0] != "TERM=xterm-test" {
		t.Fatalf("attach environment leaked local sentinel: %v", provider.spec.Environment)
	}
	if provider.waitRef != ref || provider.waitCli != clientRef || provider.waitSpec.HandoffID != connecting.HandoffID || provider.waitSpec.AuthorityEpoch != owned.AuthorityEpoch {
		t.Fatalf("wait binding ref=%#v client=%#v spec=%#v", provider.waitRef, provider.waitCli, provider.waitSpec)
	}
	if got := stderr.String(); !strings.Contains(got, "Model-visible output remains public; do not enter secrets here. Secret handoff is unavailable until the privacy capability is present.") {
		t.Fatalf("missing local privacy warning: %q", got)
	}
	if len(ipc.requests) != 3 {
		t.Fatalf("requests=%#v", ipc.requests)
	}
	if ipc.requests[0].Action != ipcadapter.HandoffLocalBootstrap || ipc.requests[0].HandoffID != connecting.HandoffID || ipc.requests[0].ClientRef != "" || ipc.requests[0].ControlID != "" {
		t.Fatalf("bootstrap request=%#v", ipc.requests[0])
	}
	if ipc.requests[1].Action != ipcadapter.HandoffLocalBind || ipc.requests[1].ClientRef != clientRef.Ref {
		t.Fatalf("bind request=%#v", ipc.requests[1])
	}
	control := ipc.requests[2]
	if control.Action != ipcadapter.HandoffLocalControl || control.HandoffID != connecting.HandoffID || control.AuthorityEpoch != owned.AuthorityEpoch || control.ControlID != "hc_ready_control-1" || control.ControlKind != handoff.HumanControlReady || control.ClientRef != "" {
		t.Fatalf("control request=%#v", control)
	}
}

func TestSessionAttachRejectsBootstrapProviderIdentityMismatchBeforeLocalAttach(t *testing.T) {
	now := time.Date(2026, 8, 19, 9, 31, 0, 0, time.UTC)
	state := handoff.State{SchemaVersion: handoff.StateSchemaVersion, HandoffID: "handoff_cli_mismatch", SessionID: "session_cli_mismatch", Phase: handoff.PhaseHumanConnecting, AuthorityEpoch: 2, DesiredOwner: delegated.OwnerHuman, ProviderOwner: delegated.OwnerNone, AgentIngress: handoff.IngressFenced, HumanIngress: handoff.IngressFenced, TransferBoundary: handoff.TransferBoundary{Kind: handoff.BoundaryProviderOrdered, Established: true}, PrivacyState: handoff.PrivacyStateStandard, PrivacyRelease: handoff.PrivacyReleaseNotRequired, CaptureState: handoff.CapturePublic, ProviderGeneration: "generation_mismatch"}
	ref := delegated.ProviderRef{SchemaVersion: delegated.ProviderRefSchemaVersion, SessionID: state.SessionID, ProviderID: "other_provider", ProviderVersion: 9, Ref: "provider_ref_mismatch", CreatedAt: now, UpdatedAt: now}
	ipc := &sessionAttachFakeIPC{responses: []ipcadapter.HandoffLocalResponse{{OK: true, Bootstrap: &handoffapp.LocalBootstrap{HandoffID: state.HandoffID, State: state, ProviderRef: ref}}}}
	provider := &sessionAttachFakeProvider{identity: delegated.ProviderIdentity{ID: "tmux_control_mode", Version: 1}}
	deps := sessionAttachDeps{caller: ipc, provider: provider, stdin: strings.NewReader(""), stdout: io.Discard, stderr: io.Discard, newID: func() string { return "bootstrap-mismatch" }}
	if err := runSessionAttachWith(t.Context(), state.HandoffID, deps); err == nil {
		t.Fatal("provider identity mismatch accepted")
	}
	if provider.ref.Ref != "" {
		t.Fatalf("provider attach attempted with mismatched ref=%#v", provider.ref)
	}
}

func TestSessionAttachRejectsHumanOwnedBootstrapBeforeSpawningAnotherClient(t *testing.T) {
	now := time.Date(2026, 8, 19, 9, 32, 0, 0, time.UTC)
	identity := delegated.ProviderIdentity{ID: "tmux_control_mode", Version: 1}
	state := handoff.State{SchemaVersion: handoff.StateSchemaVersion, HandoffID: "handoff_cli_owned", SessionID: "session_cli_owned", Phase: handoff.PhaseHumanOwned, AuthorityEpoch: 4, DesiredOwner: delegated.OwnerHuman, ProviderOwner: delegated.OwnerHuman, AgentIngress: handoff.IngressFenced, HumanIngress: handoff.IngressWritable, TransferBoundary: handoff.TransferBoundary{Kind: handoff.BoundaryProviderOrdered, Established: true}, PrivacyState: handoff.PrivacyStateStandard, PrivacyRelease: handoff.PrivacyReleaseNotRequired, CaptureState: handoff.CapturePublic, HumanClient: &handoff.HumanClientRef{Ref: "hclient_existing"}, ProviderGeneration: "generation_owned"}
	ref := delegated.ProviderRef{SchemaVersion: delegated.ProviderRefSchemaVersion, SessionID: state.SessionID, ProviderID: identity.ID, ProviderVersion: identity.Version, Ref: "dtmux_owned_ref", CreatedAt: now, UpdatedAt: now}
	ipc := &sessionAttachFakeIPC{responses: []ipcadapter.HandoffLocalResponse{{OK: true, Bootstrap: &handoffapp.LocalBootstrap{HandoffID: state.HandoffID, State: state, ProviderRef: ref}}}}
	provider := &sessionAttachFakeProvider{identity: identity}
	deps := sessionAttachDeps{caller: ipc, provider: provider, stdin: strings.NewReader(""), stdout: io.Discard, stderr: io.Discard, newID: func() string { return "bootstrap-owned" }}
	if err := runSessionAttachWith(t.Context(), state.HandoffID, deps); err == nil {
		t.Fatal("human-owned bootstrap spawned a new attach cycle")
	}
	if provider.ref.Ref != "" {
		t.Fatalf("provider attach attempted for already-owned handoff=%#v", provider.ref)
	}
}

type sessionAttachRetryIPC struct {
	requests []ipcadapter.HandoffLocalRequest
	response ipcadapter.HandoffLocalResponse
}

func (f *sessionAttachRetryIPC) CallHandoffLocal(_ context.Context, req ipcadapter.HandoffLocalRequest) (ipcadapter.HandoffLocalResponse, error) {
	f.requests = append(f.requests, req)
	if len(f.requests) == 1 {
		return ipcadapter.HandoffLocalResponse{}, io.ErrUnexpectedEOF
	}
	return f.response, nil
}

func TestSessionAttachLostControlResponseRetriesExactStableIdentity(t *testing.T) {
	state := handoff.State{HandoffID: "handoff_retry_1", AuthorityEpoch: 9}
	result := handoffapp.ControlResult{State: state, Outcome: "status"}
	caller := &sessionAttachRetryIPC{response: ipcadapter.HandoffLocalResponse{LocalVersion: 1, Kind: "response", RequestID: "ignored-by-fake", Action: ipcadapter.HandoffLocalControl, OK: true, Control: &result}}
	idCalls := 0
	deps := sessionAttachDeps{caller: caller, newID: func() string { idCalls++; return "stable_retry" }}
	resp, err := sendLocalControl(t.Context(), deps, state, handoff.HumanControlStatus)
	if err != nil || resp.Control == nil || resp.Control.Outcome != "status" {
		t.Fatalf("response=%#v err=%v", resp, err)
	}
	if idCalls != 1 || len(caller.requests) != 2 {
		t.Fatalf("id calls=%d requests=%#v", idCalls, caller.requests)
	}
	if caller.requests[0] != caller.requests[1] {
		t.Fatalf("retry changed request identity: first=%#v second=%#v", caller.requests[0], caller.requests[1])
	}
	req := caller.requests[0]
	if req.HandoffID != state.HandoffID || req.AuthorityEpoch != state.AuthorityEpoch || req.ControlID != "hc_status_stable_retry" || req.ControlKind != handoff.HumanControlStatus {
		t.Fatalf("control request=%#v", req)
	}
}

func TestSessionAttachAcceptsH4PrivateBootstrapAndPrintsPrivateWarning(t *testing.T) {
	now := time.Date(2026, 8, 20, 15, 0, 0, 0, time.UTC)
	identity := delegated.ProviderIdentity{ID: "tmux_control_mode", Version: 1}
	connecting := handoff.State{SchemaVersion: handoff.StateSchemaVersion, HandoffID: "handoff_cli_h4", SessionID: "session_cli_h4", Phase: handoff.PhaseHumanConnecting, AuthorityEpoch: 7, DesiredOwner: delegated.OwnerHuman, ProviderOwner: delegated.OwnerAgent, AgentIngress: handoff.IngressFenced, HumanIngress: handoff.IngressFenced, TransferBoundary: handoff.TransferBoundary{Kind: handoff.BoundaryProviderOrdered, Established: true}, PrivacyState: handoff.PrivacyPrivate, PrivacyRelease: handoff.PrivacyReleasePending, CaptureState: handoff.CapturePrivate, ProviderGeneration: "generation_cli_h4"}
	owned := connecting
	owned.Phase = handoff.PhaseHumanOwned
	owned.ProviderOwner = delegated.OwnerHuman
	owned.HumanIngress = handoff.IngressWritable
	owned.HumanClient = &handoff.HumanClientRef{Ref: "hclient_cli_h4"}
	agent := owned
	agent.Phase = handoff.PhaseAgentOwned
	agent.AuthorityEpoch = 8
	agent.DesiredOwner = delegated.OwnerAgent
	agent.ProviderOwner = delegated.OwnerAgent
	agent.AgentIngress = handoff.IngressWritable
	agent.HumanIngress = handoff.IngressFenced
	ref := delegated.ProviderRef{SchemaVersion: delegated.ProviderRefSchemaVersion, SessionID: connecting.SessionID, ProviderID: identity.ID, ProviderVersion: identity.Version, Ref: "dtmux_cli_h4_ref", CreatedAt: now, UpdatedAt: now}

	done := make(chan error, 1)
	clientRef := delegatedapp.ProviderClientRef{Ref: "hclient_cli_h4"}
	provider := &sessionAttachFakeProvider{identity: identity, attach: delegatedapp.HumanAttachResult{ClientRef: clientRef, ObservedOwner: delegated.OwnerNone, Done: done}, control: handoff.HumanControlReady}
	ipc := &sessionAttachFakeIPC{responses: []ipcadapter.HandoffLocalResponse{
		{OK: true, Bootstrap: &handoffapp.LocalBootstrap{HandoffID: connecting.HandoffID, State: connecting, ProviderRef: ref}},
		{OK: true, State: &owned},
		{OK: true, Control: &handoffapp.ControlResult{State: agent, Outcome: "ready"}},
	}}
	ipc.onControl = func() { done <- nil }
	var stderr bytes.Buffer
	ids := []string{"h4-bootstrap", "h4-bind", "h4-ready"}
	deps := sessionAttachDeps{caller: ipc, provider: provider, stdin: strings.NewReader(""), stdout: io.Discard, stderr: &stderr, environ: []string{"TERM=xterm-test"}, newID: func() string { v := ids[0]; ids = ids[1:]; return v }}
	if err := runSessionAttachWith(t.Context(), connecting.HandoffID, deps); err != nil {
		t.Fatal(err)
	}
	if got := stderr.String(); !strings.Contains(got, h4LocalWarning) || strings.Contains(got, "Secret handoff is unavailable") {
		t.Fatalf("H4 private warning=%q", got)
	}
}

func TestLocalAttachEnvironmentPreservesTerminalDescriptionPaths(t *testing.T) {
	got := localAttachEnvironment([]string{
		"TERM=xterm-ghostty",
		"TERMINFO=/Applications/Ghostty.app/Contents/Resources/terminfo",
		"TERMINFO_DIRS=/usr/share/terminfo:/opt/share/terminfo",
		"COLORTERM=truecolor",
		"SHELLBEAM_ATTACH_SENTINEL=private",
	})
	want := []string{
		"TERM=xterm-ghostty",
		"TERMINFO=/Applications/Ghostty.app/Contents/Resources/terminfo",
		"TERMINFO_DIRS=/usr/share/terminfo:/opt/share/terminfo",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("local attach terminal environment=%v want=%v", got, want)
	}
}
