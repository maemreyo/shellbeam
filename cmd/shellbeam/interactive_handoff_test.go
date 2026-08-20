package main

import (
	"context"
	"reflect"
	"testing"
	"time"

	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	terminalapp "github.com/maemreyo/shellbeam/internal/app/terminalpresentation"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
	terminalpresentation "github.com/maemreyo/shellbeam/internal/core/terminalpresentation"
)

type h2CapabilityProvider struct{ countingDelegatedProvider }

func (*h2CapabilityProvider) AttachHuman(context.Context, delegated.ProviderRef, delegatedapp.HumanAttachSpec) (delegatedapp.HumanAttachResult, error) {
	return delegatedapp.HumanAttachResult{}, nil
}
func (*h2CapabilityProvider) SetHumanWritable(context.Context, delegated.ProviderRef, delegatedapp.ProviderClientRef, bool) error {
	return nil
}
func (*h2CapabilityProvider) FenceHumanIngress(context.Context, delegated.ProviderRef, delegatedapp.ProviderClientRef, delegated.AuthorityEpoch) (delegatedapp.IngressFenceProof, error) {
	return delegatedapp.IngressFenceProof{}, nil
}
func (*h2CapabilityProvider) InspectHumanClient(context.Context, delegated.ProviderRef, delegatedapp.ProviderClientRef) (delegatedapp.HumanClientObservation, error) {
	return delegatedapp.HumanClientObservation{}, nil
}
func (*h2CapabilityProvider) ArmWritableHumanControl(context.Context, delegated.ProviderRef, delegatedapp.ProviderClientRef, delegatedapp.HumanControlSpec) error {
	return nil
}
func (*h2CapabilityProvider) WaitWritableHumanControl(context.Context, delegated.ProviderRef, delegatedapp.ProviderClientRef, delegatedapp.HumanControlSpec) (handoff.HumanControlKind, error) {
	return handoff.HumanControlStatus, nil
}
func (*h2CapabilityProvider) PrepareReadOnlyLocalControl(context.Context, delegated.ProviderRef, delegatedapp.ProviderClientRef) error {
	return nil
}

func TestInteractiveHandoffCapabilityRequiresQualifiedDelegatedHumanProvider(t *testing.T) {
	base := capability.Baseline(capability.Limits{})
	if got := composeInteractiveHandoffCapability(base, nil); got.InteractiveHandoff != nil || got.Features[capability.FeatureInteractiveHandoff] != capability.Unavailable {
		t.Fatalf("no H1 runtime advertised H2: %#v", got.InteractiveHandoff)
	}
	h1 := base.WithDelegatedInteractive(capability.DelegatedInteractiveSupport{ProviderID: "tmux_control_mode", ProviderVersion: 1, Platform: "darwin", MaxMutationRecords: 4096})
	if got := composeInteractiveHandoffCapability(h1, &countingDelegatedProvider{}); got.InteractiveHandoff != nil || got.Features[capability.FeatureInteractiveHandoff] != capability.Unavailable {
		t.Fatalf("H1-only provider advertised H2: %#v", got.InteractiveHandoff)
	}
	got := composeInteractiveHandoffCapability(h1, &h2CapabilityProvider{})
	if got.Features[capability.FeatureInteractiveHandoff] != capability.Available || got.InteractiveHandoff == nil || !got.InteractiveHandoff.ManualStandard || got.InteractiveHandoff.Secret || got.InteractiveHandoff.AutomaticReadiness {
		t.Fatalf("H2 capability=%#v features=%#v", got.InteractiveHandoff, got.Features)
	}
}

type handoffStartupStoreFake struct{ candidates []handoff.State }

func (s handoffStartupStoreFake) ListHandoffRecoveryCandidates(context.Context) ([]handoff.State, error) {
	return append([]handoff.State(nil), s.candidates...), nil
}

type handoffStartupReconcilerFake struct {
	catalog capability.Catalog
	calls   int
	got     []handoff.State
}

func (f *handoffStartupReconcilerFake) CapabilityCatalog() capability.Catalog { return f.catalog }
func (f *handoffStartupReconcilerFake) ReconcileHandoffStartup(_ context.Context, candidates []handoff.State, _ daemonapp.HandoffStartupOptions) error {
	f.calls++
	f.got = append([]handoff.State(nil), candidates...)
	return nil
}

func TestInteractiveHandoffStartupReconcileRunsOnlyWhenH2Available(t *testing.T) {
	candidate := handoff.State{HandoffID: "handoff-startup-hook"}
	store := handoffStartupStoreFake{candidates: []handoff.State{candidate}}
	base := capability.Baseline(capability.Limits{})
	unavailable := &handoffStartupReconcilerFake{catalog: base}
	if err := reconcileHandoffDaemonStartup(t.Context(), store, unavailable); err != nil {
		t.Fatal(err)
	}
	if unavailable.calls != 0 {
		t.Fatalf("unavailable H2 reconciled candidates: %d", unavailable.calls)
	}
	availableCatalog := base.WithDelegatedInteractive(capability.DelegatedInteractiveSupport{ProviderID: "tmux_control_mode", ProviderVersion: 1, Platform: "darwin", MaxMutationRecords: 4096}).WithInteractiveHandoff(capability.InteractiveHandoffSupport{ManualStandard: true})
	available := &handoffStartupReconcilerFake{catalog: availableCatalog}
	if err := reconcileHandoffDaemonStartup(t.Context(), store, available); err != nil {
		t.Fatal(err)
	}
	if available.calls != 1 || len(available.got) != 1 || available.got[0].HandoffID != candidate.HandoffID {
		t.Fatalf("calls=%d candidates=%#v", available.calls, available.got)
	}
}

