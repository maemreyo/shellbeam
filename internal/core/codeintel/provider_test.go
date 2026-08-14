package codeintel

import (
	"strings"
	"testing"
)

func TestProviderMetadataKeepsCompatibilityDimensionsSeparate(t *testing.T) {
	meta := ProviderMetadata{
		ProviderID:           "go_semantic",
		Incarnation:          "provider_01K00000000000000000000000",
		ExecutableVersion:    "gopls v0.20.0",
		ConfigFingerprint:    "cfg_v1_example",
		BuildFingerprint:     "build_v1_example",
		BuildQuality:         "observed",
		Coverage:             SyncExactForKnownPaths,
		SemanticScopeQuality: "partial",
	}
	if err := meta.Validate(); err != nil {
		t.Fatalf("valid provider metadata rejected: %v", err)
	}
}

func TestProviderMetadataRejectsPrivateOrUnboundedFieldsByVocabulary(t *testing.T) {
	for _, coverage := range []SyncCoverage{
		SyncExactForKnownPaths,
		SyncProviderManaged,
		SyncPartial,
		SyncUnknown,
	} {
		if err := coverage.Validate(); err != nil {
			t.Fatalf("coverage %q rejected: %v", coverage, err)
		}
	}
	if err := SyncCoverage("per_path_sync_map").Validate(); err == nil {
		t.Fatal("adapter-private sync vocabulary accepted")
	}

	meta := ProviderMetadata{
		ProviderID:  "go_semantic",
		Incarnation: "provider_01K00000000000000000000000",
		Coverage:    SyncUnknown,
	}
	meta.ProviderID = strings.Repeat("x", MaxProviderTextBytes+1)
	if err := meta.Validate(); err == nil {
		t.Fatal("oversized provider metadata accepted")
	}
}
