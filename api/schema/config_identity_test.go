package schema

import "testing"

func TestConfigV1GitIdentityProfilesContainExpectationsOnly(t *testing.T) {
	schema := resolvedSchema(t, ConfigV1)
	valid := map[string]any{
		"schema_version":          1.0,
		"max_concurrent_sessions": 4.0,
		"git_profiles": map[string]any{
			"work": map[string]any{
				"ssh_host_aliases": []any{"github-work"},
				"remote_owners":    []any{"company-org"},
				"commit_emails":    []any{"dev@company.example"},
				"gh_host":          "github.com",
				"gh_user":          "dev-work",
			},
		},
		"git_workspace_profiles": map[string]any{"ws_01K00000000000000000000000": "work"},
	}
	if err := schema.Validate(valid); err != nil {
		t.Fatalf("expectation-only identity config rejected: %v", err)
	}
	for _, invalid := range []map[string]any{
		{"schema_version": 1.0, "max_concurrent_sessions": 4.0, "git_repository_profiles": map[string]any{"repo": "work"}},
		{"schema_version": 1.0, "max_concurrent_sessions": 4.0, "git_workspace_profiles": map[string]any{"ws": "work"}},
	} {
		if err := schema.Validate(invalid); err == nil {
			t.Fatalf("malformed identity binding key accepted: %v", invalid)
		}
	}
	for _, secretField := range []string{"token", "private_key", "passphrase"} {
		invalid := map[string]any{
			"schema_version":          1.0,
			"max_concurrent_sessions": 4.0,
			"git_profiles":            map[string]any{"work": map[string]any{secretField: "secret"}},
		}
		if err := schema.Validate(invalid); err == nil {
			t.Fatalf("credential field %q accepted", secretField)
		}
	}
}
