//go:build darwin || linux

package shellintegration

import (
	"context"
	"testing"

	app "github.com/maemreyo/shellbeam/internal/app/shellintegration"
	core "github.com/maemreyo/shellbeam/internal/core/shellintegration"
)

func TestProbeUsesCurrentPaneCommandNotLoginShell(t *testing.T) {
	probe := NewUnixProbe()
	obs, err := probe.Probe(context.Background(), app.ProbeRequest{Facts: app.ProviderProcessFacts{
		SessionID: "session-1", ProviderID: "tmux_control_mode", ProviderVersion: 1, ProviderGeneration: "gen_1", PanePID: 42,
		CurrentCommand: "fish", LoginShell: "/bin/zsh",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if obs.State != app.IdentityExact || obs.Identity.Family != core.ShellFish {
		t.Fatalf("observation=%#v", obs)
	}
}

func TestProbeDetectsReplacementThenRequiresExplicitReprobe(t *testing.T) {
	probe := NewUnixProbe()
	first, err := probe.Probe(context.Background(), app.ProbeRequest{Facts: app.ProviderProcessFacts{SessionID: "session-1", ProviderID: "tmux_control_mode", ProviderVersion: 1, ProviderGeneration: "gen_1", PanePID: 42, CurrentCommand: "fish"}})
	if err != nil {
		t.Fatal(err)
	}
	changed, err := probe.Probe(context.Background(), app.ProbeRequest{Facts: app.ProviderProcessFacts{SessionID: "session-1", ProviderID: "tmux_control_mode", ProviderVersion: 1, ProviderGeneration: "gen_1", PanePID: 42, CurrentCommand: "zsh"}, Expected: &first.Identity})
	if err != nil {
		t.Fatal(err)
	}
	if changed.State != app.IdentityChanged || changed.Identity.Family != core.ShellUnknown || changed.AdapterEligible() {
		t.Fatalf("changed=%#v", changed)
	}
	reprobed, err := probe.Probe(context.Background(), app.ProbeRequest{Facts: app.ProviderProcessFacts{SessionID: "session-1", ProviderID: "tmux_control_mode", ProviderVersion: 1, ProviderGeneration: "gen_1", PanePID: 42, CurrentCommand: "zsh"}})
	if err != nil {
		t.Fatal(err)
	}
	if reprobed.State != app.IdentityExact || reprobed.Identity.Family != core.ShellZsh || reprobed.Identity.RuntimeID == first.Identity.RuntimeID {
		t.Fatalf("reprobed=%#v first=%#v", reprobed, first)
	}
}

func TestProbeUnknownForegroundNeverFallsBackToBash(t *testing.T) {
	obs, err := NewUnixProbe().Probe(context.Background(), app.ProbeRequest{Facts: app.ProviderProcessFacts{SessionID: "session-1", ProviderID: "tmux_control_mode", ProviderVersion: 1, ProviderGeneration: "gen_1", PanePID: 42, CurrentCommand: "nu", LoginShell: "/bin/bash"}})
	if err != nil {
		t.Fatal(err)
	}
	if obs.State != app.IdentityUnknown || obs.Identity.Family != core.ShellUnknown || obs.AdapterEligible() {
		t.Fatalf("unknown observation=%#v", obs)
	}
}
