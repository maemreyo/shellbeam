package shellintegration

import (
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/shellintegration"
)

func TestShellIdentityObservationRejectsChangedIdentityAsAdapterAuthority(t *testing.T) {
	now := time.Date(2026, 8, 20, 9, 40, 0, 0, time.UTC)
	current := ShellIdentityObservation{Identity: core.ShellIdentity{Family: core.ShellFish, RuntimeID: "runtime-fish"}, State: IdentityExact, ObservedAt: now}
	if err := current.Validate(); err != nil {
		t.Fatalf("exact identity rejected: %v", err)
	}
	changed := ShellIdentityObservation{Identity: core.ShellIdentity{Family: core.ShellUnknown, RuntimeID: "runtime-zsh"}, State: IdentityChanged, ObservedAt: now}
	if err := changed.Validate(); err != nil {
		t.Fatalf("changed identity rejected: %v", err)
	}
	if changed.AdapterEligible() {
		t.Fatal("changed identity remained adapter eligible")
	}
	unknown := ShellIdentityObservation{Identity: core.ShellIdentity{Family: core.ShellUnknown, RuntimeID: "runtime-nu"}, State: IdentityUnknown, ObservedAt: now}
	if err := unknown.Validate(); err != nil || unknown.AdapterEligible() {
		t.Fatalf("unknown identity err=%v eligible=%v", err, unknown.AdapterEligible())
	}
}

func TestProviderProcessFactsRequirePaneScopedCurrentProcessIdentity(t *testing.T) {
	facts := ProviderProcessFacts{SessionID: "session-1", ProviderID: "tmux_control_mode", ProviderVersion: 1, ProviderGeneration: "gen_1", PanePID: 42, CurrentCommand: "fish", PaneTTY: "/dev/ttys042", CWD: "/tmp/project", LoginShell: "/bin/zsh"}
	if err := facts.Validate(); err != nil {
		t.Fatalf("valid provider facts rejected: %v", err)
	}
	for _, bad := range []ProviderProcessFacts{
		{},
		{SessionID: "session-1", ProviderID: "tmux_control_mode", ProviderVersion: 1, ProviderGeneration: "gen_1", PanePID: 0, CurrentCommand: "fish"},
		{SessionID: "session-1", ProviderID: "tmux_control_mode", ProviderVersion: 1, ProviderGeneration: "gen_1", PanePID: 42},
	} {
		if err := bad.Validate(); err == nil {
			t.Fatalf("bad provider facts accepted: %#v", bad)
		}
	}
}

func TestContextShellIdentityUsesCanonicalFamilyRuntimeEncoding(t *testing.T) {
	cases := []struct {
		name string
		in   core.ShellIdentity
		want string
	}{
		{name: "fish", in: core.ShellIdentity{Family: core.ShellFish, RuntimeID: "runtime_01"}, want: "fish:runtime_01"},
		{name: "zsh", in: core.ShellIdentity{Family: core.ShellZsh, RuntimeID: "runtime_02"}, want: "zsh:runtime_02"},
		{name: "bash", in: core.ShellIdentity{Family: core.ShellBash, RuntimeID: "runtime_03"}, want: "bash:runtime_03"},
		{name: "nushell", in: core.ShellIdentity{Family: core.ShellNushell, RuntimeID: "runtime_04"}, want: "nushell:runtime_04"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ContextShellIdentity(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("identity=%q want=%q", got, tc.want)
			}
		})
	}
	for _, bad := range []core.ShellIdentity{
		{Family: core.ShellUnknown, RuntimeID: "runtime_unknown"},
		{Family: core.ShellFish, RuntimeID: ""},
		{Family: core.ShellFamily("future"), RuntimeID: "runtime_future"},
	} {
		if _, err := ContextShellIdentity(bad); err == nil {
			t.Fatalf("invalid shell identity accepted: %#v", bad)
		}
	}
}
