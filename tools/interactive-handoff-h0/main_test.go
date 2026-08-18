package main

import (
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
		reports[i].Report.Results[indexOf(reports[i].Report.Results, "P3")].Facts = map[string]string{"input_fence_mechanism": "same_client_readonly_fence"}
		reports[i].Report.Results[indexOf(reports[i].Report.Results, "P4")].Facts = map[string]string{"observation_topology": "per_session_observer"}
		reports[i] = bindReport(t, reports[i].Report)
	}
	return reports
}
