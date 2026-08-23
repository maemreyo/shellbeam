package terminalpresentation

import (
	"errors"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/terminalpresentation"
)

type LaunchAdapter string

const LaunchAdapterDarwinOpenArgs LaunchAdapter = "darwin_open_args"

type ProviderDefinition struct {
	Identity         core.TerminalIdentity
	LaunchAdapter    LaunchAdapter
	LaunchExecutable string
	ArgumentPrefix   []string
}

func (p ProviderDefinition) Validate() error {
	if err := p.Identity.Validate(); err != nil {
		return err
	}
	if p.LaunchAdapter != LaunchAdapterDarwinOpenArgs || p.Identity.Platform != core.PlatformDarwin {
		return errors.New("terminal provider launch adapter is not qualified")
	}
	if p.LaunchExecutable != "/usr/bin/open" {
		return errors.New("terminal provider launch executable is not qualified")
	}
	want := []string{"-n", "-b", p.Identity.BundleID, "--args", "-e"}
	if !equalStrings(p.ArgumentPrefix, want) {
		return errors.New("terminal provider launch argv does not match qualification")
	}
	return nil
}

func QualifiedProviders() []ProviderDefinition {
	return []ProviderDefinition{cloneProvider(ghosttyProvider())}
}

func QualifiedIdentities() []core.TerminalIdentity {
	providers := QualifiedProviders()
	result := make([]core.TerminalIdentity, len(providers))
	for i := range providers {
		result[i] = providers[i].Identity
	}
	return result
}

func LookupQualifiedProvider(identity core.TerminalIdentity) (ProviderDefinition, error) {
	if err := identity.Validate(); err != nil {
		return ProviderDefinition{}, err
	}
	for _, provider := range QualifiedProviders() {
		if provider.Identity.StableKey() == identity.StableKey() {
			return provider, nil
		}
		if provider.Identity.ProviderID == identity.ProviderID {
			return ProviderDefinition{}, failure.New(failure.TerminalIdentityAmbiguous, map[string]string{
				"provider_id": identity.ProviderID,
				"reason":      "qualified_identity_mismatch",
			}, nil)
		}
	}
	return ProviderDefinition{}, failure.New(failure.TerminalLauncherUnavailable, map[string]string{
		"provider_id": identity.ProviderID,
		"reason":      "provider_not_qualified",
	}, nil)
}

func ghosttyProvider() ProviderDefinition {
	identity := core.TerminalIdentity{
		ProviderID:      "ghostty",
		ProviderVersion: 1,
		Platform:        core.PlatformDarwin,
		BundleID:        "com.mitchellh.ghostty",
		ExecutableName:  "ghostty",
	}
	return ProviderDefinition{
		Identity:         identity,
		LaunchAdapter:    LaunchAdapterDarwinOpenArgs,
		LaunchExecutable: "/usr/bin/open",
		ArgumentPrefix:   []string{"-n", "-b", identity.BundleID, "--args", "-e"},
	}
}

func cloneProvider(provider ProviderDefinition) ProviderDefinition {
	provider.ArgumentPrefix = append([]string(nil), provider.ArgumentPrefix...)
	return provider
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
