package main

import (
	"context"
	"testing"

	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
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
