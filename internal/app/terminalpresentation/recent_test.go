package terminalpresentation

import (
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/terminalpresentation"
)

func TestRecentRegistryBrowserClearsActiveWithoutErasingFreshRecentTerminal(t *testing.T) {
	now := time.Date(2026, 8, 19, 15, 30, 0, 0, time.UTC)
	registry, err := NewRecentRegistry(5*time.Second, 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	ghostty := terminalIdentity("ghostty", "com.mitchellh.ghostty")
	if err := registry.Observe(ForegroundObservation{Identity: &ghostty, ObservedAt: now, Quality: core.QualityNative}); err != nil {
		t.Fatal(err)
	}
	active := registry.Candidates(now.Add(time.Second))
	if len(active) != 1 || active[0].Evidence.Source != core.SourceActive {
		t.Fatalf("active candidates=%+v", active)
	}
	if err := registry.Observe(ForegroundObservation{ObservedAt: now.Add(2 * time.Second), Quality: core.QualityNative}); err != nil {
		t.Fatal(err)
	}
	got := registry.Candidates(now.Add(3 * time.Second))
	if len(got) != 1 || got[0].Evidence.Source != core.SourceRecent || got[0].Evidence.Identity.ProviderID != "ghostty" {
		t.Fatalf("browser-front candidates=%+v", got)
	}
}

func TestRecentRegistryExpiresByReadWithoutTickerAndIgnoresOutOfOrderForeground(t *testing.T) {
	now := time.Date(2026, 8, 19, 15, 30, 0, 0, time.UTC)
	registry, err := NewRecentRegistry(time.Second, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ghostty := terminalIdentity("ghostty", "com.mitchellh.ghostty")
	_ = registry.Observe(ForegroundObservation{Identity: &ghostty, ObservedAt: now, Quality: core.QualityNative})
	_ = registry.Observe(ForegroundObservation{ObservedAt: now.Add(time.Second), Quality: core.QualityNative})
	_ = registry.Observe(ForegroundObservation{Identity: &ghostty, ObservedAt: now.Add(-time.Second), Quality: core.QualityNative})
	if got := registry.Candidates(now.Add(time.Second)); len(got) != 1 || got[0].Evidence.Source != core.SourceRecent {
		t.Fatalf("out-of-order event resurrected active state: %+v", got)
	}
	if got := registry.Candidates(now.Add(2*time.Second + time.Nanosecond)); len(got) != 0 {
		t.Fatalf("expired registry retained candidates: %+v", got)
	}
}

func TestRecentRegistryRejectsInvalidDurationsAndIdentity(t *testing.T) {
	if _, err := NewRecentRegistry(0, time.Minute); err == nil {
		t.Fatal("zero active freshness accepted")
	}
	registry, err := NewRecentRegistry(time.Second, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	bad := core.TerminalIdentity{ProviderID: "Ghostty", ProviderVersion: 1, Platform: core.PlatformDarwin, BundleID: "com.mitchellh.ghostty", ExecutableName: "ghostty"}
	if err := registry.Observe(ForegroundObservation{Identity: &bad, ObservedAt: time.Now(), Quality: core.QualityNative}); err == nil {
		t.Fatal("invalid terminal identity accepted")
	}
}
