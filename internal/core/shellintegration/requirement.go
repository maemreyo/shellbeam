package shellintegration

import (
	"fmt"
	"time"
)

type RequirementKind string

const RequirementEnvironmentExportedNonempty RequirementKind = "environment_exported_nonempty"

type Requirement struct {
	Kind RequirementKind `json:"kind"`
	Name string          `json:"name"`
}

func (v Requirement) Validate() error {
	if v.Kind != RequirementEnvironmentExportedNonempty {
		return fmt.Errorf("invalid shell requirement kind")
	}
	if !validEnvironmentName(v.Name) {
		return fmt.Errorf("invalid shell requirement name")
	}
	return nil
}

type RequirementState string

const (
	RequirementSatisfied    RequirementState = "satisfied"
	RequirementNotSatisfied RequirementState = "not_satisfied"
	RequirementUnavailable  RequirementState = "unavailable"
)

func (v RequirementState) Validate() error {
	switch v {
	case RequirementSatisfied, RequirementNotSatisfied, RequirementUnavailable:
		return nil
	default:
		return fmt.Errorf("invalid shell requirement state")
	}
}

type RequirementQuality string

const (
	RequirementQualityExactShellAdapter RequirementQuality = "exact_shell_adapter"
	RequirementQualityManual            RequirementQuality = "manual"
)

func (v RequirementQuality) Validate() error {
	switch v {
	case RequirementQualityExactShellAdapter, RequirementQualityManual:
		return nil
	default:
		return fmt.Errorf("invalid shell requirement quality")
	}
}

type RequirementResult struct {
	Requirement  Requirement        `json:"requirement"`
	State        RequirementState   `json:"state"`
	Quality      RequirementQuality `json:"quality"`
	SafeBoundary bool               `json:"safe_boundary"`
	ObservedAt   time.Time          `json:"observed_at"`
}

func (v RequirementResult) Validate() error {
	if err := v.Requirement.Validate(); err != nil {
		return err
	}
	if err := v.State.Validate(); err != nil {
		return err
	}
	if err := v.Quality.Validate(); err != nil {
		return err
	}
	if v.ObservedAt.IsZero() {
		return fmt.Errorf("shell requirement observation missing")
	}
	if v.State != RequirementUnavailable && !v.SafeBoundary {
		return fmt.Errorf("shell requirement result lacks safe boundary")
	}
	return nil
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
