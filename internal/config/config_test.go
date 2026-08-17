package config

import (
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultsValidate(t *testing.T) {
	c := Defaults()
	if c.MaxConcurrentSessions != 4 || c.MaxSessionOutputBytes != 268435456 || c.ControlReserveSessionBytes != 1048576 {
		t.Fatalf("defaults=%#v", c)
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.MaxConcurrentSessions = 0
	if err := c.Validate(); err == nil {
		t.Fatal("zero capacity accepted")
	}
}

func TestResolvePaths(t *testing.T) {
	p, err := ResolvePaths("linux", 42, "/home/u", map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if p.RuntimeDir != "/tmp/shellbeam-42" {
		t.Fatalf("runtime=%s", p.RuntimeDir)
	}
	if _, err := ResolvePaths("windows", 1, "/x", nil); err == nil {
		t.Fatal("windows accepted")
	}
}

func TestExperimentalCheckpointsConfigIsOptInStrictAndPublic(t *testing.T) {
	defaults := Defaults()
	defaultJSON, err := json.Marshal(defaults)
	if err != nil {
		t.Fatal(err)
	}
	var public map[string]any
	if err := json.Unmarshal(defaultJSON, &public); err != nil {
		t.Fatal(err)
	}
	if got, ok := public["experimental_checkpoints"]; !ok || got != false {
		t.Fatalf("default experimental_checkpoints=%v present=%v json=%s", got, ok, defaultJSON)
	}
	if strings.Contains(string(defaultJSON), "checkpoint-content") || strings.Contains(string(defaultJSON), "private_root") {
		t.Fatalf("public config leaked provider-private path: %s", defaultJSON)
	}

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("schema_version = 1\nmax_concurrent_sessions = 4\nexperimental_checkpoints = true\n"), 0o600); err != nil {
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
	if err := json.Unmarshal(enabledJSON, &public); err != nil {
		t.Fatal(err)
	}
	if got := public["experimental_checkpoints"]; got != true {
		t.Fatalf("enabled experimental_checkpoints=%v json=%s", got, enabledJSON)
	}
	defaultHash := sha256.Sum256(defaultJSON)
	enabledHash := sha256.Sum256(enabledJSON)
	if defaultHash == enabledHash {
		t.Fatal("experimental checkpoint config did not change public config hash")
	}

	unknown := filepath.Join(t.TempDir(), "unknown.toml")
	if err := os.WriteFile(unknown, []byte("schema_version = 1\nmax_concurrent_sessions = 4\nexperimental_checkpoint_private_root = '/tmp/nope'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(unknown, Overrides{}); err == nil {
		t.Fatal("unknown/private checkpoint config key was accepted")
	}
}
