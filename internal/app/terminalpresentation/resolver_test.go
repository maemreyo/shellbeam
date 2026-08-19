package terminalpresentation

import (
	"context"
	"errors"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/terminalpresentation"
)

func TestResolverRecentBeatsBridgeAndSingleRunningWhenBrowserIsFrontmost(t *testing.T) {
	now := time.Date(2026, 8, 19, 15, 40, 0, 0, time.UTC)
	ghostty := terminalIdentity("ghostty", "com.mitchellh.ghostty")
	wezterm := terminalIdentity("wezterm", "com.github.wez.wezterm")
	registry, _ := NewRecentRegistry(5*time.Second, time.Minute)
	_ = registry.Observe(ForegroundObservation{Identity: &ghostty, ObservedAt: now.Add(-10 * time.Second), Quality: core.QualityNative})
	_ = registry.Observe(ForegroundObservation{ObservedAt: now.Add(-9 * time.Second), Quality: core.QualityNative})
	activity := &fakeActivitySource{current: ForegroundObservation{ObservedAt: now, Quality: core.QualityNative}}
	resolver, err := NewResolver(registry, activity, &fakeRunningSource{identities: []core.TerminalIdentity{wezterm}}, time.Second, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	result, err := resolver.Resolve(context.Background(), ResolveRequest{BridgeAffinity: evidence(ghostty, core.SourceBridgeAffinity, now, time.Minute, core.QualityValidated), Fallback: evidence(wezterm, core.SourceFallback, now, time.Minute, core.QualityQualified)})
	if err != nil {
		t.Fatal(err)
	}
	if result.Resolution.Selected == nil || result.Resolution.Selected.Evidence.Source != core.SourceRecent {
		t.Fatalf("selected=%+v", result.Resolution.Selected)
	}
}

func TestResolverExistingClientWinsActive(t *testing.T) {
	now := time.Date(2026, 8, 19, 15, 40, 0, 0, time.UTC)
	ghostty := terminalIdentity("ghostty", "com.mitchellh.ghostty")
	wezterm := terminalIdentity("wezterm", "com.github.wez.wezterm")
	registry, _ := NewRecentRegistry(time.Minute, time.Minute)
	resolver, _ := NewResolver(registry, &fakeActivitySource{current: ForegroundObservation{Identity: &wezterm, ObservedAt: now, Quality: core.QualityNative}}, &fakeRunningSource{}, time.Second, func() time.Time { return now })
	result, err := resolver.Resolve(context.Background(), ResolveRequest{ExistingClient: evidence(ghostty, core.SourceExistingClient, now, time.Minute, core.QualityExact)})
	if err != nil {
		t.Fatal(err)
	}
	if result.Resolution.Selected == nil || result.Resolution.Selected.Evidence.Source != core.SourceExistingClient {
		t.Fatalf("selected=%+v", result.Resolution.Selected)
	}
}

func TestResolverProviderFailureDegradesLaneAndKeepsBridgeEvidence(t *testing.T) {
	now := time.Date(2026, 8, 19, 15, 40, 0, 0, time.UTC)
	ghostty := terminalIdentity("ghostty", "com.mitchellh.ghostty")
	registry, _ := NewRecentRegistry(time.Second, time.Minute)
	resolver, _ := NewResolver(registry, &fakeActivitySource{currentErr: errors.New("activity unavailable")}, &fakeRunningSource{err: errors.New("running unavailable")}, time.Second, func() time.Time { return now })
	result, err := resolver.Resolve(context.Background(), ResolveRequest{BridgeAffinity: evidence(ghostty, core.SourceBridgeAffinity, now, time.Minute, core.QualityValidated)})
	if err != nil {
		t.Fatal(err)
	}
	if result.Resolution.Selected == nil || result.Resolution.Selected.Evidence.Source != core.SourceBridgeAffinity {
		t.Fatalf("selected=%+v", result.Resolution.Selected)
	}
	if len(result.UnavailableSources) != 2 || result.UnavailableSources[0] != UnavailableActivity || result.UnavailableSources[1] != UnavailableRunning {
		t.Fatalf("unavailable=%v", result.UnavailableSources)
	}
}

func TestResolverMultipleRunningCandidatesFallThroughAndRejectsWrongLane(t *testing.T) {
	now := time.Date(2026, 8, 19, 15, 40, 0, 0, time.UTC)
	ghostty := terminalIdentity("ghostty", "com.mitchellh.ghostty")
	wezterm := terminalIdentity("wezterm", "com.github.wez.wezterm")
	registry, _ := NewRecentRegistry(time.Second, time.Second)
	resolver, _ := NewResolver(registry, nil, &fakeRunningSource{identities: []core.TerminalIdentity{ghostty, wezterm}}, time.Second, func() time.Time { return now })
	result, err := resolver.Resolve(context.Background(), ResolveRequest{Fallback: evidence(ghostty, core.SourceFallback, now, time.Minute, core.QualityQualified)})
	if err != nil {
		t.Fatal(err)
	}
	if result.Resolution.Selected == nil || result.Resolution.Selected.Evidence.Source != core.SourceFallback {
		t.Fatalf("selected=%+v", result.Resolution.Selected)
	}
	if _, err := resolver.Resolve(context.Background(), ResolveRequest{BridgeAffinity: evidence(ghostty, core.SourceRecent, now, time.Minute, core.QualityNative)}); err == nil {
		t.Fatal("recent evidence accepted as bridge affinity")
	}
}

func terminalIdentity(providerID, bundleID string) core.TerminalIdentity {
	return core.TerminalIdentity{ProviderID: providerID, ProviderVersion: 1, Platform: core.PlatformDarwin, BundleID: bundleID, ExecutableName: providerID}
}
func evidence(identity core.TerminalIdentity, source core.EvidenceSource, now time.Time, ttl time.Duration, quality core.EvidenceQuality) *core.Evidence {
	return &core.Evidence{Identity: identity, Source: source, ObservedAt: now, FreshUntil: now.Add(ttl), Quality: quality}
}

type fakeActivitySource struct {
	current    ForegroundObservation
	currentErr error
}

func (f *fakeActivitySource) Current(context.Context) (ForegroundObservation, error) {
	return f.current, f.currentErr
}
func (f *fakeActivitySource) Run(context.Context, func(ForegroundObservation) error) error {
	return nil
}

type fakeRunningSource struct {
	identities []core.TerminalIdentity
	err        error
}

func (f *fakeRunningSource) Running(context.Context) ([]core.TerminalIdentity, error) {
	return f.identities, f.err
}

func TestResolverPropagatesCallerCancellationInsteadOfDegradingIt(t *testing.T) {
	now := time.Date(2026, 8, 19, 15, 40, 0, 0, time.UTC)
	registry, _ := NewRecentRegistry(time.Second, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	resolver, _ := NewResolver(registry, &fakeActivitySource{currentErr: context.Canceled}, &fakeRunningSource{}, time.Second, func() time.Time { return now })
	if _, err := resolver.Resolve(ctx, ResolveRequest{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Resolve() error=%v want context.Canceled", err)
	}
}
