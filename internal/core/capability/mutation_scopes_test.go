package capability

import (
	"reflect"
	"testing"
)

func TestA26MutationScopeCapabilityIsExplicitVersionedAndBounded(t *testing.T) {
	base := Baseline(Limits{})
	if base.Features[FeatureMutationScopes] != Unavailable || len(base.MutationScopeSchemaVersions) != 0 {
		t.Fatalf("baseline leaked mutation scope support: %#v", base)
	}
	got := base.WithMutationScopes(16, 64, 16, 256, 32, 900000, 1800000)
	if got.Features[FeatureMutationScopes] != Available {
		t.Fatalf("feature=%q", got.Features[FeatureMutationScopes])
	}
	if !reflect.DeepEqual(got.MutationScopeSchemaVersions, []int{1}) {
		t.Fatalf("versions=%v", got.MutationScopeSchemaVersions)
	}
	limits := got.Limits
	if limits.MutationScopeActivePerActivity != 16 || limits.MutationScopeActivePerWorkspace != 64 || limits.MutationScopePathsPerScope != 16 || limits.MutationScopeSelectorBytes != 256 || limits.MutationScopeAdvisories != 32 || limits.MutationScopeMinTTLMS != 1000 || limits.MutationScopeDefaultTTLMS != 900000 || limits.MutationScopeMaxTTLMS != 1800000 {
		t.Fatalf("limits=%#v", limits)
	}
	if bad := base.WithMutationScopes(0, 64, 16, 256, 32, 900000, 1800000); bad.Features[FeatureMutationScopes] != Unavailable || len(bad.MutationScopeSchemaVersions) != 0 {
		t.Fatal("invalid limits promoted mutation scopes")
	}
	if bad := base.WithMutationScopes(16, 64, 16, 256, 32, 1800000, 900000); bad.Features[FeatureMutationScopes] != Unavailable {
		t.Fatal("invalid ttl ordering promoted mutation scopes")
	}
}

func TestA26MutationScopeCloneDoesNotAliasVersions(t *testing.T) {
	original := Baseline(Limits{}).WithMutationScopes(16, 64, 16, 256, 32, 900000, 1800000)
	clone := original.Clone()
	clone.MutationScopeSchemaVersions[0] = 99
	if original.MutationScopeSchemaVersions[0] != 1 {
		t.Fatal("clone aliased mutation scope versions")
	}
}
