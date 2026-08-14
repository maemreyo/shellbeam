package workspace

import (
	"testing"
	"time"
)

func TestWorkspaceRecordAcceptsUnconventionalLabelButRejectsControls(t *testing.T) {
	now := time.Now().UTC()
	record := Workspace{
		SchemaVersion: 1, ID: WorkspaceID("ws_01K00000000000000000000000"),
		RepositoryID: RepositoryID("repo_01K00000000000000000000000"),
		Label:        "review/foo: odd-but-usable", Root: "/tmp/worktree", GitDir: "/tmp/repo/.git/worktrees/foo",
		CreatedAt: now, LastSeenAt: now,
	}
	if err := record.Validate(); err != nil {
		t.Fatalf("unconventional label rejected: %v", err)
	}
	record.Label = "bad\nlabel"
	if err := record.Validate(); err == nil {
		t.Fatal("accepted control character in label")
	}
}

func TestRepositoryRecordRequiresAbsoluteCommonDir(t *testing.T) {
	now := time.Now().UTC()
	record := Repository{SchemaVersion: 1, ID: RepositoryID("repo_01K00000000000000000000000"), CommonDir: "/tmp/repo/.git", CreatedAt: now, LastSeenAt: now}
	if err := record.Validate(); err != nil {
		t.Fatal(err)
	}
	record.CommonDir = "relative/.git"
	if err := record.Validate(); err == nil {
		t.Fatal("accepted relative common dir")
	}
}
