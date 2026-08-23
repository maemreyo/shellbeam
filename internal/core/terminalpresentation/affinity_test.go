package terminalpresentation

import (
	"reflect"
	"testing"
	"time"
)

func TestBridgeAffinityHintIsFreshBoundedValidatedEvidence(t *testing.T) {
	now := time.Date(2026, 8, 19, 16, 0, 0, 0, time.UTC)
	identity := TerminalIdentity{ProviderID: "ghostty", ProviderVersion: 1, Platform: PlatformDarwin, BundleID: "com.mitchellh.ghostty", ExecutableName: "ghostty"}
	hint, err := NewBridgeAffinityHint(identity, now, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if hint.EvidenceSource != SourceBridgeAffinity || hint.FreshUntil != now.Add(15*time.Minute) {
		t.Fatalf("hint=%+v", hint)
	}
	evidence, err := hint.Evidence()
	if err != nil {
		t.Fatal(err)
	}
	if evidence.Source != SourceBridgeAffinity || evidence.Quality != QualityValidated || !evidence.FreshAt(now.Add(15*time.Minute)) {
		t.Fatalf("evidence=%+v", evidence)
	}
	if evidence.FreshAt(now.Add(15*time.Minute + time.Nanosecond)) {
		t.Fatal("bridge affinity remained fresh after bound")
	}
}

func TestBridgeAffinityHintRejectsTimelessAndWrongSourceClaims(t *testing.T) {
	now := time.Date(2026, 8, 19, 16, 0, 0, 0, time.UTC)
	identity := TerminalIdentity{ProviderID: "ghostty", ProviderVersion: 1, Platform: PlatformDarwin, BundleID: "com.mitchellh.ghostty", ExecutableName: "ghostty"}
	if _, err := NewBridgeAffinityHint(identity, now, 0); err == nil {
		t.Fatal("zero freshness accepted")
	}
	if _, err := NewBridgeAffinityHint(identity, now, MaxBridgeAffinityFreshness+time.Nanosecond); err == nil {
		t.Fatal("timeless affinity accepted")
	}
	bad := BridgeAffinityHint{Identity: identity, ObservedAt: now, FreshUntil: now.Add(time.Minute), EvidenceSource: SourceRecent}
	if err := bad.Validate(); err == nil {
		t.Fatal("recent evidence mislabeled as bridge affinity accepted")
	}
}

func TestBridgeAffinityTerminologyAndPrecedenceRemainLowPriority(t *testing.T) {
	typeOf := reflect.TypeOf(BridgeAffinityHint{})
	for i := 0; i < typeOf.NumField(); i++ {
		if typeOf.Field(i).Name == "RequestOriginTerminal" {
			t.Fatal("forbidden request-origin terminology exported")
		}
	}
	now := time.Date(2026, 8, 19, 16, 0, 0, 0, time.UTC)
	identity := TerminalIdentity{ProviderID: "ghostty", ProviderVersion: 1, Platform: PlatformDarwin, BundleID: "com.mitchellh.ghostty", ExecutableName: "ghostty"}
	hint, _ := NewBridgeAffinityHint(identity, now, time.Minute)
	bridgeEvidence, _ := hint.Evidence()
	active := Evidence{Identity: identity, Source: SourceActive, ObservedAt: now.Add(-time.Second), FreshUntil: now.Add(time.Minute), Quality: QualityNative}
	got, err := Rank(now, []Candidate{{Evidence: bridgeEvidence}, {Evidence: active}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Selected == nil || got.Selected.Evidence.Source != SourceActive {
		t.Fatalf("selected=%+v", got.Selected)
	}
}
