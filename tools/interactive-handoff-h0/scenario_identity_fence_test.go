package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestH0P0PrivateServerIdentity(t *testing.T) {
	result := runNativeProbeForTest(t, "P0", probeP0PrivateServerIdentity, 15*time.Second)
	assertProbePass(t, result)
	if result.Facts["assume_paste_time"] != "0" {
		t.Fatalf("P0 facts=%#v", result.Facts)
	}
}

func TestH0P1ExactHumanClientIdentity(t *testing.T) {
	result := runNativeProbeForTest(t, "P1", probeP1ExactHumanClientIdentity, 20*time.Second)
	assertProbePass(t, result)
}

func TestH0P2ExactClientFlagIsolation(t *testing.T) {
	result := runNativeProbeForTest(t, "P2", probeP2ExactClientFlagIsolation, 20*time.Second)
	assertProbePass(t, result)
	if result.Facts["client_flag_control"] == "" {
		t.Fatalf("P2 missing control fact: %#v", result.Facts)
	}
}

func TestH0P3SameClientIngressFence(t *testing.T) {
	if testing.Short() {
		t.Skip("P3 stress")
	}
	result := runNativeProbeForTest(t, "P3", probeP3SameClientIngressFence, 90*time.Second)
	assertProbePass(t, result)
	if result.Facts["input_fence_mechanism"] == "" || result.Facts["iterations"] != "1000" {
		t.Fatalf("P3 facts=%#v", result.Facts)
	}
}

func runNativeProbeForTest(t *testing.T, id string, probe nativeProbeFunc, timeout time.Duration) ProbeResult {
	t.Helper()
	tmuxPath := requireH0Tmux(t)
	rawDir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	result := probe(ctx, nativeProbeEnv{Tmux: tmuxPath, RawDir: rawDir})
	if _, err := os.Stat(filepath.Join(rawDir, id+".txt")); err != nil {
		t.Fatalf("%s raw transcript: %v", id, err)
	}
	return result
}

func assertProbePass(t *testing.T, result ProbeResult) {
	t.Helper()
	if result.Status != StatusPass {
		t.Fatalf("%s status=%s summary=%s facts=%#v", result.ID, result.Status, result.Summary, result.Facts)
	}
}

func requireH0Tmux(t *testing.T) string {
	t.Helper()
	path := os.Getenv("SHELLBEAM_H0_TMUX")
	if path == "" {
		t.Skip("SHELLBEAM_H0_TMUX not set")
	}
	if !filepath.IsAbs(path) {
		t.Fatalf("SHELLBEAM_H0_TMUX must be absolute: %q", path)
	}
	return path
}
