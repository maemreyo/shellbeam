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
