package shellintegration

import "testing"

func TestShellIdentityAndCapabilityClosedVocabulary(t *testing.T) {
	validShells := []ShellFamily{ShellFish, ShellZsh, ShellBash, ShellUnknown}
	for _, family := range validShells {
		identity := ShellIdentity{Family: family, RuntimeID: "runtime-1"}
		if err := identity.Validate(); err != nil {
			t.Fatalf("shell %q rejected: %v", family, err)
		}
	}
	for _, bad := range []ShellIdentity{
		{},
		{Family: "nu", RuntimeID: "runtime-1"},
		{Family: ShellFish},
		{Family: ShellFish, RuntimeID: "/bin/fish"},
	} {
		if err := bad.Validate(); err == nil {
			t.Fatalf("bad shell identity accepted: %#v", bad)
		}
	}

	levels := []CapabilityLevel{CapabilityPTYOnly, CapabilityInteractive, CapabilityShellAware, CapabilityRequirementAware, CapabilityFullHandoff}
	for _, level := range levels {
		if err := level.Validate(); err != nil {
			t.Fatalf("capability %q rejected: %v", level, err)
		}
	}
	if err := CapabilityLevel("full").Validate(); err == nil {
		t.Fatal("unknown capability accepted")
	}
}
