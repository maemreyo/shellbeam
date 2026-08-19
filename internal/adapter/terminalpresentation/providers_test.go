package terminalpresentation

import (
	"reflect"
	"testing"

	core "github.com/maemreyo/shellbeam/internal/core/terminalpresentation"
)

func TestQualifiedProvidersMatchTask1PreflightExactly(t *testing.T) {
	providers := QualifiedProviders()
	if len(providers) != 1 {
		t.Fatalf("providers=%+v", providers)
	}
	got := providers[0]
	wantIdentity := core.TerminalIdentity{
		ProviderID:      "ghostty",
		ProviderVersion: 1,
		Platform:        core.PlatformDarwin,
		BundleID:        "com.mitchellh.ghostty",
		ExecutableName:  "ghostty",
	}
	if got.Identity != wantIdentity {
		t.Fatalf("identity=%+v want=%+v", got.Identity, wantIdentity)
	}
	if got.LaunchAdapter != LaunchAdapterDarwinOpenArgs || got.LaunchExecutable != "/usr/bin/open" {
		t.Fatalf("provider=%+v", got)
	}
	wantPrefix := []string{"-n", "-b", "com.mitchellh.ghostty", "--args", "-e"}
	if !reflect.DeepEqual(got.ArgumentPrefix, wantPrefix) {
		t.Fatalf("prefix=%q want=%q", got.ArgumentPrefix, wantPrefix)
	}
}

func TestQualifiedProvidersReturnsDefensiveCopies(t *testing.T) {
	first := QualifiedProviders()
	first[0].ArgumentPrefix[0] = "--mutated"
	second := QualifiedProviders()
	if second[0].ArgumentPrefix[0] != "-n" {
		t.Fatalf("registry leaked mutable prefix: %+v", second[0])
	}
}

func TestLookupQualifiedProviderRequiresExactFrozenIdentity(t *testing.T) {
	identity := QualifiedProviders()[0].Identity
	provider, err := LookupQualifiedProvider(identity)
	if err != nil || provider.Identity != identity {
		t.Fatalf("lookup=%+v err=%v", provider, err)
	}
	for _, mutate := range []func(*core.TerminalIdentity){
		func(v *core.TerminalIdentity) { v.ProviderVersion++ },
		func(v *core.TerminalIdentity) { v.BundleID = "com.example.fake" },
		func(v *core.TerminalIdentity) { v.ExecutableName = "fakeghostty" },
		func(v *core.TerminalIdentity) { v.Platform = core.PlatformLinux },
	} {
		changed := identity
		mutate(&changed)
		if _, err := LookupQualifiedProvider(changed); err == nil {
			t.Fatalf("mismatched identity accepted: %+v", changed)
		}
	}
}
