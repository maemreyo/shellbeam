//go:build linux || darwin

package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	handoffapp "github.com/maemreyo/shellbeam/internal/app/interactivehandoff"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
)

type publicHandoffFakeActions struct {
	fakeActions
	state  handoff.State
	public handoff.PublicState
	calls  []string
}

func (a *publicHandoffFakeActions) RequestHandoffPublic(_ context.Context, req handoff.Request) (handoff.PublicState, error) {
	a.calls = append(a.calls, "request:"+req.HandoffID)
	return a.public, nil
}
func (a *publicHandoffFakeActions) WaitHandoffPublic(_ context.Context, req handoffapp.WaitRequest) (handoff.PublicState, bool, error) {
	a.calls = append(a.calls, "wait:"+req.HandoffID)
	return a.public, true, nil
}
func (a *publicHandoffFakeActions) AbortHandoffPublic(_ context.Context, id string) (handoff.PublicState, error) {
	a.calls = append(a.calls, "abort:"+id)
	return a.public, nil
}
func (a *publicHandoffFakeActions) InspectHandoffPublic(_ context.Context, id string) (handoff.PublicState, error) {
	a.calls = append(a.calls, "inspect:"+id)
	return a.public, nil
}

func publicHandoffState() handoff.State {
	return handoff.State{
		SchemaVersion: handoff.StateSchemaVersion, HandoffID: "handoff_public_1", SessionID: "session_public_1",
		Phase: handoff.PhaseHumanConnecting, AuthorityEpoch: 4, DesiredOwner: delegated.OwnerHuman, ProviderOwner: delegated.OwnerAgent,
		AgentIngress: handoff.IngressFenced, HumanIngress: handoff.IngressFenced,
		TransferBoundary: handoff.TransferBoundary{Kind: handoff.BoundaryProviderOrdered, Established: true},
		PrivacyState:     handoff.PrivacyStateStandard, PrivacyRelease: handoff.PrivacyReleaseNotRequired, CaptureState: handoff.CapturePublic,
		ProviderGeneration: "private_generation_must_not_escape",
	}
}

func TestInteractiveHandoffIPCV2ClosedRequestShapes(t *testing.T) {
	valid := []string{
		`{"ipc_version":2,"kind":"request","request_id":"h1","action":"handoff.request","handoff_id":"handoff_public_1","session_id":"session_public_1","reason":"manual_intervention","privacy":"standard","completion":{"kind":"manual_ready"}}`,
		`{"ipc_version":2,"kind":"request","request_id":"h2","action":"handoff.wait","handoff_id":"handoff_public_1","yield_time_ms":1000}`,
		`{"ipc_version":2,"kind":"request","request_id":"h3","action":"handoff.abort","handoff_id":"handoff_public_1"}`,
		`{"ipc_version":2,"kind":"request","request_id":"h4","action":"inspect.handoff","handoff_id":"handoff_public_1"}`,
	}
	for _, raw := range valid {
		if _, err := decodeRequestV2(strings.NewReader(raw)); err != nil {
			t.Fatalf("valid handoff request rejected: %s: %v", raw, err)
		}
	}
	invalid := []string{
		`{"ipc_version":2,"kind":"request","request_id":"bad","action":"handoff.request","handoff_id":".bad","session_id":"session_public_1","reason":"manual_intervention","privacy":"standard","completion":{"kind":"manual_ready"}}`,
		`{"ipc_version":2,"kind":"request","request_id":"bad","action":"handoff.request","handoff_id":"handoff_public_1","session_id":"session_public_1","reason":"manual_intervention","privacy":"standard","completion":{"kind":"manual_ready","extra":true}}`,
		`{"ipc_version":2,"kind":"request","request_id":"bad","action":"handoff.wait","handoff_id":"handoff_public_1","session_id":"cross_branch"}`,
	}
	for _, raw := range invalid {
		if _, err := decodeRequestV2(strings.NewReader(raw)); !errors.Is(err, failure.InvalidInput) {
			t.Fatalf("invalid handoff shape accepted: %s: %v", raw, err)
		}
	}
	// Future vocabulary is structurally recognized by transport. H2 policy is
	// enforced by the daemon service before provider mutation, not hidden by schema.
	future := []string{
		`{"ipc_version":2,"kind":"request","request_id":"future-secret","action":"handoff.request","handoff_id":"handoff_public_1","session_id":"session_public_1","reason":"credential_required","privacy":"secret","completion":{"kind":"manual_ready"}}`,
		`{"ipc_version":2,"kind":"request","request_id":"future-auto","action":"handoff.request","handoff_id":"handoff_public_1","session_id":"session_public_1","reason":"credential_required","privacy":"standard","completion":{"kind":"environment_exported_nonempty","name":"TOKEN"}}`,
	}
	for _, raw := range future {
		if _, err := decodeRequestV2(strings.NewReader(raw)); err != nil {
			t.Fatalf("future vocabulary rejected structurally: %s: %v", raw, err)
		}
	}
}

