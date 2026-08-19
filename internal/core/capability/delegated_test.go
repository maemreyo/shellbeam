package capability

import (
	"reflect"
	"testing"
)

func TestDelegatedInteractiveCapabilityIsExplicitBoundedVersionedAndCloned(t *testing.T) {
	base := Baseline(Limits{})
	if base.Features[FeatureDelegatedInteractive] != Unavailable || base.DelegatedInteractive != nil || containsInt(base.ReceiptSchemaVersions, 5) {
		t.Fatalf("baseline leaked delegated support: %#v", base)
	}
	support := DelegatedInteractiveSupport{
		ProviderID: "tmux_control_mode", ProviderVersion: 1, Platform: "darwin",
		MaxMutationRecords: 4096, DaemonRestartContinuity: false, HostRebootContinuity: false,
	}
	got := base.WithDelegatedInteractive(support)
	if got.Features[FeatureDelegatedInteractive] != Available || got.DelegatedInteractive == nil {
		t.Fatalf("capability=%#v", got)
	}
	if !reflect.DeepEqual(*got.DelegatedInteractive, support) || !containsInt(got.ReceiptSchemaVersions, 5) {
		t.Fatalf("support=%#v receipts=%v", got.DelegatedInteractive, got.ReceiptSchemaVersions)
	}
	clone := got.Clone()
	clone.DelegatedInteractive.ProviderID = "changed"
	if got.DelegatedInteractive.ProviderID != "tmux_control_mode" {
		t.Fatal("delegated capability clone aliases support pointer")
	}
	if base.Features[FeatureDelegatedInteractive] != Unavailable || base.DelegatedInteractive != nil || containsInt(base.ReceiptSchemaVersions, 5) {
		t.Fatal("WithDelegatedInteractive mutated baseline")
	}
}

func TestDelegatedInteractiveCapabilityRejectsIncompleteIdentityOrBounds(t *testing.T) {
	base := Baseline(Limits{})
	cases := []DelegatedInteractiveSupport{
		{},
		{ProviderID: "tmux_control_mode", ProviderVersion: 0, Platform: "darwin", MaxMutationRecords: 4096},
		{ProviderID: "tmux_control_mode", ProviderVersion: 1, Platform: "", MaxMutationRecords: 4096},
		{ProviderID: "tmux_control_mode", ProviderVersion: 1, Platform: "darwin", MaxMutationRecords: 0},
	}
	for _, support := range cases {
		got := base.WithDelegatedInteractive(support)
		if got.Features[FeatureDelegatedInteractive] != Unavailable || got.DelegatedInteractive != nil || containsInt(got.ReceiptSchemaVersions, 5) {
			t.Fatalf("invalid support advertised: %#v", support)
		}
	}
}

func TestDelegatedInteractiveContinuityFieldsAreTruthfulAndIndependent(t *testing.T) {
	base := Baseline(Limits{})
	got := base.WithDelegatedInteractive(DelegatedInteractiveSupport{ProviderID: "tmux_control_mode", ProviderVersion: 1, Platform: "darwin", MaxMutationRecords: 4096, DaemonRestartContinuity: false, HostRebootContinuity: false})
	if got.DelegatedInteractive.DaemonRestartContinuity || got.DelegatedInteractive.HostRebootContinuity {
		t.Fatalf("task7 overclaimed continuity: %#v", got.DelegatedInteractive)
	}
}

func containsInt(values []int, want int) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
