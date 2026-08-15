package project

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestFoldReadinessRequiredChecks(t *testing.T) {
	tests := []struct {
		name   string
		checks []ReadinessCheck
		want   ReadinessState
	}{
		{"none", nil, ReadinessUnavailable},
		{"all satisfied", []ReadinessCheck{
			{ID: "go", Kind: RequirementToolchain, Required: true, Status: CheckCompatible},
			{ID: "git", Kind: RequirementExecutable, Required: true, Status: CheckAvailable},
			{ID: "DATABASE_URL", Kind: RequirementEnvironmentPresence, Required: true, Status: CheckPresentNonEmpty},
		}, ReadinessReady},
		{"known failure", []ReadinessCheck{
			{ID: "git", Kind: RequirementExecutable, Required: true, Status: CheckMissing},
			{ID: "go", Kind: RequirementToolchain, Required: true, Status: CheckUnknown},
		}, ReadinessNotReady},
		{"required unknown", []ReadinessCheck{
			{ID: "git", Kind: RequirementExecutable, Required: true, Status: CheckAvailable},
			{ID: "go", Kind: RequirementToolchain, Required: true, Status: CheckUnavailable},
		}, ReadinessPartial},
		{"optional failure only", []ReadinessCheck{
			{ID: "docker", Kind: RequirementExecutable, Required: false, Status: CheckMissing},
		}, ReadinessReady},
		{"optional unknown does not degrade", []ReadinessCheck{
			{ID: "git", Kind: RequirementExecutable, Required: true, Status: CheckAvailable},
			{ID: "AWS_PROFILE", Kind: RequirementEnvironmentPresence, Required: false, Status: CheckUnknown},
		}, ReadinessReady},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := FoldReadiness(tc.checks); got != tc.want {
				t.Fatalf("FoldReadiness=%q want %q", got, tc.want)
			}
		})
	}
}

func TestReadinessValidationRejectsFabricatedOrMalformedFacts(t *testing.T) {
	base := validReadiness()
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*Readiness){
		"bad state":          func(v *Readiness) { v.State = "green" },
		"bad repository":     func(v *Readiness) { v.RepositoryID = "../repo" },
		"bad workspace":      func(v *Readiness) { v.WorkspaceID = "../workspace" },
		"bad manifest":       func(v *Readiness) { v.ManifestDigest = "bad" },
		"unsupported schema": func(v *Readiness) { v.ManifestSchemaVersion = 99 },
		"bad environment fp": func(v *Readiness) { v.EnvironmentFingerprint = "secret-value" },
		"bad toolchain fp":   func(v *Readiness) { v.ToolchainFingerprint = "not-a-digest" },
		"zero capture":       func(v *Readiness) { v.CapturedAt = time.Time{} },
		"bad cache":          func(v *Readiness) { v.CacheQuality = "stale" },
		"fresh nonzero age":  func(v *Readiness) { v.CacheAgeMS = 1 },
		"negative cache age": func(v *Readiness) { v.CacheQuality, v.CacheAgeMS = CacheCached, -1 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			got := base
			got.Checks = append([]ReadinessCheck(nil), base.Checks...)
			mutate(&got)
			if err := got.Validate(); err == nil {
				t.Fatalf("invalid readiness accepted: %#v", got)
			}
		})
	}
}

func TestReadinessCheckValidationIsKindAware(t *testing.T) {
	valid := []ReadinessCheck{
		{ID: "go", Kind: RequirementToolchain, Required: true, Status: CheckCompatible, ProviderID: "go-host", ProviderVersion: 1},
		{ID: "git", Kind: RequirementExecutable, Required: true, Status: CheckAvailable},
		{ID: "DATABASE_URL", Kind: RequirementEnvironmentPresence, Required: true, Status: CheckPresentNonEmpty},
	}
	for _, check := range valid {
		if err := check.Validate(); err != nil {
			t.Fatalf("valid check %#v: %v", check, err)
		}
	}
	invalid := []ReadinessCheck{
		{ID: "DATABASE_URL", Kind: RequirementExecutable, Required: true, Status: CheckAvailable},
		{ID: "go", Kind: RequirementToolchain, Required: true, Status: CheckPresentNonEmpty},
		{ID: "DATABASE_URL", Kind: RequirementEnvironmentPresence, Required: true, Status: CheckCompatible},
		{ID: "git", Kind: RequirementExecutable, Required: true, Status: CheckUnavailable, ProviderVersion: 1},
		{ID: "git", Kind: "network", Required: true, Status: CheckAvailable},
	}
	for _, check := range invalid {
		if err := check.Validate(); err == nil {
			t.Fatalf("invalid check accepted: %#v", check)
		}
	}
}

func TestReadinessFingerprintsAreDeterministicAndDoNotContainEnvironmentValues(t *testing.T) {
	checksA := []ReadinessCheck{
		{ID: "DATABASE_URL", Kind: RequirementEnvironmentPresence, Required: true, Status: CheckPresentNonEmpty},
		{ID: "go", Kind: RequirementToolchain, Required: true, Status: CheckCompatible, ProviderID: "go-host", ProviderVersion: 1},
	}
	checksB := []ReadinessCheck{checksA[1], checksA[0]}
	first, err := ReadinessChecksFingerprint(checksA)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ReadinessChecksFingerprint(checksB)
	if err != nil || first != second || len(first) != 64 {
		t.Fatalf("fingerprints first=%q second=%q err=%v", first, second, err)
	}
	changed := append([]ReadinessCheck(nil), checksA...)
	changed[0].Status = CheckAbsent
	third, err := ReadinessChecksFingerprint(changed)
	if err != nil || third == first {
		t.Fatalf("status change did not change fingerprint: %q err=%v", third, err)
	}
	encoded, err := json.Marshal(validReadiness())
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"postgres://alice:secret@db", "RAW_ENV_SECRET", "environment_value", "environment_hash"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("readiness serialization leaked forbidden material %q: %s", forbidden, encoded)
		}
	}
}

func validReadiness() Readiness {
	checks := []ReadinessCheck{{ID: "git", Kind: RequirementExecutable, Required: true, Status: CheckAvailable}}
	fingerprint, _ := ReadinessChecksFingerprint(checks)
	return Readiness{
		SchemaVersion:          ReadinessSchemaVersion,
		State:                  ReadinessReady,
		RepositoryID:           "repo_01K00000000000000000000000",
		WorkspaceID:            "ws_01K00000000000000000000000",
		ManifestDigest:         strings.Repeat("a", 64),
		ManifestSchemaVersion:  ManifestSchemaV2,
		EnvironmentFingerprint: fingerprint,
		ToolchainFingerprint:   strings.Repeat("b", 64),
		CapturedAt:             time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC),
		CacheQuality:           CacheFresh,
		Checks:                 checks,
	}
}
