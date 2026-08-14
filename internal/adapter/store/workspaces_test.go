package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestWorkspaceRegistryRoundTripRenameAndForget(t *testing.T) {
	r, err := Open(filepath.Join(t.TempDir(), "state"), Limits{MaxSessions: 1, MaxSessionOutput: 1024, MaxTotalState: 1 << 20, ControlReserve: 1024})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	repo := core.Repository{SchemaVersion: 1, ID: core.RepositoryID("repo_01K00000000000000000000000"), CommonDir: "/tmp/repo/.git", CreatedAt: now, LastSeenAt: now}
	ws := core.Workspace{SchemaVersion: 1, ID: core.WorkspaceID("ws_01K00000000000000000000000"), RepositoryID: repo.ID, Label: "odd/review: label", Root: "/tmp/worktree", GitDir: "/tmp/repo/.git/worktrees/odd", CreatedAt: now, LastSeenAt: now}
	if err := r.SaveRepository(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	if err := r.SaveWorkspace(context.Background(), ws); err != nil {
		t.Fatal(err)
	}
	repos, err := r.ListRepositories(context.Background())
	if err != nil || len(repos) != 1 || repos[0].ID != repo.ID {
		t.Fatalf("repositories=%#v err=%v", repos, err)
	}
	workspaces, err := r.ListWorkspaces(context.Background())
	if err != nil || len(workspaces) != 1 || workspaces[0].Label != ws.Label {
		t.Fatalf("workspaces=%#v err=%v", workspaces, err)
	}
	ws.Label = "renamed"
	ws.LastSeenAt = now.Add(time.Second)
	if err := r.SaveWorkspace(context.Background(), ws); err != nil {
		t.Fatal(err)
	}
	workspaces, err = r.ListWorkspaces(context.Background())
	if err != nil || len(workspaces) != 1 || workspaces[0].Label != "renamed" {
		t.Fatalf("renamed=%#v err=%v", workspaces, err)
	}
	marker := filepath.Join(t.TempDir(), "must-survive")
	if err := os.WriteFile(marker, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.DeleteWorkspace(context.Background(), ws.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("metadata delete touched unrelated filesystem: %v", err)
	}
	workspaces, err = r.ListWorkspaces(context.Background())
	if err != nil || len(workspaces) != 0 {
		t.Fatalf("after delete=%#v err=%v", workspaces, err)
	}
	if err := r.DeleteWorkspace(context.Background(), ws.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete=%v", err)
	}
}

func TestWorkspaceRegistryListsDeterministically(t *testing.T) {
	r, err := Open(filepath.Join(t.TempDir(), "state"), Limits{MaxSessions: 1, MaxSessionOutput: 1024, MaxTotalState: 1 << 20, ControlReserve: 1024})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	repoID := core.RepositoryID("repo_01K00000000000000000000000")
	for _, ws := range []core.Workspace{
		{SchemaVersion: 1, ID: core.WorkspaceID("ws_01K00000000000000000000002"), RepositoryID: repoID, Label: "zeta", Root: "/tmp/zeta", GitDir: "/tmp/repo/.git/worktrees/zeta", CreatedAt: now, LastSeenAt: now},
		{SchemaVersion: 1, ID: core.WorkspaceID("ws_01K00000000000000000000001"), RepositoryID: repoID, Label: "alpha", Root: "/tmp/alpha", GitDir: "/tmp/repo/.git/worktrees/alpha", CreatedAt: now, LastSeenAt: now},
	} {
		if err := r.SaveWorkspace(context.Background(), ws); err != nil {
			t.Fatal(err)
		}
	}
	got, err := r.ListWorkspaces(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID > got[1].ID {
		t.Fatalf("non-deterministic order=%#v", got)
	}
}
