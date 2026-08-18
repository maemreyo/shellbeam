package delegatedsession

import "testing"

func TestReconcileAuthorityGrantsOnlyExactCurrentAgentOwnership(t *testing.T) {
	provider := ProviderIdentity{ID: "tmux_control_mode", Version: 1}
	base := ReconcileInput{
		Epoch:            4,
		DesiredOwner:     OwnerAgent,
		ObservedOwner:    OwnerAgent,
		ProviderIdentity: provider,
		ProviderCurrent:  true,
	}
	got := ReconcileAuthority(base)
	if got.Epoch != 4 || got.Owner != OwnerAgent || got.Fenced {
		t.Fatalf("exact current agent authority not granted: %#v", got)
	}

	cases := map[string]func(*ReconcileInput){
		"provider_stale":   func(v *ReconcileInput) { v.ProviderCurrent = false },
		"observed_none":    func(v *ReconcileInput) { v.ObservedOwner = OwnerNone },
		"desired_none":     func(v *ReconcileInput) { v.DesiredOwner = OwnerNone },
		"human_match":      func(v *ReconcileInput) { v.DesiredOwner, v.ObservedOwner = OwnerHuman, OwnerHuman },
		"invalid_provider": func(v *ReconcileInput) { v.ProviderIdentity.Version = 0 },
		"invalid_epoch":    func(v *ReconcileInput) { v.Epoch = 0 },
		"unknown_observed": func(v *ReconcileInput) { v.ObservedOwner = "unknown" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			in := base
			mutate(&in)
			got := ReconcileAuthority(in)
			if got.Owner != OwnerNone || !got.Fenced {
				t.Fatalf("unsafe authority granted: %#v", got)
			}
		})
	}
}
