package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestSnapshotCleanModifiedUntrackedRenamedAndDetachedFixtures(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, string)
		check func(*testing.T, core.FastSnapshot)
	}{
		{name: "clean", check: func(t *testing.T, got core.FastSnapshot) {
			if got.Dirty.Dirty || got.Detached || got.Head == "" || got.Ref == "" {
				t.Fatalf("clean snapshot=%#v", got)
			}
		}},
		{name: "modified", setup: func(t *testing.T, repo string) {
			if err := os.WriteFile(filepath.Join(repo, "README"), []byte("modified\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, check: func(t *testing.T, got core.FastSnapshot) {
			if !got.Dirty.Dirty || got.Dirty.Modified != 1 {
				t.Fatalf("modified snapshot=%#v", got)
			}
		}},
		{name: "untracked", setup: func(t *testing.T, repo string) {
			if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("new\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, check: func(t *testing.T, got core.FastSnapshot) {
			if !got.Dirty.Dirty || got.Dirty.Untracked != 1 {
				t.Fatalf("untracked snapshot=%#v", got)
			}
		}},
		{name: "renamed", setup: func(t *testing.T, repo string) { runGit(t, repo, "mv", "README", "RENAMED") }, check: func(t *testing.T, got core.FastSnapshot) {
			if !got.Dirty.Dirty || got.Dirty.Renamed != 1 {
				t.Fatalf("renamed snapshot=%#v", got)
			}
		}},
		{name: "detached", setup: func(t *testing.T, repo string) { runGit(t, repo, "checkout", "--detach", "HEAD") }, check: func(t *testing.T, got core.FastSnapshot) {
			if !got.Detached || got.Ref != "" {
				t.Fatalf("detached snapshot=%#v", got)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := initRepository(t)
			if tt.setup != nil {
				tt.setup(t, repo)
			}
			got := snapshotRepository(t, repo)
			if got.Quality != core.QualityFresh || got.Generation == "" {
				t.Fatalf("snapshot=%#v", got)
			}
			tt.check(t, got)
		})
	}
}

func TestSnapshotAheadBehindUsesOnlyConfiguredLocalUpstream(t *testing.T) {
	t.Run("missing upstream", func(t *testing.T) {
		got := snapshotRepository(t, initRepository(t))
		if got.Upstream != "" || got.UpstreamQuality != core.QualityUnavailable {
			t.Fatalf("snapshot=%#v", got)
		}
	})
	t.Run("remote tracking ref is stale quality", func(t *testing.T) {
		repo := initRepository(t)
		base := strings.TrimSpace(runGitOutput(t, repo, "rev-parse", "HEAD"))
		runGit(t, repo, "remote", "add", "origin", repo)
		runGit(t, repo, "update-ref", "refs/remotes/origin/main", base)
		runGit(t, repo, "branch", "--set-upstream-to=origin/main", "main")
		if err := os.WriteFile(filepath.Join(repo, "ahead.txt"), []byte("ahead\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		runGit(t, repo, "add", "ahead.txt")
		runGit(t, repo, "commit", "-m", "ahead")
		got := snapshotRepository(t, repo)
		if got.Upstream != "origin/main" || got.Ahead != 1 || got.Behind != 0 || got.UpstreamQuality != core.QualityStale {
			t.Fatalf("snapshot=%#v", got)
		}
	})
}

func snapshotRepository(t *testing.T, path string) core.FastSnapshot {
	t.Helper()
	adapter := New()
	workspace := observedWorkspace(t, adapter, path)
	got := adapter.Snapshot(context.Background(), workspace)
	if got.Quality != core.QualityFresh {
		t.Fatalf("quality=%q diagnostic=%q", got.Quality, got.DiagnosticCode)
	}
	return got
}

func observedWorkspace(t *testing.T, adapter *Repository, path string) core.Workspace {
	t.Helper()
	observation, err := adapter.Inspect(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	return core.Workspace{SchemaVersion: core.SchemaVersion, ID: core.WorkspaceID("ws_01K00000000000000000000000"), RepositoryID: core.RepositoryID("repo_01K00000000000000000000000"), Label: "fixture", Root: observation.Root, GitDir: observation.GitDir, Branch: observation.Branch, CreatedAt: now, LastSeenAt: now}
}
