package shellintegration

import (
	"testing"
	"time"
)

func TestRequirementAllowsOnlyExportedNonemptyEnvironmentName(t *testing.T) {
	valid := Requirement{Kind: RequirementEnvironmentExportedNonempty, Name: "CONTROL_PLANE_API_KEY"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid requirement rejected: %v", err)
	}
	for _, bad := range []Requirement{
		{},
		{Kind: "command", Name: "echo secret"},
		{Kind: RequirementEnvironmentExportedNonempty},
		{Kind: RequirementEnvironmentExportedNonempty, Name: "1SECRET"},
		{Kind: RequirementEnvironmentExportedNonempty, Name: "SECRET-VALUE"},
		{Kind: RequirementEnvironmentExportedNonempty, Name: "$(env)"},
	} {
		if err := bad.Validate(); err == nil {
			t.Fatalf("bad requirement accepted: %#v", bad)
		}
	}
}

func TestRequirementResultCarriesOnlyTypedBooleanStateAndBoundaryFacts(t *testing.T) {
	now := time.Date(2026, 8, 20, 8, 40, 0, 0, time.UTC)
	req := Requirement{Kind: RequirementEnvironmentExportedNonempty, Name: "CONTROL_PLANE_API_KEY"}
	for _, state := range []RequirementState{RequirementSatisfied, RequirementNotSatisfied} {
		result := RequirementResult{Requirement: req, State: state, Quality: RequirementQualityExactShellAdapter, SafeBoundary: true, ObservedAt: now}
		if err := result.Validate(); err != nil {
			t.Fatalf("state %q rejected: %v", state, err)
		}
	}
	unavailable := RequirementResult{Requirement: req, State: RequirementUnavailable, Quality: RequirementQualityExactShellAdapter, SafeBoundary: false, ObservedAt: now}
	if err := unavailable.Validate(); err != nil {
		t.Fatalf("unavailable result rejected: %v", err)
	}
	for _, bad := range []RequirementResult{
		{Requirement: req, State: RequirementSatisfied, Quality: RequirementQualityExactShellAdapter, SafeBoundary: false, ObservedAt: now},
		{Requirement: req, State: "present", Quality: RequirementQualityExactShellAdapter, SafeBoundary: true, ObservedAt: now},
		{Requirement: req, State: RequirementSatisfied, Quality: "guessed", SafeBoundary: true, ObservedAt: now},
		{Requirement: req, State: RequirementSatisfied, Quality: RequirementQualityExactShellAdapter, SafeBoundary: true},
	} {
		if err := bad.Validate(); err == nil {
			t.Fatalf("bad result accepted: %#v", bad)
		}
	}
}
