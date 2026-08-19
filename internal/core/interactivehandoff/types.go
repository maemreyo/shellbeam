package interactivehandoff

import (
	"fmt"

	"github.com/maemreyo/shellbeam/internal/core/failure"
)

type Reason string

const (
	ReasonCredentialRequired     Reason = "credential_required"
	ReasonAuthenticationRequired Reason = "authentication_required"
	ReasonHumanConfirmation      Reason = "human_confirmation"
	ReasonComplexInteractiveUI   Reason = "complex_interactive_ui"
	ReasonManualIntervention     Reason = "manual_intervention"
)

func (v Reason) Validate() error {
	switch v {
	case ReasonCredentialRequired, ReasonAuthenticationRequired, ReasonHumanConfirmation, ReasonComplexInteractiveUI, ReasonManualIntervention:
		return nil
	default:
		return fmt.Errorf("invalid handoff reason")
	}
}

type Privacy string

const (
	PrivacyStandard Privacy = "standard"
	PrivacySecret   Privacy = "secret"
)

func (v Privacy) Validate() error {
	switch v {
	case PrivacyStandard, PrivacySecret:
		return nil
	default:
		return fmt.Errorf("invalid handoff privacy")
	}
}

type CompletionKind string

const (
	CompletionEnvironmentExportedNonempty CompletionKind = "environment_exported_nonempty"
	CompletionManualReady                 CompletionKind = "manual_ready"
)

type Completion struct {
	Kind CompletionKind `json:"kind"`
	Name string         `json:"name,omitempty"`
}

func (v Completion) Validate() error {
	switch v.Kind {
	case CompletionManualReady:
		if v.Name != "" {
			return fmt.Errorf("manual ready completion must not name a requirement")
		}
	case CompletionEnvironmentExportedNonempty:
		if !validEnvironmentName(v.Name) {
			return fmt.Errorf("invalid environment completion name")
		}
	default:
		return fmt.Errorf("invalid handoff completion")
	}
	return nil
}

type Request struct {
	HandoffID  string     `json:"handoff_id"`
	SessionID  string     `json:"session_id"`
	Reason     Reason     `json:"reason"`
	Privacy    Privacy    `json:"privacy"`
	Completion Completion `json:"completion"`
}

func (v Request) Validate() error {
	if !validOpaque(v.HandoffID, 128) || !validOpaque(v.SessionID, 128) {
		return failure.New(failure.InvalidInput, map[string]string{"field": "handoff_identity"}, nil)
	}
	if err := v.Reason.Validate(); err != nil {
		return failure.New(failure.InvalidInput, map[string]string{"field": "reason"}, err)
	}
	if err := v.Privacy.Validate(); err != nil {
		return failure.New(failure.InvalidInput, map[string]string{"field": "privacy"}, err)
	}
	if err := v.Completion.Validate(); err != nil {
		return failure.New(failure.InvalidInput, map[string]string{"field": "completion"}, err)
	}
	return nil
}

func (v Request) ValidateH2() error {
	if err := v.Validate(); err != nil {
		return err
	}
	if v.Privacy == PrivacySecret {
		return failure.New(failure.FeatureUnavailable, map[string]string{"feature": "secret_handoff"}, nil)
	}
	if v.Completion.Kind != CompletionManualReady {
		return failure.New(failure.FeatureUnavailable, map[string]string{"feature": "automatic_handoff_completion"}, nil)
	}
	return nil
}

func validOpaque(value string, max int) bool {
	if value == "" || len(value) > max || !isAlphaNumeric(value[0]) {
		return false
	}
	for i := 1; i < len(value); i++ {
		c := value[i]
		if !isAlphaNumeric(c) && c != '_' && c != '-' {
			return false
		}
	}
	return true
}

func validEnvironmentName(value string) bool {
	if value == "" || len(value) > 128 || !(value[0] == '_' || isAlpha(value[0])) {
		return false
	}
	for i := 1; i < len(value); i++ {
		if value[i] != '_' && !isAlphaNumeric(value[i]) {
			return false
		}
	}
	return true
}

func isAlphaNumeric(c byte) bool { return isAlpha(c) || c >= '0' && c <= '9' }
func isAlpha(c byte) bool        { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
