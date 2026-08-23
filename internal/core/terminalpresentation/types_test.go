package terminalpresentation

import (
	"testing"
	"time"
)

func TestTerminalIdentityValidateRejectsExecutablePathsAndUnknownPlatforms(t *testing.T) {
	valid := TerminalIdentity{
		ProviderID:      "ghostty",
		ProviderVersion: 1,
		Platform:        PlatformDarwin,
		BundleID:        "com.mitchellh.ghostty",
		ExecutableName:  "ghostty",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid identity rejected: %v", err)
	}

	cases := []TerminalIdentity{
		{},
		{ProviderID: "Ghostty", ProviderVersion: 1, Platform: PlatformDarwin, BundleID: "com.mitchellh.ghostty", ExecutableName: "ghostty"},
		{ProviderID: "ghostty", ProviderVersion: 0, Platform: PlatformDarwin, BundleID: "com.mitchellh.ghostty", ExecutableName: "ghostty"},
		{ProviderID: "ghostty", ProviderVersion: 1, Platform: "plan9", BundleID: "com.mitchellh.ghostty", ExecutableName: "ghostty"},
		{ProviderID: "ghostty", ProviderVersion: 1, Platform: PlatformDarwin, BundleID: "", ExecutableName: "ghostty"},
		{ProviderID: "ghostty", ProviderVersion: 1, Platform: PlatformDarwin, BundleID: "com.mitchellh.ghostty", ExecutableName: "/Applications/Ghostty.app/Contents/MacOS/ghostty"},
		{ProviderID: "ghostty", ProviderVersion: 1, Platform: PlatformLinux, ExecutableName: "/usr/bin/ghostty"},
	}
	for i, tc := range cases {
		if err := tc.Validate(); err == nil {
			t.Fatalf("case %d unexpectedly valid: %+v", i, tc)
		}
	}
}

func TestEvidenceValidateAndFreshAtUseExplicitFreshnessData(t *testing.T) {
	now := time.Date(2026, 8, 19, 15, 0, 0, 0, time.UTC)
	e := Evidence{
		Identity: TerminalIdentity{
			ProviderID:      "ghostty",
			ProviderVersion: 1,
			Platform:        PlatformDarwin,
			BundleID:        "com.mitchellh.ghostty",
			ExecutableName:  "ghostty",
		},
		Source:     SourceRecent,
		ObservedAt: now.Add(-time.Minute),
		FreshUntil: now,
		Quality:    QualityNative,
	}
	if err := e.Validate(); err != nil {
		t.Fatalf("valid evidence rejected: %v", err)
	}
	if !e.FreshAt(now) {
		t.Fatal("evidence must remain fresh through FreshUntil")
	}
	if e.FreshAt(now.Add(time.Nanosecond)) {
		t.Fatal("evidence must expire after FreshUntil")
	}

	bad := []Evidence{
		{Identity: e.Identity, Source: "request_origin_terminal", ObservedAt: e.ObservedAt, FreshUntil: e.FreshUntil, Quality: e.Quality},
		{Identity: e.Identity, Source: e.Source, ObservedAt: time.Time{}, FreshUntil: e.FreshUntil, Quality: e.Quality},
		{Identity: e.Identity, Source: e.Source, ObservedAt: e.ObservedAt, FreshUntil: time.Time{}, Quality: e.Quality},
		{Identity: e.Identity, Source: e.Source, ObservedAt: now, FreshUntil: now.Add(-time.Second), Quality: e.Quality},
		{Identity: e.Identity, Source: e.Source, ObservedAt: e.ObservedAt, FreshUntil: e.FreshUntil, Quality: "guessed"},
	}
	for i, tc := range bad {
		if err := tc.Validate(); err == nil {
			t.Fatalf("bad evidence %d unexpectedly valid: %+v", i, tc)
		}
	}
}
