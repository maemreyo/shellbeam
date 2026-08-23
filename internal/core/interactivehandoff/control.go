package interactivehandoff

import (
	"fmt"

	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
)

type HumanControlKind string

const (
	HumanControlReady          HumanControlKind = "ready"
	HumanControlAbort          HumanControlKind = "abort"
	HumanControlStatus         HumanControlKind = "status"
	HumanControlResume         HumanControlKind = "resume"
	HumanControlTerminate      HumanControlKind = "terminate"
	HumanControlRequestControl HumanControlKind = "request_control"
)

func (v HumanControlKind) Validate() error {
	switch v {
	case HumanControlReady, HumanControlAbort, HumanControlStatus, HumanControlResume, HumanControlTerminate, HumanControlRequestControl:
		return nil
	default:
		return fmt.Errorf("invalid human control kind")
	}
}

type ControlSignal struct {
	HandoffID      string                   `json:"handoff_id"`
	AuthorityEpoch delegated.AuthorityEpoch `json:"authority_epoch"`
	ControlID      string                   `json:"control_id"`
	Kind           HumanControlKind         `json:"kind"`
}

func (v ControlSignal) Validate() error {
	if !validOpaque(v.HandoffID, 128) || !validOpaque(v.ControlID, 128) {
		return failure.New(failure.InvalidInput, map[string]string{"field": "human_control"}, nil)
	}
	if err := v.AuthorityEpoch.Validate(); err != nil {
		return failure.New(failure.InvalidInput, map[string]string{"field": "authority_epoch"}, err)
	}
	if err := v.Kind.Validate(); err != nil {
		return failure.New(failure.InvalidInput, map[string]string{"field": "human_control_kind"}, err)
	}
	return nil
}

type ControlAction string

const (
	ControlReplay  ControlAction = "replay"
	ControlReserve ControlAction = "reserve"
)

type ControlDecision struct {
	Action ControlAction `json:"action"`
	Signal ControlSignal `json:"signal"`
}

func DecideControl(existing *ControlSignal, incoming ControlSignal, currentEpoch delegated.AuthorityEpoch) (ControlDecision, error) {
	if existing != nil {
		if *existing != incoming {
			return ControlDecision{}, failure.New(failure.HandoffConflict, map[string]string{"handoff_id": incoming.HandoffID, "control_id": incoming.ControlID}, nil)
		}
		return ControlDecision{Action: ControlReplay, Signal: *existing}, nil
	}
	if err := incoming.Validate(); err != nil {
		return ControlDecision{}, err
	}
	if incoming.AuthorityEpoch != currentEpoch {
		return ControlDecision{}, failure.New(failure.StaleControlGeneration, map[string]string{
			"expected_epoch": fmt.Sprint(currentEpoch),
			"current_epoch":  fmt.Sprint(incoming.AuthorityEpoch),
		}, nil)
	}
	return ControlDecision{Action: ControlReserve, Signal: incoming}, nil
}
