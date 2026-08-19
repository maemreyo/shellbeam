package main

import (
	"context"
	"testing"

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
