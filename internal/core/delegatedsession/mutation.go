package delegatedsession

import (
	"fmt"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/failure"
)

type MutationKind string

const (
	MutationWrite             MutationKind = "write"
	MutationSignal            MutationKind = "signal"
	MutationKill              MutationKind = "kill"
	MutationResize            MutationKind = "resize"
	MutationTransfer          MutationKind = "transfer"
	MutationHumanControl      MutationKind = "human_control"
	MutationProviderAuthority MutationKind = "provider_authority"
)

func (k MutationKind) Validate() error {
	switch k {
	case MutationWrite, MutationSignal, MutationKill, MutationResize, MutationTransfer, MutationHumanControl, MutationProviderAuthority:
		return nil
	default:
		return fmt.Errorf("invalid delegated mutation kind")
	}
}

type MutationIdentity struct {
	SessionID     string         `json:"session_id"`
	Epoch         AuthorityEpoch `json:"authority_epoch"`
	Kind          MutationKind   `json:"kind"`
	IdempotencyID string         `json:"idempotency_id,omitempty"`
	Offset        int64          `json:"offset"`
	Fingerprint   string         `json:"fingerprint"`
}

func (m MutationIdentity) Validate() error {
	if !validOpaque(m.SessionID, 128) || !validOpaque(m.Fingerprint, 128) {
		return fmt.Errorf("invalid delegated mutation identity")
	}
	if err := m.Epoch.Validate(); err != nil {
		return err
	}
	if err := m.Kind.Validate(); err != nil {
		return err
	}
	if m.Kind == MutationWrite {
		if m.IdempotencyID != "" || m.Offset < 0 {
			return fmt.Errorf("invalid delegated write identity")
		}
		return nil
	}
	if !validOpaque(m.IdempotencyID, 128) || m.Offset != -1 {
		return fmt.Errorf("invalid delegated control identity")
	}
	return nil
}

const MutationRecordSchemaVersion = 1

type MutationState string

const (
	MutationReserved       MutationState = "reserved"
	MutationDelivered      MutationState = "delivered"
	MutationCompleted      MutationState = "completed"
	MutationFailed         MutationState = "failed"
	MutationOutcomeUnknown MutationState = "outcome_unknown"
)

func (s MutationState) Validate() error {
	switch s {
	case MutationReserved, MutationDelivered, MutationCompleted, MutationFailed, MutationOutcomeUnknown:
		return nil
	default:
		return fmt.Errorf("invalid delegated mutation state")
	}
}

type MutationRecord struct {
	SchemaVersion int              `json:"schema_version"`
	Identity      MutationIdentity `json:"identity"`
	State         MutationState    `json:"state"`
	Outcome       string           `json:"outcome,omitempty"`
	CreatedAt     time.Time        `json:"created_at"`
	UpdatedAt     time.Time        `json:"updated_at"`
}

func (r MutationRecord) Validate() error {
	if r.SchemaVersion != MutationRecordSchemaVersion {
		return fmt.Errorf("invalid delegated mutation record schema")
	}
	if err := r.Identity.Validate(); err != nil {
		return err
	}
	if err := r.State.Validate(); err != nil {
		return err
	}
	if r.Outcome != "" && !validOpaque(r.Outcome, 256) {
		return fmt.Errorf("invalid delegated mutation outcome")
	}
	if r.CreatedAt.IsZero() || r.UpdatedAt.IsZero() || r.UpdatedAt.Before(r.CreatedAt) {
		return fmt.Errorf("invalid delegated mutation timestamps")
	}
	return nil
}

type MutationLookup interface {
	LookupMutation(id MutationIdentity) (MutationRecord, bool, error)
}

type MutationContext struct {
	CurrentEpoch AuthorityEpoch
	CurrentOwner Owner
}

type AdmissionAction string

const (
	AdmissionReplay  AdmissionAction = "replay"
	AdmissionReserve AdmissionAction = "reserve"
)

type AdmissionDecision struct {
	Action AdmissionAction `json:"action"`
	Record MutationRecord  `json:"record"`
}

func DecideMutation(known *MutationRecord, incoming MutationIdentity, ctx MutationContext) (AdmissionDecision, error) {
	if known != nil {
		if known.Identity != incoming {
			return AdmissionDecision{}, failure.New(failure.OperationConflict, nil, nil)
		}
		return AdmissionDecision{Action: AdmissionReplay, Record: *known}, nil
	}
	if err := incoming.Validate(); err != nil {
		return AdmissionDecision{}, failure.New(failure.InvalidInput, map[string]string{"field": "mutation"}, err)
	}
	if incoming.Epoch != ctx.CurrentEpoch {
		return AdmissionDecision{}, failure.New(failure.StaleControlGeneration, map[string]string{
			"session_id":     incoming.SessionID,
			"expected_epoch": fmt.Sprint(ctx.CurrentEpoch),
			"current_epoch":  fmt.Sprint(incoming.Epoch),
		}, nil)
	}
	if ctx.CurrentOwner != OwnerAgent {
		return AdmissionDecision{}, failure.New(failure.SessionControlNotOwned, map[string]string{
			"session_id":     incoming.SessionID,
			"owner":          string(ctx.CurrentOwner),
			"required_owner": string(OwnerAgent),
			"current_epoch":  fmt.Sprint(ctx.CurrentEpoch),
		}, nil)
	}
	return AdmissionDecision{Action: AdmissionReserve, Record: MutationRecord{Identity: incoming}}, nil
}