func TestInteractiveHandoffIPCV2DispatchesPublicActionsAndProjectsSafeState(t *testing.T) {
	canonical := publicHandoffState()
	created := time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC)
	updated := created.Add(2 * time.Second)
	projected, err := handoff.ProjectPublicState(canonical, created, updated)
	if err != nil {
		t.Fatal(err)
	}
	actions := &publicHandoffFakeActions{state: canonical, public: projected}
	_, client := localHandoffServer(t, actions)
	completion := handoff.Completion{Kind: handoff.CompletionManualReady}
	cases := []RequestV2{
		{IPVersion: 2, Kind: "request", RequestID: "req", Action: "handoff.request", HandoffID: actions.state.HandoffID, SessionID: actions.state.SessionID, HandoffReason: handoff.ReasonManualIntervention, HandoffPrivacy: handoff.PrivacyStandard, HandoffCompletion: &completion},
		{IPVersion: 2, Kind: "request", RequestID: "wait", Action: "handoff.wait", HandoffID: actions.state.HandoffID, YieldMS: int64(time.Second / time.Millisecond)},
		{IPVersion: 2, Kind: "request", RequestID: "abort", Action: "handoff.abort", HandoffID: actions.state.HandoffID},
		{IPVersion: 2, Kind: "request", RequestID: "inspect", Action: "inspect.handoff", HandoffID: actions.state.HandoffID},
	}
	for _, req := range cases {
		resp, err := client.CallV2(t.Context(), req)
		if err != nil || !resp.OK || resp.Handoff == nil {
			t.Fatalf("action=%s resp=%#v err=%v", req.Action, resp, err)
		}
		if resp.Handoff.HandoffID != actions.state.HandoffID || resp.Handoff.SessionID != actions.state.SessionID || resp.Handoff.AuthorityEpoch != actions.state.AuthorityEpoch || resp.Handoff.Status != handoff.StatusHumanConnecting || resp.Handoff.Attached || resp.Handoff.CreatedAt == nil || resp.Handoff.UpdatedAt == nil || !resp.Handoff.CreatedAt.Equal(created) || !resp.Handoff.UpdatedAt.Equal(updated) {
			t.Fatalf("action=%s projection=%#v", req.Action, resp.Handoff)
		}
		if req.Action == "handoff.wait" && !resp.HandoffTimedOut {
			t.Fatal("handoff.wait lost timed_out")
		}
		wire, err := json.Marshal(resp.Handoff)
		if err != nil {
			t.Fatal(err)
		}
		text := string(wire)
		for _, forbidden := range []string{"private_generation_must_not_escape", "provider_generation", "human_client", "client_ref", "tmux", "pane_id", "window_id"} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("unsafe public projection contains %q: %s", forbidden, text)
			}
		}
	}
}
