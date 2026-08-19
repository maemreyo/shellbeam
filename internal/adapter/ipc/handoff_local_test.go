//go:build linux || darwin

package ipc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	handoffapp "github.com/maemreyo/shellbeam/internal/app/interactivehandoff"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
)

type localHandoffFakeActions struct {
	fakeActions
	bootstrap handoffapp.LocalBootstrap
	bound     handoff.State
	control   handoffapp.ControlResult
	lastBind  delegatedapp.ProviderClientRef
	lastSig   handoff.ControlSignal
}

func (a *localHandoffFakeActions) BootstrapLocalHuman(context.Context, string) (handoffapp.LocalBootstrap, error) {
	return a.bootstrap, nil
}
func (a *localHandoffFakeActions) BindLocalHuman(_ context.Context, _ string, client delegatedapp.ProviderClientRef) (handoff.State, error) {
	a.lastBind = client
	return a.bound, nil
}
func (a *localHandoffFakeActions) HandoffHumanControl(_ context.Context, sig handoff.ControlSignal) (handoffapp.ControlResult, error) {
	a.lastSig = sig
	return a.control, nil
}

func localHandoffServer(t *testing.T, actions Actions) (*Server, *Client) {
	t.Helper()
	runtime, err := os.MkdirTemp("/tmp", "sb-hl-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtime) })
	srv, err := Listen(runtime, actions)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	go func() { _ = srv.Serve() }()
	return srv, NewClient(srv.SocketPath())
}

func TestHandoffLocalBootstrapBindAndControlUsePrivateEndpoint(t *testing.T) {
	now := time.Date(2026, 8, 19, 9, 20, 0, 0, time.UTC)
	state := handoff.State{SchemaVersion: handoff.StateSchemaVersion, HandoffID: "handoff-local-1", SessionID: "session-local-1", Phase: handoff.PhaseHumanConnecting, AuthorityEpoch: 2, DesiredOwner: delegated.OwnerHuman, ProviderOwner: delegated.OwnerAgent, AgentIngress: handoff.IngressFenced, HumanIngress: handoff.IngressFenced, TransferBoundary: handoff.TransferBoundary{Kind: handoff.BoundaryProviderOrdered, Established: true}, PrivacyState: handoff.PrivacyStateStandard, PrivacyRelease: handoff.PrivacyReleaseNotRequired, CaptureState: handoff.CapturePublic, ProviderGeneration: "generation_local"}
	ref := delegated.ProviderRef{SchemaVersion: delegated.ProviderRefSchemaVersion, SessionID: state.SessionID, ProviderID: "tmux_control_mode", ProviderVersion: 1, Ref: "dtmux_local_ref", CreatedAt: now, UpdatedAt: now}
	actions := &localHandoffFakeActions{bootstrap: handoffapp.LocalBootstrap{HandoffID: state.HandoffID, State: state, ProviderRef: ref}, bound: state, control: handoffapp.ControlResult{State: state, Outcome: "status"}}
	_, client := localHandoffServer(t, actions)

	boot, err := client.CallHandoffLocal(t.Context(), HandoffLocalRequest{LocalVersion: 1, Kind: "request", RequestID: "local-bootstrap", Action: HandoffLocalBootstrap, HandoffID: state.HandoffID})
	if err != nil || !boot.OK || boot.Bootstrap == nil || boot.Bootstrap.ProviderRef != ref || boot.Bootstrap.State != state {
		t.Fatalf("bootstrap=%#v err=%v", boot, err)
	}
	clientRef := delegatedapp.ProviderClientRef{Ref: "hclient_local_1"}
	bound, err := client.CallHandoffLocal(t.Context(), HandoffLocalRequest{LocalVersion: 1, Kind: "request", RequestID: "local-bind", Action: HandoffLocalBind, HandoffID: state.HandoffID, ClientRef: clientRef.Ref})
	if err != nil || !bound.OK || bound.State == nil || actions.lastBind != clientRef {
		t.Fatalf("bind=%#v last=%#v err=%v", bound, actions.lastBind, err)
	}
	sig := handoff.ControlSignal{HandoffID: state.HandoffID, AuthorityEpoch: 2, ControlID: "control-local-1", Kind: handoff.HumanControlStatus}
	controlled, err := client.CallHandoffLocal(t.Context(), HandoffLocalRequest{LocalVersion: 1, Kind: "request", RequestID: "local-control", Action: HandoffLocalControl, HandoffID: sig.HandoffID, AuthorityEpoch: sig.AuthorityEpoch, ControlID: sig.ControlID, ControlKind: sig.Kind})
	if err != nil || !controlled.OK || controlled.Control == nil || actions.lastSig != sig {
		t.Fatalf("control=%#v last=%#v err=%v", controlled, actions.lastSig, err)
	}
}

func TestHandoffLocalStrictFieldsAndPublicV2RemainClosed(t *testing.T) {
	actions := &localHandoffFakeActions{}
	_, client := localHandoffServer(t, actions)
	if _, err := client.CallV2(t.Context(), RequestV2{IPVersion: 2, Kind: "request", RequestID: "public-local", Action: "handoff.local.bootstrap"}); !errors.Is(err, failure.InvalidInput) {
		t.Fatalf("public v2 accepted local action: %v", err)
	}
	bad := HandoffLocalRequest{LocalVersion: 1, Kind: "request", RequestID: "bad", Action: HandoffLocalBootstrap, HandoffID: "handoff-local-1", ClientRef: "hclient_must_not_be_allowed"}
	if _, err := client.CallHandoffLocal(t.Context(), bad); !errors.Is(err, failure.InvalidInput) {
		t.Fatalf("bootstrap accepted bind-only field: %v", err)
	}
}

func TestHandoffLocalBootstrapWireContainsNoProviderPrivateTmuxFacts(t *testing.T) {
	now := time.Date(2026, 8, 19, 9, 30, 0, 0, time.UTC)
	state := handoff.State{SchemaVersion: handoff.StateSchemaVersion, HandoffID: "handoff-wire", SessionID: "session-wire", Phase: handoff.PhaseHumanConnecting, AuthorityEpoch: 2, DesiredOwner: delegated.OwnerHuman, ProviderOwner: delegated.OwnerAgent, AgentIngress: handoff.IngressFenced, HumanIngress: handoff.IngressFenced, TransferBoundary: handoff.TransferBoundary{Kind: handoff.BoundaryProviderOrdered, Established: true}, PrivacyState: handoff.PrivacyStateStandard, PrivacyRelease: handoff.PrivacyReleaseNotRequired, CaptureState: handoff.CapturePublic, ProviderGeneration: "generation_wire"}
	ref := delegated.ProviderRef{SchemaVersion: delegated.ProviderRefSchemaVersion, SessionID: state.SessionID, ProviderID: "tmux_control_mode", ProviderVersion: 1, Ref: "dtmux_wire_ref", CreatedAt: now, UpdatedAt: now}
	actions := &localHandoffFakeActions{bootstrap: handoffapp.LocalBootstrap{HandoffID: state.HandoffID, State: state, ProviderRef: ref}}
	_, client := localHandoffServer(t, actions)
	resp, err := client.CallHandoffLocal(t.Context(), HandoffLocalRequest{LocalVersion: 1, Kind: "request", RequestID: "wire", Action: HandoffLocalBootstrap, HandoffID: state.HandoffID})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"socket_path", "client_name", "client_tty", "pane_id", "window_id", "tmux_session", "server_pid", "pane_pid"} {
		if bytes.Contains(raw, []byte(forbidden)) {
			t.Fatalf("private fact %q leaked in %s", forbidden, raw)
		}
	}
}

func TestHandoffLocalRequestBodyIsSmallAndBounded(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), maxHandoffLocalRequestBytes+1)
	if _, err := decodeHandoffLocal(bytes.NewReader(payload)); !errors.Is(err, failure.InvalidInput) {
		t.Fatalf("oversize err=%v", err)
	}
}
