package capability

import (
	"reflect"
	"testing"
)

func TestCatalogBaselineIsHonestAboutCurrentSupport(t *testing.T) {
	limits := Limits{
		CommandBytes:       32768,
		ResponseBytes:      262144,
		SessionOutputBytes: 268435456,
		RuntimeMS:          86400000,
		LiveSessions:       4,
		ActivityHistory:    0,
	}
	catalog := Baseline(limits)
	if catalog.ProtocolVersion != 2 {
		t.Fatalf("protocol=%d", catalog.ProtocolVersion)
	}
	if !reflect.DeepEqual(catalog.ReceiptSchemaVersions, []int{1, 2}) {
		t.Fatalf("receipt versions=%v", catalog.ReceiptSchemaVersions)
	}
	if len(catalog.ManifestVersions) != 0 {
		t.Fatalf("manifest advertised before implementation: %v", catalog.ManifestVersions)
	}
	if !reflect.DeepEqual(catalog.Limits, limits) {
		t.Fatalf("limits=%#v want %#v", catalog.Limits, limits)
	}
	for _, feature := range TargetFeatures() {
		if got := catalog.Features[feature]; got != Unavailable {
			t.Fatalf("feature %s advertised as %s before implementation", feature, got)
		}
	}
}

func TestCatalogBaselineDoesNotAliasFeatureMaps(t *testing.T) {
	first := Baseline(Limits{})
	second := Baseline(Limits{})
	first.Features[FeatureWorkspaceAddressing] = Available
	if second.Features[FeatureWorkspaceAddressing] != Unavailable {
		t.Fatal("baseline catalogs share mutable feature map")
	}
}
