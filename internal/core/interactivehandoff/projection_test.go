package interactivehandoff

import (
	"testing"

	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
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
