package interactivehandoff

import delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"

type DerivedStatus string

const (
	StatusUnknown         DerivedStatus = "UNKNOWN"
	StatusAgentOwned      DerivedStatus = "AGENT_OWNED"
	StatusHumanOwned      DerivedStatus = "HUMAN_OWNED"
	StatusAgentFencing    DerivedStatus = "AGENT_FENCING"
	StatusHumanConnecting DerivedStatus = "HUMAN_CONNECTING"
	StatusHumanFencing    DerivedStatus = "HUMAN_FENCING"
	StatusReclaimPending  DerivedStatus = "RECLAIM_PENDING"
	StatusReclaimBlocked  DerivedStatus = "RECLAIM_BLOCKED"
	StatusAborted         DerivedStatus = "ABORTED"
)

func ProjectStatus(v State) DerivedStatus {
	if err := v.Validate(); err != nil {
		return StatusUnknown
	}
	switch v.Phase {
	case PhaseAgentOwned:
		if v.DesiredOwner == delegated.OwnerAgent && v.ProviderOwner == delegated.OwnerAgent && v.AgentIngress == IngressWritable && v.HumanIngress == IngressFenced {
			return StatusAgentOwned
		}
	case PhaseHumanOwned:
		if v.DesiredOwner == delegated.OwnerHuman && v.ProviderOwner == delegated.OwnerHuman && v.AgentIngress == IngressFenced && v.HumanIngress == IngressWritable && v.HumanClient != nil {
			return StatusHumanOwned
		}
	case PhaseAgentFencing:
		return StatusAgentFencing
	case PhaseHumanConnecting:
		return StatusHumanConnecting
	case PhaseHumanFencing:
		return StatusHumanFencing
	case PhaseReclaimPending:
		if v.TransferBoundary.Established {
			return StatusReclaimPending
		}
		return StatusReclaimBlocked
	case PhaseAborted:
		if v.AgentIngress == IngressFenced && v.HumanIngress == IngressFenced {
			if v.DesiredOwner == delegated.OwnerAgent && !v.TransferBoundary.Established {
				return StatusReclaimBlocked
			}
			return StatusAborted
		}
	}
	return StatusUnknown
}
