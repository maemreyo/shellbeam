package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestH0P10ManualResizeIsolation(t *testing.T) {
	result := runOrderingProbeForTest(t, "P10", probeP10ResizeIsolation, 45*time.Second)
	assertProbePass(t, result)
	want := map[string]string{
		"resize_policy":                 "manual_explicit_human_adoption",
		"first_human_size":              "120x40",
		"passive_observer_changed_size": "false",
		"readonly_changed_size":         "false",
		"detach_changed_size":           "false",
		"second_human_size":             "90x30",
	}
	for key, value := range want {
		if result.Facts[key] != value {
			t.Fatalf("P10 fact %s=%q want %q; all=%#v", key, result.Facts[key], value, result.Facts)
		}
	}
}

func TestH0P11ClientLossPreservesIdentityServerLossDoesNot(t *testing.T) {
	result := runOrderingProbeForTest(t, "P11", probeP11CrashReconnectIdentity, 45*time.Second)
	assertProbePass(t, result)
	want := map[string]string{
		"control_client_loss":        "recoverable_same_object_identity",
		"human_client_loss":          "recoverable_same_object_identity",
		"observer_restart":           "recoverable_same_object_identity",
		"server_loss":                "provider_lost",
		"friendly_name_recreation":   "not_continuation",
		"server_incarnation_changed": "true",
	}
	for key, value := range want {
		if result.Facts[key] != value {
			t.Fatalf("P11 fact %s=%q want %q; all=%#v", key, result.Facts[key], value, result.Facts)
		}
	}
}

func TestH0P12PrivacyACKOrderingAndBackpressure(t *testing.T) {
	result := runOrderingProbeForTest(t, "P12", probeP12ACKOrderingAndBackpressure, 60*time.Second)
	assertProbePass(t, result)
	want := map[string]string{
		"during_private_visible":                "false",
		"cross_client_total_ordering_claimed":   "false",
		"all_control_clients_off_backpressures": "true",
		"human_display_prevents_backpressure":   "true",
		"no_output_stops_tmux_reading":          "false",
	}
	for key, value := range want {
		if result.Facts[key] != value {
			t.Fatalf("P12 fact %s=%q want %q; all=%#v", key, result.Facts[key], value, result.Facts)
		}
	}
	if result.Facts["off_ack_command_number"] == "" || result.Facts["on_ack_command_number"] == "" {
		t.Fatalf("P12 missing command ACK numbers: %#v", result.Facts)
	}
}

func runOrderingProbeForTest(t *testing.T, id string, probe nativeProbeFunc, timeout time.Duration) ProbeResult {
	t.Helper()
	tmuxPath := requireH0Tmux(t)
	rawDir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	result := probe(ctx, nativeProbeEnv{Tmux: tmuxPath, RawDir: rawDir})
	raw, err := os.ReadFile(filepath.Join(rawDir, id+".txt"))
	if err != nil {
		t.Fatalf("%s raw transcript: %v", id, err)
	}
	if strings.Contains(string(raw), "REAL_") {
		t.Fatalf("%s raw transcript contains unexpected real-secret marker", id)
	}
	t.Logf("%s raw:\n%s", id, raw)
	return result
}
