package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCandidateModeSelection(t *testing.T) {
	const exactModule = "v0.0.0-20260623181947-01eb4420fa68"
	cases := []struct {
		version    string
		experiment string
		module     string
		want       string
		ok         bool
	}{
		{"go1.26.5", "", exactModule, "go1.26-pinned-json-library-boundary", true},
		{"go1.26.5", "jsonv2", exactModule, "", false},
		{"go1.26.5", "other", exactModule, "", false},
		{"go1.26.5", "", "v0.0.0-deadbeef", "", false},
		{"go1.25.9", "", exactModule, "", false},
		{"go1.27.0", "", exactModule, "", false},
	}
	for _, tt := range cases {
		got, ok := candidateMode(tt.version, tt.experiment, tt.module)
		if got != tt.want || ok != tt.ok {
			t.Fatalf("candidateMode(%q,%q,%q)=(%q,%t), want (%q,%t)", tt.version, tt.experiment, tt.module, got, ok, tt.want, tt.ok)
		}
	}
}

func TestRunWritesCompletePassingReport(t *testing.T) {
	out := filepath.Join(t.TempDir(), "report.json")
	if err := run("testdata/fixtures.json", out); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var rep report
	if err := json.Unmarshal(raw, &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Verdict != "PASS" || rep.ExitStatus != 0 {
		t.Fatalf("verdict=%s exit=%d", rep.Verdict, rep.ExitStatus)
	}
	wantMode, ok := candidateMode(runtime.Version(), os.Getenv("GOEXPERIMENT"), rep.ModuleVersion)
	if !ok || rep.CandidateMode != wantMode {
		t.Fatalf("candidate=%q want=%q runtime=%q experiment=%q", rep.CandidateMode, wantMode, runtime.Version(), os.Getenv("GOEXPERIMENT"))
	}
	if rep.FixtureManifestSHA256 == "" || rep.Command == "" || rep.GoVersionCommand == "" || rep.ModuleVersion == "" {
		t.Fatalf("missing provenance: %+v", rep)
	}
	wantFragment := "go run ./tools/rich-media-parser-gate -fixtures testdata/fixtures.json -out "
	if !strings.Contains(rep.Command, wantFragment) {
		t.Fatalf("command=%q missing stable logical fragment %q", rep.Command, wantFragment)
	}
	if rep.GoExperiment != "" {
		t.Fatalf("library-boundary report has global experiment %q", rep.GoExperiment)
	}
	if strings.Contains(rep.Command, "GOEXPERIMENT=") {
		t.Fatalf("library-boundary command falsely advertises experiment: %q", rep.Command)
	}
	if strings.Contains(rep.Command, "/go-build") || strings.Contains(rep.Command, "/Caches/go-build") {
		t.Fatalf("command leaks nondeterministic executable path: %q", rep.Command)
	}
	if rep.GoExperiment != os.Getenv("GOEXPERIMENT") || rep.CGOEnabled == "" {
		t.Fatalf("go env incomplete: %+v", rep)
	}
	if len(rep.InvalidChecks) != 5 {
		t.Fatalf("invalid checks=%d", len(rep.InvalidChecks))
	}
	if len(rep.ValidV2Checks) < 12 {
		t.Fatalf("valid checks=%d", len(rep.ValidV2Checks))
	}
	for _, c := range append(append([]checkResult{}, rep.InvalidChecks...), rep.ValidV2Checks...) {
		if c.Status != "PASS" {
			t.Fatalf("check failed: %+v", c)
		}
	}
}

func TestRunRejectsFrozenSemanticDigestMismatch(t *testing.T) {
	raw, err := os.ReadFile("testdata/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	valid := m["valid_v2"].([]any)
	valid[0].(map[string]any)["semantic_sha256"] = "0000000000000000000000000000000000000000000000000000000000000000"
	bad, _ := json.Marshal(m)
	fixture := filepath.Join(t.TempDir(), "fixtures.json")
	if err := os.WriteFile(fixture, bad, 0644); err != nil {
		t.Fatal(err)
	}
	if err := run(fixture, filepath.Join(t.TempDir(), "report.json")); err == nil {
		t.Fatal("expected digest mismatch failure")
	}
}
