package project

import (
	"strings"
	"testing"
)

func TestParameterFingerprintIsDeterministicAndSourceSensitive(t *testing.T) {
	first := []ParameterBinding{
		{ID: "package", Kind: ParameterRepoPackage, Value: "./internal/app", Source: BindingSourceCaller, ProviderID: "go-repo-package", ProviderVersion: 1},
		{ID: "count", Kind: ParameterInteger, Value: "3", Source: BindingSourceDefault},
	}
	second := []ParameterBinding{first[1], first[0]}
	a, err := ParameterFingerprint(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := ParameterFingerprint(second)
	if err != nil || a != b || len(a) != 64 {
		t.Fatalf("fingerprints a=%q b=%q err=%v", a, b, err)
	}
	changed := append([]ParameterBinding(nil), first...)
	changed[1].Source = BindingSourceCaller
	c, err := ParameterFingerprint(changed)
	if err != nil || c == a {
		t.Fatalf("source change did not change fingerprint c=%q err=%v", c, err)
	}
}

func TestCommandBindingValidationAndDigest(t *testing.T) {
	binding := validCommandBinding(t)
	if err := binding.Validate(); err != nil {
		t.Fatal(err)
	}
	first, err := binding.Digest()
	if err != nil || len(first) != 64 {
		t.Fatalf("digest=%q err=%v", first, err)
	}
	copy := binding
	copy.ResolvedArgv = append([]string(nil), binding.ResolvedArgv...)
	second, err := copy.Digest()
	if err != nil || second != first {
		t.Fatalf("stable digest first=%q second=%q err=%v", first, second, err)
	}
	copy.ResolvedArgv[2] = "./other"
	third, err := copy.Digest()
	if err != nil || third == first {
		t.Fatalf("argv change did not alter digest third=%q err=%v", third, err)
	}
}

func TestCommandBindingRejectsMalformedFrozenFacts(t *testing.T) {
	base := validCommandBinding(t)
	cases := map[string]func(*CommandBinding){
		"schema":                func(v *CommandBinding) { v.SchemaVersion = 99 },
		"manifest digest":       func(v *CommandBinding) { v.ManifestDigest = "bad" },
		"manifest schema":       func(v *CommandBinding) { v.ManifestSchemaVersion = ManifestSchemaV1 },
		"command id":            func(v *CommandBinding) { v.CommandID = "../bad" },
		"parameter digest":      func(v *CommandBinding) { v.ParameterFingerprint = "bad" },
		"argv empty":            func(v *CommandBinding) { v.ResolvedArgv = nil },
		"argv token empty":      func(v *CommandBinding) { v.ResolvedArgv[0] = "" },
		"logical cwd":           func(v *CommandBinding) { v.LogicalCWD = "../escape" },
		"resolved cwd":          func(v *CommandBinding) { v.ResolvedCWD = "relative" },
		"path quality":          func(v *CommandBinding) { v.PathObservationQuality = "confined" },
		"bad source generation": func(v *CommandBinding) { v.SourceGeneration = "secret" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			got := base
			got.ResolvedArgv = append([]string(nil), base.ResolvedArgv...)
			got.Parameters = append([]ParameterBinding(nil), base.Parameters...)
			mutate(&got)
			if err := got.Validate(); err == nil {
				t.Fatalf("invalid binding accepted: %#v", got)
			}
		})
	}
}

func validCommandBinding(t *testing.T) CommandBinding {
	t.Helper()
	params := []ParameterBinding{{ID: "package", Kind: ParameterRepoPackage, Value: "./internal/app", Source: BindingSourceCaller, ProviderID: "go-repo-package", ProviderVersion: 1}}
	fingerprint, err := ParameterFingerprint(params)
	if err != nil {
		t.Fatal(err)
	}
	return CommandBinding{
		SchemaVersion:          BindingSchemaVersion,
		ManifestDigest:         strings.Repeat("a", 64),
		ManifestSchemaVersion:  ManifestSchemaV2,
		CommandID:              "test_package",
		ParameterFingerprint:   fingerprint,
		Parameters:             params,
		ResolvedArgv:           []string{"go", "test", "./internal/app"},
		LogicalCWD:             ".",
		ResolvedCWD:            "/repo",
		SourceGeneration:       "gen_" + strings.Repeat("b", 64),
		PathObservationQuality: PathObservationExactAtBind,
	}
}

func TestCommandBindingV1CompatibilityAndV2EvidenceMetadata(t *testing.T) {
	legacy := validCommandBinding(t)
	legacy.SchemaVersion = 1
	legacy.Kind = ""
	legacy.SourceScope = ""
	legacy.ExpectedOutputs = nil
	if err := legacy.Validate(); err != nil {
		t.Fatalf("persisted v1 binding rejected: %v", err)
	}

	v2 := validCommandBinding(t)
	v2.SchemaVersion = 2
	v2.Kind = "test"
	v2.SourceScope = "full"
	v2.ExpectedOutputs = []Output{{Path: "dist/report.json", Kind: "file", Required: true, Digest: "sha256"}}
	if err := v2.Validate(); err != nil {
		t.Fatalf("v2 binding rejected: %v", err)
	}
	first, err := v2.Digest()
	if err != nil {
		t.Fatal(err)
	}
	changed := v2
	changed.ExpectedOutputs = append([]Output(nil), v2.ExpectedOutputs...)
	changed.ExpectedOutputs[0].Path = "dist/other.json"
	second, err := changed.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("expected-output change did not change frozen binding digest")
	}

	legacy.Kind = "test"
	if err := legacy.Validate(); err == nil {
		t.Fatal("schema-v1 binding accepted schema-v2 evidence metadata")
	}
}
