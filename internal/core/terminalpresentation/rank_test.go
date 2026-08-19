package terminalpresentation

import (
	"testing"
	"time"
)

func TestRankUsesFrozenResolverPrecedence(t *testing.T) {
	now := time.Date(2026, 8, 19, 15, 0, 0, 0, time.UTC)
	ghostty := testIdentity("ghostty", "com.mitchellh.ghostty")
	wezterm := testIdentity("wezterm", "com.github.wez.wezterm")
	fallback := testIdentity("fallback", "com.example.fallback")

	tests := []rankCase{
		{
			name: "existing exact client beats every presentation hint",
			candidates: []Candidate{
				testCandidate(ghostty, SourceRecent, now.Add(-time.Second), now.Add(time.Minute)),
				testCandidate(wezterm, SourceActive, now, now.Add(time.Minute)),
				testCandidate(ghostty, SourceExistingClient, now.Add(-2*time.Second), now.Add(time.Minute)),
			},
			want: "ghostty",
		},
		{
			name: "active supported terminal beats recent",
			candidates: []Candidate{
				testCandidate(ghostty, SourceRecent, now, now.Add(time.Minute)),
				testCandidate(wezterm, SourceActive, now.Add(-time.Second), now.Add(time.Minute)),
			},
			want: "wezterm",
		},
		{
			name: "frontmost browser contributes no terminal candidate so recent survives",
			candidates: []Candidate{
				testCandidate(ghostty, SourceRecent, now.Add(-time.Second), now.Add(time.Minute)),
				testCandidate(wezterm, SourceBridgeAffinity, now, now.Add(time.Minute)),
			},
			want: "ghostty",
		},
		{
			name: "fresh bridge affinity beats single running",
			candidates: []Candidate{
				testCandidate(ghostty, SourceBridgeAffinity, now.Add(-time.Second), now.Add(time.Minute)),
				testCandidate(wezterm, SourceSingleRunning, now, now.Add(time.Minute)),
			},
			want: "ghostty",
		},
		{
			name: "single running beats qualified fallback",
			candidates: []Candidate{
				testCandidate(wezterm, SourceSingleRunning, now, now.Add(time.Minute)),
				testCandidate(fallback, SourceFallback, now, now.Add(time.Minute)),
			},
			want: "wezterm",
		},
		{
			name: "multiple distinct single-running identities are ambiguous and fall through",
			candidates: []Candidate{
				testCandidate(ghostty, SourceSingleRunning, now, now.Add(time.Minute)),
				testCandidate(wezterm, SourceSingleRunning, now, now.Add(time.Minute)),
				testCandidate(fallback, SourceFallback, now, now.Add(time.Minute)),
			},
			want: "fallback",
		},
		{
			name: "stale bridge affinity is rejected instead of becoming timeless preference",
			candidates: []Candidate{
				testCandidate(ghostty, SourceBridgeAffinity, now.Add(-2*time.Minute), now.Add(-time.Nanosecond)),
				testCandidate(wezterm, SourceSingleRunning, now, now.Add(time.Minute)),
			},
			want: "wezterm",
		},
	}

	assertRankCases(t, now, tests)
}

type rankCase struct {
	name       string
	candidates []Candidate
	want       string
}

func assertRankCases(t *testing.T, now time.Time, tests []rankCase) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Rank(now, tt.candidates)
			if err != nil {
				t.Fatalf("Rank() error: %v", err)
			}
			if got.Selected == nil {
				t.Fatal("Rank() selected no candidate")
			}
			if got.Selected.Evidence.Identity.ProviderID != tt.want {
				t.Fatalf("selected provider=%q want=%q", got.Selected.Evidence.Identity.ProviderID, tt.want)
			}
		})
	}
}

func TestRankTieBreaksByNewestObservationThenStableProviderID(t *testing.T) {
	now := time.Date(2026, 8, 19, 15, 0, 0, 0, time.UTC)
	alpha := testIdentity("alpha", "com.example.alpha")
	beta := testIdentity("beta", "com.example.beta")

	got, err := Rank(now, []Candidate{
		testCandidate(alpha, SourceRecent, now.Add(-2*time.Second), now.Add(time.Minute)),
		testCandidate(beta, SourceRecent, now.Add(-time.Second), now.Add(time.Minute)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Selected == nil || got.Selected.Evidence.Identity.ProviderID != "beta" {
		t.Fatalf("newest tie-break selected %+v", got.Selected)
	}

	got, err = Rank(now, []Candidate{
		testCandidate(beta, SourceRecent, now, now.Add(time.Minute)),
		testCandidate(alpha, SourceRecent, now, now.Add(time.Minute)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Selected == nil || got.Selected.Evidence.Identity.ProviderID != "alpha" {
		t.Fatalf("stable provider tie-break selected %+v", got.Selected)
	}
}

func TestRankRejectsMalformedEvidenceAndReturnsNoSelectionWhenNothingFresh(t *testing.T) {
	now := time.Date(2026, 8, 19, 15, 0, 0, 0, time.UTC)
	ghostty := testIdentity("ghostty", "com.mitchellh.ghostty")

	_, err := Rank(now, []Candidate{{Evidence: Evidence{
		Identity:   ghostty,
		Source:     SourceRecent,
		ObservedAt: now,
		FreshUntil: now.Add(time.Minute),
		Quality:    "guessed",
	}}})
	if err == nil {
		t.Fatal("malformed evidence must fail closed")
	}

	got, err := Rank(now, []Candidate{
		testCandidate(ghostty, SourceRecent, now.Add(-time.Minute), now.Add(-time.Second)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Selected != nil {
		t.Fatalf("expired evidence selected: %+v", got.Selected)
	}
}

func testIdentity(providerID, bundleID string) TerminalIdentity {
	return TerminalIdentity{
		ProviderID:      providerID,
		ProviderVersion: 1,
		Platform:        PlatformDarwin,
		BundleID:        bundleID,
		ExecutableName:  providerID,
	}
}

func testCandidate(identity TerminalIdentity, source EvidenceSource, observedAt, freshUntil time.Time) Candidate {
	quality := QualityNative
	switch source {
	case SourceExistingClient:
		quality = QualityExact
	case SourceBridgeAffinity:
		quality = QualityValidated
	case SourceFallback:
		quality = QualityQualified
	}
	return Candidate{Evidence: Evidence{
		Identity:   identity,
		Source:     source,
		ObservedAt: observedAt,
		FreshUntil: freshUntil,
		Quality:    quality,
	}}
}
