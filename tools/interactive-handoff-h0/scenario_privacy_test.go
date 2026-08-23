package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestH0P4PrivacyScopeAndCandidateTopology(t *testing.T) {
	result := runPrivacyProbeForTest(t, "P4", probeP4PrivacyScope, 45*time.Second)
	assertProbePass(t, result)
	t.Logf("%s facts=%#v", result.ID, result.Facts)
	passCount := 0
	for _, candidate := range privacyCandidateNames() {
		status := result.Facts["candidate."+candidate+".p4"]
		if status != "PASS" && status != "FAIL" {
			t.Fatalf("P4 candidate %s not measured: %#v", candidate, result.Facts)
		}
		if status == "PASS" {
			passCount++
		}
	}
	if passCount == 0 {
		t.Fatalf("P4 has no eligible candidate: %#v", result.Facts)
	}
	if result.Facts["no_output_scope"] != "control_client" {
		t.Fatalf("P4 no-output scope=%q facts=%#v", result.Facts["no_output_scope"], result.Facts)
	}
	if result.Facts["observation_topology"] != "unqualified" {
		t.Fatalf("Task 3 prematurely selected topology: %#v", result.Facts)
	}
}

func TestH0P5PrivateFromFirstByteForEveryP4EligibleCandidate(t *testing.T) {
	result := runPrivacyProbeForTest(t, "P5", probeP5PrivateFromFirstByte, 60*time.Second)
	assertProbePass(t, result)
	t.Logf("%s facts=%#v", result.ID, result.Facts)
	passCount := 0
	for _, candidate := range privacyCandidateNames() {
		status := result.Facts["candidate."+candidate+".p5"]
		if status == "PASS" {
			passCount++
		} else if status != "FAIL" && status != "NOT_ELIGIBLE_P4" {
			t.Fatalf("P5 candidate %s invalid status %q facts=%#v", candidate, status, result.Facts)
		}
	}
	if passCount == 0 {
		t.Fatalf("P5 has no first-byte-private candidate: %#v", result.Facts)
	}
}

func TestH0P6ReconnectDoesNotReplayPrivateHistory(t *testing.T) {
	result := runPrivacyProbeForTest(t, "P6", probeP6ReconnectNoReplay, 75*time.Second)
	assertProbePass(t, result)
	t.Logf("%s facts=%#v", result.ID, result.Facts)
	passCount := 0
	for _, candidate := range privacyCandidateNames() {
		status := result.Facts["candidate."+candidate+".p6"]
		if status == "PASS" {
			passCount++
		} else if status != "FAIL" && status != "NOT_ELIGIBLE_P5" {
			t.Fatalf("P6 candidate %s invalid status %q facts=%#v", candidate, status, result.Facts)
		}
	}
	if passCount == 0 {
		t.Fatalf("P6 has no reconnect-safe candidate: %#v", result.Facts)
	}
	if result.Facts["capture_pane_used"] != "false" {
		t.Fatalf("P6 used forbidden history recovery: %#v", result.Facts)
	}
}

func TestH0P7AttachmentPreservesDelegatedEnvironment(t *testing.T) {
	result := runPrivacyProbeForTest(t, "P7", probeP7EnvironmentPreservation, 30*time.Second)
	assertProbePass(t, result)
	t.Logf("%s facts=%#v", result.ID, result.Facts)
	if result.Facts["negative_control_without_E"] != "mutated" || result.Facts["attach_with_E"] != "preserved" || result.Facts["switch_with_E"] != "preserved" {
		t.Fatalf("P7 facts=%#v", result.Facts)
	}
}

func runPrivacyProbeForTest(t *testing.T, id string, probe nativeProbeFunc, timeout time.Duration) ProbeResult {
	t.Helper()
	tmuxPath := requireH0Tmux(t)
	rawDir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	result := probe(ctx, nativeProbeEnv{Tmux: tmuxPath, RawDir: rawDir})
	rawPath := filepath.Join(rawDir, id+".txt")
	raw, err := os.ReadFile(rawPath)
	if err != nil {
		t.Fatalf("%s raw transcript: %v", id, err)
	}
	if strings.Contains(string(raw), "REAL_") {
		t.Fatalf("%s raw transcript contains unexpected real-secret marker", id)
	}
	if id == "P4" || id == "P5" || id == "P6" {
		t.Logf("%s raw:\n%s", id, string(raw))
	}
	return result
}

func TestGatedSecretProducerCommandIsValidShell(t *testing.T) {
	cmd := gatedSecretProducerCommand("/tmp/h0 gate", "A_SECRET_RACE")
	if strings.Contains(cmd, "''A_SECRET") {
		t.Fatalf("producer has nested quote corruption: %s", cmd)
	}
	check := exec.Command("/bin/sh", "-n", "-c", cmd)
	if out, err := check.CombinedOutput(); err != nil {
		t.Fatalf("producer shell syntax invalid: %v: %s\n%s", err, out, cmd)
	}
}
