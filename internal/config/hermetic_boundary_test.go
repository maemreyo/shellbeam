package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHermeticBoundaryConfigIsExplicitOptInWithoutProviderPrivatePaths(t *testing.T) {
	defaults := Defaults()
	if defaults.ExperimentalHermeticBoundary {
		t.Fatal("hermetic boundary must default off")
	}
	encoded, err := json.Marshal(defaults)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"experimental_hermetic_boundary":false`) {
		t.Fatalf("public opt-in missing: %s", encoded)
	}
	for _, forbidden := range []string{"bubblewrap_path", "toolchain_root", "security_policy_path", "capture_root", "private_root"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("public config leaked private hermetic key %q: %s", forbidden, encoded)
		}
	}

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("schema_version = 1\nmax_concurrent_sessions = 4\nexperimental_hermetic_boundary = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path, Overrides{})
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.ExperimentalHermeticBoundary {
		t.Fatal("explicit hermetic opt-in was not loaded")
	}

	unknown := filepath.Join(t.TempDir(), "unknown.toml")
	if err := os.WriteFile(unknown, []byte("schema_version = 1\nmax_concurrent_sessions = 4\nhermetic_toolchain_root = '/private/toolchain'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(unknown, Overrides{}); err == nil {
		t.Fatal("provider-private hermetic config key was accepted")
	}
}
