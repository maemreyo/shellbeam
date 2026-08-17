//go:build goexperiment.jsonv2

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	if rep.CandidateMode != "go1.26-jsonv2-experiment" {
		t.Fatalf("candidate=%q", rep.CandidateMode)
	}
	if rep.FixtureManifestSHA256 == "" || rep.Command == "" || rep.GoVersionCommand == "" {
		t.Fatalf("missing provenance: %+v", rep)
	}
	wantPrefix := "GOEXPERIMENT=jsonv2 go run ./tools/rich-media-parser-gate -fixtures testdata/fixtures.json -out "
	if !strings.HasPrefix(rep.Command, wantPrefix) {
		t.Fatalf("command=%q want stable logical prefix %q", rep.Command, wantPrefix)
	}
	if strings.Contains(rep.Command, "/go-build") || strings.Contains(rep.Command, "/Caches/go-build") {
		t.Fatalf("command leaks nondeterministic executable path: %q", rep.Command)
	}
	if rep.GoExperiment != "jsonv2" || rep.CGOEnabled == "" {
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
