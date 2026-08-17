package config

import (
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestE27InputTraceExperimentalInputTracingConfigIsOptInStrictAndPublic(t *testing.T) {
	defaults := Defaults()
	defaultJSON, err := json.Marshal(defaults)
	if err != nil {
		t.Fatal(err)
	}
	var public map[string]any
	if err := json.Unmarshal(defaultJSON, &public); err != nil {
		t.Fatal(err)
	}
	if got, ok := public["experimental_input_tracing"]; !ok || got != false {
		t.Fatalf("default experimental_input_tracing=%v present=%v json=%s", got, ok, defaultJSON)
	}
	if strings.Contains(string(defaultJSON), "input-trace-content") || strings.Contains(string(defaultJSON), "private_root") {
		t.Fatalf("public config leaked provider-private path: %s", defaultJSON)
	}

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("schema_version = 1\nmax_concurrent_sessions = 4\nexperimental_input_tracing = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	enabled, err := Load(path, Overrides{})
	if err != nil {
		t.Fatal(err)
	}
	enabledJSON, err := json.Marshal(enabled)
	if err != nil {
		t.Fatal(err)
	}
	public = nil
	if err := json.Unmarshal(enabledJSON, &public); err != nil {
		t.Fatal(err)
	}
	if got := public["experimental_input_tracing"]; got != true {
		t.Fatalf("enabled experimental_input_tracing=%v json=%s", got, enabledJSON)
	}
	if sha256.Sum256(defaultJSON) == sha256.Sum256(enabledJSON) {
		t.Fatal("experimental input tracing config did not change public config hash")
	}

	unknown := filepath.Join(t.TempDir(), "unknown.toml")
	if err := os.WriteFile(unknown, []byte("schema_version = 1\nmax_concurrent_sessions = 4\nexperimental_input_trace_private_root = '/tmp/nope'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(unknown, Overrides{}); err == nil {
		t.Fatal("unknown/private input trace config key was accepted")
	}
}