func TestTerminalPresentationCapabilityRequiresH2AndExactQualifiedProviders(t *testing.T) {
	base := capability.Baseline(capability.Limits{})
	ghostty := terminalIdentityForCapabilityTest()
	if got := composeTerminalPresentationCapability(base, []terminalpresentation.TerminalIdentity{ghostty}); got.InteractiveHandoff != nil {
		t.Fatalf("H3 advertised without H2: %#v", got.InteractiveHandoff)
	}
	h2 := base.WithDelegatedInteractive(capability.DelegatedInteractiveSupport{ProviderID: "tmux_control_mode", ProviderVersion: 1, Platform: "darwin", MaxMutationRecords: 4096}).WithInteractiveHandoff(capability.InteractiveHandoffSupport{ManualStandard: true})
	if got := composeTerminalPresentationCapability(h2, nil); got.InteractiveHandoff == nil || got.InteractiveHandoff.TerminalPresentation != nil || !got.InteractiveHandoff.ManualStandard {
		t.Fatalf("missing H3 changed H2: %#v", got.InteractiveHandoff)
	}
	got := composeTerminalPresentationCapability(h2, []terminalpresentation.TerminalIdentity{ghostty})
	if got.InteractiveHandoff == nil || got.InteractiveHandoff.TerminalPresentation == nil {
		t.Fatalf("qualified H3 not advertised: %#v", got.InteractiveHandoff)
	}
	presentation := got.InteractiveHandoff.TerminalPresentation
	wantSources := []string{"active", "recent", "bridge_affinity", "single_running"}
	if !reflect.DeepEqual(presentation.ResolutionSources, wantSources) || !reflect.DeepEqual(presentation.QualifiedLaunchers, []string{"ghostty"}) {
		t.Fatalf("terminal presentation=%#v", presentation)
	}
}

func terminalIdentityForCapabilityTest() terminalpresentation.TerminalIdentity {
	return terminalpresentation.TerminalIdentity{ProviderID: "ghostty", ProviderVersion: 1, Platform: terminalpresentation.PlatformDarwin, BundleID: "com.mitchellh.ghostty", ExecutableName: "ghostty"}
}

type terminalRuntimeActivityFake struct{ runs int }

func (f *terminalRuntimeActivityFake) Current(context.Context) (terminalapp.ForegroundObservation, error) {
	return terminalapp.ForegroundObservation{ObservedAt: time.Date(2026, 8, 20, 7, 0, 0, 0, time.UTC), Quality: terminalpresentation.QualityNative}, nil
}
func (f *terminalRuntimeActivityFake) Run(_ context.Context, _ func(terminalapp.ForegroundObservation) error) error {
	f.runs++
	return nil
}

type terminalRuntimeRunningFake struct {
	identities []terminalpresentation.TerminalIdentity
}

func (f terminalRuntimeRunningFake) Running(context.Context) ([]terminalpresentation.TerminalIdentity, error) {
	return append([]terminalpresentation.TerminalIdentity(nil), f.identities...), nil
}

type terminalRuntimeStoreFake struct{}

func (terminalRuntimeStoreFake) ReserveTerminalLaunch(context.Context, terminalapp.LaunchRecord) (terminalapp.LaunchRecord, bool, error) {
	return terminalapp.LaunchRecord{}, false, nil
}
func (terminalRuntimeStoreFake) CompleteTerminalLaunch(context.Context, terminalapp.LaunchRecord) (terminalapp.LaunchRecord, error) {
	return terminalapp.LaunchRecord{}, nil
}

type terminalRuntimeLauncherFake struct{}

func (terminalRuntimeLauncherFake) Launch(context.Context, terminalapp.LaunchRequest) (terminalapp.LaunchResult, error) {
	return terminalapp.LaunchResult{}, nil
}

type terminalRuntimeExactProverFake struct{}

func (terminalRuntimeExactProverFake) ExactHumanClientPresent(context.Context, string) (bool, error) {
	return false, nil
}

func TestBuildTerminalPresentationRuntimeBindsSharedResolverAndPresenterFactory(t *testing.T) {
	base := capability.Baseline(capability.Limits{}).WithDelegatedInteractive(capability.DelegatedInteractiveSupport{ProviderID: "tmux_control_mode", ProviderVersion: 1, Platform: "darwin", MaxMutationRecords: 4096}).WithInteractiveHandoff(capability.InteractiveHandoffSupport{ManualStandard: true})
	activity := &terminalRuntimeActivityFake{}
	ghostty := terminalIdentityForCapabilityTest()
	runtime, err := buildTerminalPresentationRuntime(terminalPresentationRuntimeInput{
		Catalog: base, Providers: []terminalpresentation.TerminalIdentity{ghostty}, Store: terminalRuntimeStoreFake{},
		Activity: activity, Running: terminalRuntimeRunningFake{identities: []terminalpresentation.TerminalIdentity{ghostty}}, Launcher: terminalRuntimeLauncherFake{},
		Executable: "/usr/local/bin/shellbeam", Now: func() time.Time { return time.Date(2026, 8, 20, 7, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.PresenterFactory == nil || runtime.Start == nil || runtime.Catalog.InteractiveHandoff == nil || runtime.Catalog.InteractiveHandoff.TerminalPresentation == nil {
		t.Fatalf("runtime=%#v", runtime)
	}
	if presenter := runtime.PresenterFactory(terminalRuntimeExactProverFake{}); presenter == nil {
		t.Fatal("presenter factory returned nil")
	}
	if err := runtime.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if activity.runs != 1 {
		t.Fatalf("activity runs=%d want=1", activity.runs)
	}
}
