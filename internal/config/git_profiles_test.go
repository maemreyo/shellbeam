package config

import (
	gitidentity "github.com/maemreyo/shellbeam/internal/core/gitidentity"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitIdentityProfilesValidateBindings(t *testing.T) {
	cfg := Config{
		GitProfiles:           map[string]gitidentity.Profile{"work": {CommitEmails: []string{"dev@company.example"}}},
		GitRepositoryProfiles: map[string]string{"repo_01K00000000000000000000000": "work"},
		GitWorkspaceProfiles:  map[string]string{"ws_01K00000000000000000000000": "work"},
	}
	if err := cfg.ValidateGitIdentityProfiles(); err != nil {
		t.Fatal(err)
	}
	cfg.GitWorkspaceProfiles["ws_01K00000000000000000000000"] = "missing"
	if err := cfg.ValidateGitIdentityProfiles(); err == nil {
		t.Fatal("missing profile binding accepted")
	}
}

func TestLoadRejectsUnknownGitIdentityProfileBinding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	data := []byte(`schema_version = 1
max_concurrent_sessions = 4

[git_workspace_profiles]
ws = "missing"
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path, Overrides{}); err == nil {
		t.Fatal("unknown Git identity profile binding loaded")
	}
}

func TestGitIdentityProfileValidationRejectsMalformedNamesAndBindingIDs(t *testing.T) {
	profile := gitidentity.Profile{CommitEmails: []string{"dev@company.example"}}
	for _, name := range []string{string([]byte{98, 97, 100, 10, 110, 97, 109, 101}), strings.Repeat("x", 129)} {
		cfg := Config{GitProfiles: map[string]gitidentity.Profile{name: profile}}
		if err := cfg.ValidateGitIdentityProfiles(); err == nil {
			t.Fatalf("unsafe profile name accepted: %q", name)
		}
	}
	for _, cfg := range []Config{
		{GitProfiles: map[string]gitidentity.Profile{"work": profile}, GitRepositoryProfiles: map[string]string{"repo": "work"}},
		{GitProfiles: map[string]gitidentity.Profile{"work": profile}, GitWorkspaceProfiles: map[string]string{"ws": "work"}},
	} {
		if err := cfg.ValidateGitIdentityProfiles(); err == nil {
			t.Fatalf("malformed binding ID accepted: %#v", cfg)
		}
	}
}
