package capability

import (
	"reflect"
	"testing"
)

func TestVerificationSemanticsCapabilityIsExplicitVersionedAndCloned(t *testing.T) {
	base := Baseline(Limits{})
	if base.Features[FeatureVerificationSemantics] != Unavailable || base.VerificationSemantics != nil {
		t.Fatalf("baseline overclaims verification: %#v", base.VerificationSemantics)
	}
	support := VerificationSemanticsSupport{SchemaVersions: []int{1}, PolicySchemaVersions: []int{1}, MaxDomains: 16, MaxRelations: 512, MaxObligations: 256, MaxPolicyGaps: 128, MaxPolicyRules: 128, MaxClassifications: 128, MaxEvidenceRequirementsPerRule: 32}
	got := base.WithVerificationSemantics(support)
	if got.Features[FeatureVerificationSemantics] != Available || got.VerificationSemantics == nil || !reflect.DeepEqual(*got.VerificationSemantics, support) {
		t.Fatalf("got=%#v", got.VerificationSemantics)
	}
	clone := got.Clone()
	clone.VerificationSemantics.SchemaVersions[0] = 99
	if got.VerificationSemantics.SchemaVersions[0] != 1 {
		t.Fatal("clone aliased support slices")
	}
}

func TestVerificationSemanticsCapabilityRejectsInvalidLimits(t *testing.T) {
	support := VerificationSemanticsSupport{SchemaVersions: []int{1}, PolicySchemaVersions: []int{1}, MaxDomains: 16, MaxRelations: 512, MaxObligations: 256, MaxPolicyGaps: 128, MaxPolicyRules: 128, MaxClassifications: 128, MaxEvidenceRequirementsPerRule: 0}
	got := Baseline(Limits{}).WithVerificationSemantics(support)
	if got.Features[FeatureVerificationSemantics] != Unavailable || got.VerificationSemantics != nil {
		t.Fatal("invalid support advertised")
	}
}

func TestVerificationSemanticsSupportV2AdvertisesV1DecodeCompatibility(t *testing.T) {
	support := VerificationSemanticsSupport{SchemaVersions: []int{1, 2}, PolicySchemaVersions: []int{1}, MaxDomains: 16, MaxRelations: 512, MaxObligations: 256, MaxPolicyGaps: 128, MaxPolicyRules: 128, MaxClassifications: 128, MaxEvidenceRequirementsPerRule: 32}
	catalog := Baseline(Limits{}).WithVerificationSemantics(support)
	if catalog.VerificationSemantics == nil || len(catalog.VerificationSemantics.SchemaVersions) != 2 || catalog.VerificationSemantics.SchemaVersions[0] != 1 || catalog.VerificationSemantics.SchemaVersions[1] != 2 {
		t.Fatalf("v2 support not advertised: %#v", catalog.VerificationSemantics)
	}
}
