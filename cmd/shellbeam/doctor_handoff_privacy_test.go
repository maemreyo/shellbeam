package main

import (
	"strings"
	"testing"

	control "github.com/maemreyo/shellbeam/internal/app/control"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	receipt "github.com/maemreyo/shellbeam/internal/core/receipt"
	shell "github.com/maemreyo/shellbeam/internal/core/shellintegration"
)

func TestDoctorInteractiveHandoffReportsProviderShellPrivacyLevelsWithoutSensitiveState(t *testing.T) {
	catalog := capability.Baseline(capability.Limits{}).
		WithDelegatedInteractive(capability.DelegatedInteractiveSupport{ProviderID: "tmux_control_mode", ProviderVersion: 1, Platform: "darwin", MaxMutationRecords: 4096, DaemonRestartContinuity: true}).
		WithInteractiveHandoff(capability.InteractiveHandoffSupport{
			ManualStandard: true, Secret: true, AutomaticReadiness: true,
			ShellIntegrations: []capability.ShellIntegrationSupport{{Shell: shell.ShellFish, Level: shell.CapabilityRequirementAware, SafeBoundary: true, EnvironmentExportedNonempty: true}},
			RequirementKinds:  []shell.RequirementKind{shell.RequirementEnvironmentExportedNonempty},
			Privacy:           &capability.HandoffPrivacySupport{SecretPrivateInterval: true, PrivacyReleaseSeparate: true, ObserverTopologyQualified: true},
			CaptureQualities:  []receipt.CaptureQuality{receipt.CaptureComplete, receipt.CapturePartial, receipt.CaptureIncomplete},
		})
	check := doctorInteractiveHandoffCheck(catalog)
	if check.ID != "interactive_handoff" || check.Status != control.Pass {
		t.Fatalf("check=%+v", check)
	}
	for _, want := range []string{"provider=tmux_control_mode", "secret=available", "privacy_topology=qualified", "fish=requirement_aware", "environment_exported_nonempty", "capture=complete,partial,incomplete"} {
		if !strings.Contains(check.Hint, want) && !strings.Contains(check.Message, want) {
			t.Fatalf("doctor output missing %q: %+v", want, check)
		}
	}
	for _, forbidden := range []string{"CONTROL_PLANE_API_KEY", "secret_value", "secret_hash", "human_input", "terminal_history", "provider_generation"} {
		if strings.Contains(check.Hint, forbidden) || strings.Contains(check.Message, forbidden) {
			t.Fatalf("doctor leaked %q: %+v", forbidden, check)
		}
	}
}
