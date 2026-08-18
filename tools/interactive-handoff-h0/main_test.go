package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCollectIdentityRequiresAbsoluteExecutableAndHashesExactBytes(t *testing.T) {
	dir := t.TempDir()
	tmuxPath := filepath.Join(dir, "tmux")
	content := []byte("#!/bin/sh\necho 'tmux H0-test'\n")
	if err := os.WriteFile(tmuxPath, content, 0o700); err != nil {
		t.Fatal(err)
	}
	id, err := collectIdentity(tmuxPath)
	if err != nil {
		t.Fatal(err)
	}
	if id.TmuxPath != tmuxPath || id.TmuxVersion != "tmux H0-test" {
		t.Fatalf("identity=%#v", id)
	}
	sum := sha256.Sum256(content)
	if id.TmuxSHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("sha=%q", id.TmuxSHA256)
	}
	if len(id.GitHead) != 40 || id.GOOS == "" || id.GOARCH == "" || id.GoVersion == "" {
		t.Fatalf("incomplete identity=%#v", id)
	}
	if _, err := collectIdentity("relative/tmux"); err == nil {
		t.Fatal("relative tmux path accepted")
	}
}

func TestRenderAndVerifyGateBindExactPlatformReportBytes(t *testing.T) {
	dir := t.TempDir()
	reports := qualifiedNativeReports(t)
	inputs := make([]string, 0, len(reports))
	for i := range reports {
		path := filepath.Join(dir, reports[i].Report.GOOS+"-report.json")
		b, err := marshalDeterministic(reports[i].Report)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, b, 0o600); err != nil {
			t.Fatal(err)
		}
		inputs = append(inputs, path)
	}
	gatePath := filepath.Join(dir, "gate.json")
	markdownPath := filepath.Join(dir, "gate.md")
	args := []string{"render"}
	for _, path := range inputs {
		args = append(args, "--input", path)
	}
	args = append(args, "--gate-json", gatePath, "--markdown", markdownPath)
	if err := runCLI(args); err != nil {
		t.Fatal(err)
	}
	if err := runCLI([]string{"verify-gate", "--gate-json", gatePath}); err != nil {
		t.Fatalf("fresh gate failed verification: %v", err)
	}
	md, err := os.ReadFile(markdownPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(md), "H1_ALLOWED: `true`") {
		t.Fatalf("markdown missing derived allow: %s", md)
	}

	f, err := os.OpenFile(inputs[0], os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(" \n"); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := runCLI([]string{"verify-gate", "--gate-json", gatePath}); err == nil {
		t.Fatal("modified bound report bytes accepted")
	}
}

func qualifiedNativeReports(t *testing.T) []BoundReport {
	t.Helper()
	reports := passingNativeReports(t)
	for i := range reports {
		qualifyProviderFacts(&reports[i].Report, candidatePerSessionObserver)
		reports[i] = bindReport(t, reports[i].Report)
	}
	return reports
}

func TestNativeProbeRegistryHasP0ThroughP15(t *testing.T) {
	for _, id := range requiredProbeIDs() {
		if nativeProbeRegistry[id] == nil {
			t.Fatalf("missing native probe %s", id)
		}
	}
	if len(nativeProbeRegistry) != len(requiredProbeIDs()) {
		t.Fatalf("registry size=%d want %d", len(nativeProbeRegistry), len(requiredProbeIDs()))
	}
}

func TestRunNativeProbesCallsP0ThroughP15ExactlyOnceInOrder(t *testing.T) {
	var called []string
	registry := map[string]nativeProbeFunc{}
	for _, id := range requiredProbeIDs() {
		id := id
		registry[id] = func(_ context.Context, _ nativeProbeEnv) ProbeResult {
			called = append(called, id)
			return ProbeResult{ID: id, Status: StatusPass, Summary: "pass"}
		}
	}
	results := runNativeProbes(nativeProbeEnv{}, registry)
	if len(results) != 16 || strings.Join(called, ",") != strings.Join(requiredProbeIDs(), ",") {
		t.Fatalf("called=%v results=%v", called, results)
	}
}

func TestValidateRunPathsRejectsOutsideH0BuildRoot(t *testing.T) {
	repo := t.TempDir()
	insideRaw := filepath.Join(repo, ".build", "interactive-handoff-h0", "darwin")
	insideJSON := filepath.Join(insideRaw, "report.json")
	if _, _, err := validateRunPaths(repo, insideRaw, insideJSON); err != nil {
		t.Fatalf("inside paths rejected: %v", err)
	}
	if _, _, err := validateRunPaths(repo, filepath.Join(repo, "tmp"), insideJSON); err == nil {
		t.Fatal("raw dir outside H0 build root accepted")
	}
	if _, _, err := validateRunPaths(repo, insideRaw, filepath.Join(repo, "report.json")); err == nil {
		t.Fatal("report path outside H0 build root accepted")
	}
}

func TestValidateRunPathsRejectsSymlinkEscape(t *testing.T) {
	repo := t.TempDir()
	outside := t.TempDir()
	build := filepath.Join(repo, ".build")
	if err := os.MkdirAll(build, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(build, "interactive-handoff-h0")); err != nil {
		t.Fatal(err)
	}
	raw := filepath.Join(repo, ".build", "interactive-handoff-h0", "darwin")
	if _, _, err := validateRunPaths(repo, raw, filepath.Join(raw, "report.json")); err == nil {
		t.Fatal("symlink escape below H0 build root accepted")
	}
}
