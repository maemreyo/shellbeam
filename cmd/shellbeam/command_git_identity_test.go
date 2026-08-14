package main

import (
	"context"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
	"path/filepath"
	"testing"
)

func TestParseGitIdentityArgs(t *testing.T) {
	got, err := parseGitIdentityArgs([]string{"work", "--effect", "pr", "--deep", "--json"})
	if err != nil {
		t.Fatal(err)
	}
	if got.target != "work" || got.effect != "pr" || !got.deep || !got.json {
		t.Fatalf("got=%#v", got)
	}
	for _, effect := range []string{"push", "pr", "tag", "release", "publish", "verify"} {
		if _, err := parseGitIdentityArgs([]string{"--effect", effect}); err != nil {
			t.Fatalf("effect %s rejected: %v", effect, err)
		}
	}
	if _, err := parseGitIdentityArgs([]string{"--effect", "deploy"}); err == nil {
		t.Fatal("invalid effect accepted")
	}
}

type fakeWorkspaceLister struct{ values []workspace.Workspace }

func (f fakeWorkspaceLister) ListWorkspaces(context.Context) ([]workspace.Workspace, error) {
	return f.values, nil
}

func TestIdentityWorkspaceLookupUsesLabelAndMostSpecificCurrentRoot(t *testing.T) {
	base := t.TempDir()
	nested := filepath.Join(base, "nested")
	records := []workspace.Workspace{
		{ID: "ws_01K00000000000000000000000", Label: "root", Root: base},
		{ID: "ws_01K00000000000000000000001", Label: "nested", Root: nested},
	}
	lookup := identityWorkspaceLookup{store: fakeWorkspaceLister{values: records}}
	got, err := lookup.Inspect(context.Background(), "nested")
	if err != nil || got.Label != "nested" {
		t.Fatalf("got=%#v err=%v", got, err)
	}
	got, err = workspaceContaining(records, filepath.Join(nested, "src"))
	if err != nil || got.Label != "nested" {
		t.Fatalf("contained=%#v err=%v", got, err)
	}
}
