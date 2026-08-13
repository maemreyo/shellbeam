package buildinfo

import "testing"

func TestCurrentUsesLinkerValues(t *testing.T) {
	oldVersion, oldCommit, oldBuiltAt := version, commit, builtAt
	t.Cleanup(func() { version, commit, builtAt = oldVersion, oldCommit, oldBuiltAt })
	version, commit, builtAt = "v0.1.0-dev", "abc123", "2026-08-13T00:00:00Z"
	got := Current()
	if got.Version != version || got.Commit != commit || got.BuiltAt != builtAt {
		t.Fatalf("Current() = %#v", got)
	}
}
