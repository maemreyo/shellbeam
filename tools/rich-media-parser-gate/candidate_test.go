//go:build goexperiment.jsonv2

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
	cases := []struct {
		version    string
		experiment string
		want       string
		ok         bool
	}{
		{"go1.26.5-X:jsonv2", "jsonv2", "go1.26-jsonv2-experiment", true},
		{"go1.26.5", "", "", false},
		{"go1.27rc2", "", "go1.27-jsonv2-preview", true},
		{"go1.27.0", "", "go1.27-stable-jsonv2", true},
		{"go1.27.1", "", "go1.27-stable-jsonv2", true},
		{"go1.27.0", "jsonv2", "", false},
	}
	for _, tt := range cases {
		got, ok := candidateMode(tt.version, tt.experiment)
		if got != tt.want || ok != tt.ok {
			t.Fatalf("candidateMode(%q,%q)=(%q,%t), want (%q,%t)", tt.version, tt.experiment, got, ok, tt.want, tt.ok)
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
	wantMode, ok := candidateMode(runtime.Version(), os.Getenv("GOEXPERIMENT"))
	if !ok || rep.CandidateMode != wantMode {
		t.Fatalf("candidate=%q want=%q runtime=%q experiment=%q", rep.CandidateMode, wantMode, runtime.Version(), os.Getenv("GOEXPERIMENT"))
	}
	if rep.FixtureManifestSHA256 == "" || rep.Command == "" || rep.GoVersionCommand == "" {
		t.Fatalf("missing provenance: %+v", rep)
	}
	wantFragment := "go run ./tools/rich-media-parser-gate -fixtures testdata/fixtures.json -out "
	if !strings.Contains(rep.Command, wantFragment) {
		t.Fatalf("command=%q missing stable logical fragment %q", rep.Command, wantFragment)
	}
	if rep.GoExperiment == "jsonv2" && !strings.Contains(rep.Command, "GOEXPERIMENT=jsonv2 ") {
		t.Fatalf("experimental command missing explicit mode: %q", rep.Command)
	}
	if rep.GoExperiment == "" && strings.Contains(rep.Command, "GOEXPERIMENT=jsonv2 ") {
		t.Fatalf("stable/preview command falsely advertises experiment: %q", rep.Command)
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
