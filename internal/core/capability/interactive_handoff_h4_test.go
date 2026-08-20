package capability

import (
	"reflect"
	"testing"

	receipt "github.com/maemreyo/shellbeam/internal/core/receipt"
	shell "github.com/maemreyo/shellbeam/internal/core/shellintegration"
)

func h4Support() InteractiveHandoffSupport {
	return InteractiveHandoffSupport{
		ManualStandard: true, Secret: true, AutomaticReadiness: true,
		ShellIntegrations: []ShellIntegrationSupport{
			{Shell: shell.ShellFish, Level: shell.CapabilityRequirementAware, SafeBoundary: true, EnvironmentExportedNonempty: true},
			{Shell: shell.ShellZsh, Level: shell.CapabilityRequirementAware, SafeBoundary: true, EnvironmentExportedNonempty: true},
			{Shell: shell.ShellBash, Level: shell.CapabilityRequirementAware, SafeBoundary: true, EnvironmentExportedNonempty: true},
		},
		RequirementKinds: []shell.RequirementKind{shell.RequirementEnvironmentExportedNonempty},
		Privacy:          &HandoffPrivacySupport{SecretPrivateInterval: true, PrivacyReleaseSeparate: true, ObserverTopologyQualified: true, HumanInputPersisted: false},
		CaptureQualities: []receipt.CaptureQuality{receipt.CaptureComplete, receipt.CapturePartial, receipt.CaptureIncomplete},
	}
}

func TestInteractiveHandoffH4SupportIsClosedCloneSafeAndRequiresDelegatedProvider(t *testing.T) {
	base := Baseline(Limits{})
	if got := base.WithInteractiveHandoff(h4Support()); got.InteractiveHandoff != nil {
		t.Fatalf("H4 advertised without delegated provider: %#v", got.InteractiveHandoff)
	}
	h1 := base.WithDelegatedInteractive(DelegatedInteractiveSupport{ProviderID: "tmux_control_mode", ProviderVersion: 1, Platform: "darwin", MaxMutationRecords: 4096})
	got := h1.WithInteractiveHandoff(h4Support())
	if got.InteractiveHandoff == nil || !got.InteractiveHandoff.Secret || !got.InteractiveHandoff.AutomaticReadiness || !got.InteractiveHandoff.ValidH4() {
		t.Fatalf("H4 capability=%#v", got.InteractiveHandoff)
	}
	clone := got.Clone()
	clone.InteractiveHandoff.ShellIntegrations[0].Shell = shell.ShellUnknown
	clone.InteractiveHandoff.RequirementKinds[0] = "bad"
	clone.InteractiveHandoff.CaptureQualities[0] = "bad"
	if reflect.DeepEqual(clone.InteractiveHandoff, got.InteractiveHandoff) {
		t.Fatal("H4 capability clone aliased slices")
	}
}

func TestInteractiveHandoffH4RejectsUnknownShellRequirementOrPrivacyTopology(t *testing.T) {
	base := Baseline(Limits{}).WithDelegatedInteractive(DelegatedInteractiveSupport{ProviderID: "tmux_control_mode", ProviderVersion: 1, Platform: "darwin", MaxMutationRecords: 4096})
	for _, mutate := range []func(*InteractiveHandoffSupport){
		func(s *InteractiveHandoffSupport) { s.ShellIntegrations[0].Shell = shell.ShellUnknown },
		func(s *InteractiveHandoffSupport) { s.RequirementKinds = []shell.RequirementKind{"script"} },
		func(s *InteractiveHandoffSupport) {
			s.CaptureQualities = []receipt.CaptureQuality{receipt.CapturePartial}
		},
		func(s *InteractiveHandoffSupport) { s.Privacy.ObserverTopologyQualified = false },
		func(s *InteractiveHandoffSupport) { s.Privacy.HumanInputPersisted = true },
	} {
		s := h4Support()
		mutate(&s)
		if got := base.WithInteractiveHandoff(s); got.InteractiveHandoff != nil {
			t.Fatalf("invalid H4 support advertised: %#v", got.InteractiveHandoff)
		}
	}
}
