package capability

import (
	"reflect"
	"testing"
)

func TestWithTerminalPresentationExtendsH2WithoutChangingManualAuthority(t *testing.T) {
	base := Baseline(Limits{}).
		WithDelegatedInteractive(DelegatedInteractiveSupport{ProviderID: "tmux_control_mode", ProviderVersion: 1, Platform: "darwin", MaxMutationRecords: 4096}).
		WithInteractiveHandoff(InteractiveHandoffSupport{ManualStandard: true})

	support := TerminalPresentationSupport{
		ResolutionSources:  []string{"existing_client", "active", "recent", "bridge_affinity", "single_running", "fallback"},
		QualifiedLaunchers: []string{"ghostty"},
	}
	got := base.WithTerminalPresentation(support)
	if got.Features[FeatureInteractiveHandoff] != Available || got.InteractiveHandoff == nil || !got.InteractiveHandoff.ManualStandard {
		t.Fatalf("H2 authority capability changed: %#v", got.InteractiveHandoff)
	}
	if got.InteractiveHandoff.TerminalPresentation == nil || !reflect.DeepEqual(got.InteractiveHandoff.TerminalPresentation.ResolutionSources, support.ResolutionSources) || !reflect.DeepEqual(got.InteractiveHandoff.TerminalPresentation.QualifiedLaunchers, support.QualifiedLaunchers) {
		t.Fatalf("terminal presentation capability=%#v", got.InteractiveHandoff.TerminalPresentation)
	}

	got.InteractiveHandoff.TerminalPresentation.ResolutionSources[0] = "mutated"
	got.InteractiveHandoff.TerminalPresentation.QualifiedLaunchers[0] = "mutated"
	if base.InteractiveHandoff.TerminalPresentation != nil {
		t.Fatal("terminal presentation mutation aliased H2 base")
	}
}

func TestWithTerminalPresentationFailsClosedWithoutH2OrQualifiedLauncher(t *testing.T) {
	base := Baseline(Limits{}).WithDelegatedInteractive(DelegatedInteractiveSupport{ProviderID: "tmux_control_mode", ProviderVersion: 1, Platform: "darwin", MaxMutationRecords: 4096})
	if got := base.WithTerminalPresentation(TerminalPresentationSupport{ResolutionSources: []string{"active"}, QualifiedLaunchers: []string{"ghostty"}}); got.InteractiveHandoff != nil {
		t.Fatalf("H3 advertised without H2: %#v", got.InteractiveHandoff)
	}

	h2 := base.WithInteractiveHandoff(InteractiveHandoffSupport{ManualStandard: true})
	if got := h2.WithTerminalPresentation(TerminalPresentationSupport{ResolutionSources: []string{"active"}}); got.InteractiveHandoff.TerminalPresentation != nil {
		t.Fatalf("H3 advertised without qualified launcher: %#v", got.InteractiveHandoff.TerminalPresentation)
	}
}
