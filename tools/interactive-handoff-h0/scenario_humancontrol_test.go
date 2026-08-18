package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestH0P8WritableHumanControlIsShellIndependent(t *testing.T) {
	result := runHumanControlProbeForTest(t, "P8", probeP8WritableHumanControl, 30*time.Second)
	assertProbePass(t, result)
	want := map[string]string{
		"signal_transport":              "tmux_wait-for",
		"foreground_child_received_key": "false",
		"shell_prompt_required":         "false",
		"shell_command_fallback":        "pane_stdin_not_control_plane",
	}
	for key, value := range want {
		if result.Facts[key] != value {
			t.Fatalf("P8 fact %s=%q want %q; all=%#v", key, result.Facts[key], value, result.Facts)
		}
	}
}

func TestH0P9ReadOnlyDetachToLocalControlIsReachable(t *testing.T) {
	result := runHumanControlProbeForTest(t, "P9", probeP9ReadOnlyLocalControl, 30*time.Second)
	assertProbePass(t, result)
	want := map[string]string{
		"arbitrary_binding_while_readonly": "blocked",
		"detach_while_readonly":            "reachable",
		"local_actions":                    "resume,status,terminate",
		"ingress_proxy_introduced":         "false",
		"pane_control_bytes_injected":      "false",
	}
	for key, value := range want {
		if result.Facts[key] != value {
			t.Fatalf("P9 fact %s=%q want %q; all=%#v", key, result.Facts[key], value, result.Facts)
		}
	}
}

func runHumanControlProbeForTest(t *testing.T, id string, probe nativeProbeFunc, timeout time.Duration) ProbeResult {
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
	t.Logf("%s raw:\n%s", id, raw)
	return result
}
