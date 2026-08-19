package interactivehandoff

import (
	"fmt"
	"time"

	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
)

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

// PublicStateSchemaVersion is the bounded model-visible handoff projection.
// It intentionally excludes provider generation, provider/client references,
// terminal internals, and any human input bytes.
const PublicStateSchemaVersion = 1

type PublicState struct {
	SchemaVersion    int                      `json:"schema_version"`
	HandoffID        string                   `json:"handoff_id"`
	SessionID        string                   `json:"session_id"`
	AuthorityEpoch   delegated.AuthorityEpoch `json:"authority_epoch"`
	Status           DerivedStatus            `json:"status"`
	AgentIngress     IngressState             `json:"agent_ingress"`
	HumanIngress     IngressState             `json:"human_ingress"`
	TransferBoundary TransferBoundary         `json:"transfer_boundary"`
	Attached         bool                     `json:"attached"`
	FailureCode      string                   `json:"failure_code,omitempty"`
	CreatedAt        *time.Time               `json:"created_at,omitempty"`
	UpdatedAt        *time.Time               `json:"updated_at,omitempty"`
	AttachArgv       []string                 `json:"attach_argv,omitempty"`
}

func ProjectPublicState(v State, createdAt, updatedAt time.Time) (PublicState, error) {
	if err := v.ValidateH2(); err != nil {
		return PublicState{}, err
	}
	if createdAt.IsZero() != updatedAt.IsZero() || (!createdAt.IsZero() && updatedAt.Before(createdAt)) {
		return PublicState{}, fmt.Errorf("invalid public handoff timestamps")
	}
	status := ProjectStatus(v)
	out := PublicState{
		SchemaVersion: PublicStateSchemaVersion,
		HandoffID:     v.HandoffID, SessionID: v.SessionID, AuthorityEpoch: v.AuthorityEpoch,
		Status: status, AgentIngress: v.AgentIngress, HumanIngress: v.HumanIngress,
		TransferBoundary: v.TransferBoundary, Attached: v.HumanClient != nil,
	}
	if !createdAt.IsZero() {
		created, updated := createdAt.UTC(), updatedAt.UTC()
		out.CreatedAt, out.UpdatedAt = &created, &updated
	}
	if status == StatusReclaimBlocked {
		out.FailureCode = string(failure.HandoffReclaimBlocked)
	}
	if v.Phase == PhaseHumanConnecting && v.HumanClient == nil {
		out.AttachArgv = []string{"shellbeam", "session", "attach", "--handoff-id", v.HandoffID}
	}
	return out, nil
}
