package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCaptureMCPBridgeAffinityRequiresDarwinAndValidatedAncestor(t *testing.T) {
	now := time.Date(2026, 8, 19, 16, 15, 0, 0, time.UTC)
	deps := mcpTerminalAffinityDeps{
		platform: "darwin",
		now:      func() time.Time { return now },
		getenv: func(key string) string {
			if key == "TERM_PROGRAM" {
				return "ghostty"
			}
			return ""
		},
		ancestors: func(context.Context) ([]string, error) {
			return []string{"/bin/fish", "/Applications/Ghostty.app/Contents/MacOS/ghostty"}, nil
		},
	}
	got, err := captureMCPBridgeAffinity(context.Background(), deps)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Identity.ProviderID != "ghostty" || got.Identity.BundleID != "com.mitchellh.ghostty" {
		t.Fatalf("affinity=%+v", got)
	}
	if got.ObservedAt != now || got.FreshUntil != now.Add(defaultMCPBridgeAffinityFreshness) {
		t.Fatalf("freshness=%+v", got)
	}
}

func TestCaptureMCPBridgeAffinityRawTERMProgramAloneDoesNotQualify(t *testing.T) {
	now := time.Date(2026, 8, 19, 16, 15, 0, 0, time.UTC)
	got, err := captureMCPBridgeAffinity(context.Background(), mcpTerminalAffinityDeps{
		platform: "darwin", now: func() time.Time { return now },
		getenv:    func(string) string { return "ghostty" },
		ancestors: func(context.Context) ([]string, error) { return []string{"/bin/fish", "/usr/bin/login"}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("raw TERM_PROGRAM qualified affinity=%+v", got)
	}
}

func TestCaptureMCPBridgeAffinityDegradesOnUnsupportedPlatformOrAncestryFailure(t *testing.T) {
	now := time.Date(2026, 8, 19, 16, 15, 0, 0, time.UTC)
	called := false
	got, err := captureMCPBridgeAffinity(context.Background(), mcpTerminalAffinityDeps{
		platform: "linux", now: func() time.Time { return now }, getenv: func(string) string { return "ghostty" },
		ancestors: func(context.Context) ([]string, error) { called = true; return nil, nil },
	})
	if err != nil || got != nil || called {
		t.Fatalf("linux got=%+v err=%v called=%v", got, err, called)
	}

	got, err = captureMCPBridgeAffinity(context.Background(), mcpTerminalAffinityDeps{
		platform: "darwin", now: func() time.Time { return now }, getenv: func(string) string { return "ghostty" },
		ancestors: func(context.Context) ([]string, error) { return nil, errors.New("ps unavailable") },
	})
	if err != nil || got != nil {
		t.Fatalf("ancestry degradation got=%+v err=%v", got, err)
	}
}

func TestMCPBridgeAffinityProviderSetIsFrozenToPreflight(t *testing.T) {
	providers := mcpBridgeTerminalProviders()
	if len(providers) != 1 {
		t.Fatalf("providers=%+v", providers)
	}
	got := providers[0]
	if got.ProviderID != "ghostty" || got.ProviderVersion != 1 || got.BundleID != "com.mitchellh.ghostty" || got.ExecutableName != "ghostty" {
		t.Fatalf("provider=%+v", got)
	}
}
