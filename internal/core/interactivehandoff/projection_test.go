package interactivehandoff

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
)

func TestDerivedStatusRequiresCanonicalAuthorityFacts(t *testing.T) {
	agent := agentOwnedState()
	if got := ProjectStatus(agent); got != StatusAgentOwned {
		t.Fatalf("agent status=%q", got)
	}
	unsafe := agent
	unsafe.ProviderOwner = delegated.OwnerNone
	if got := ProjectStatus(unsafe); got == StatusAgentOwned {
		t.Fatalf("projection granted agent ownership without provider proof: %q", got)
	}

	human := agent
	human.Phase = PhaseHumanOwned
	human.DesiredOwner = delegated.OwnerHuman
	human.ProviderOwner = delegated.OwnerHuman
	human.AgentIngress = IngressFenced
	human.HumanIngress = IngressWritable
	human.HumanClient = &HumanClientRef{Ref: "human-client-1"}
	if got := ProjectStatus(human); got != StatusHumanOwned {
		t.Fatalf("human status=%q", got)
	}
}

func TestDerivedStatusProjectsTransferAndAbortWithoutGrantingAuthority(t *testing.T) {
	base := agentOwnedState()
	for phase, want := range map[Phase]DerivedStatus{
		PhaseAgentFencing:    StatusAgentFencing,
		PhaseHumanConnecting: StatusHumanConnecting,
		PhaseHumanFencing:    StatusHumanFencing,
	} {
		state := base
		state.Phase = phase
		state.AgentIngress = IngressFenced
		state.HumanIngress = IngressFenced
		if got := ProjectStatus(state); got != want {
			t.Fatalf("phase %q status=%q want=%q", phase, got, want)
		}
	}

	blocked := base
	blocked.Phase = PhaseAborted
	blocked.AgentIngress = IngressFenced
	blocked.HumanIngress = IngressFenced
	blocked.DesiredOwner = delegated.OwnerAgent
	blocked.ProviderOwner = delegated.OwnerNone
	blocked.TransferBoundary = TransferBoundary{Kind: BoundaryNone}
	if got := ProjectStatus(blocked); got != StatusReclaimBlocked {
		t.Fatalf("blocked abort status=%q", got)
	}

	aborted := blocked
	aborted.DesiredOwner = delegated.OwnerNone
	if got := ProjectStatus(aborted); got != StatusAborted {
		t.Fatalf("aborted status=%q", got)
	}
}

func TestPublicProjectionCarriesOnlyBoundedHandoffFailureCode(t *testing.T) {
	state := agentOwnedState()
	state.Phase = PhaseReclaimPending
	state.DesiredOwner = delegated.OwnerHuman
	state.ProviderOwner = delegated.OwnerNone
	state.AgentIngress = IngressFenced
	state.HumanIngress = IngressUnknown
	state.TransferBoundary = TransferBoundary{Kind: BoundaryNone}
	state.HumanClient = &HumanClientRef{Ref: "hclient_failure"}
	state.FailureCode = failure.HandoffClientLost
	got, err := ProjectPublicState(state, time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if got.FailureCode != string(failure.HandoffClientLost) {
		t.Fatalf("failure_code=%q", got.FailureCode)
	}
	state.FailureCode = failure.DelegatedProviderMismatch
	if err := state.ValidateH2(); err == nil {
		t.Fatal("unbounded provider failure accepted in canonical handoff state")
	}
}

func TestProjectPublicStateH4ExposesOnlyPrivacyAndCaptureTruth(t *testing.T) {
	state := agentOwnedState()
	state.HandoffID = "handoff-public-h4"
	state.SessionID = "session-public-h4"
	state.PrivacyState = PrivacyPrivate
	state.PrivacyRelease = PrivacyReleasePending
	state.CaptureState = CapturePrivate
	got, err := ProjectPublicState(state, time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if got.PrivacyState != PrivacyPrivate || got.PrivacyRelease != PrivacyReleasePending || got.CaptureState != CapturePrivate {
		t.Fatalf("privacy projection=%#v", got)
	}
	wire, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{state.ProviderGeneration, "human_client", "provider_ref", "secret_value", "terminal_history"} {
		if forbidden != "" && bytes.Contains(wire, []byte(forbidden)) {
			t.Fatalf("public H4 projection leaked %q: %s", forbidden, wire)
		}
	}
}
