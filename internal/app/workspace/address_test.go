package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestAddressResolvesNestedDefaultAndInternalSymlink(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "src", "pkg")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "src"), filepath.Join(root, "inside")); err != nil {
		t.Fatal(err)
	}
	service, ws, _ := addressFixture(t, root)
	for _, tt := range []struct{ cwd, want string }{{"", root}, {".", root}, {"src/pkg", nested}, {"inside/pkg", nested}} {
		got, err := service.ResolveAddress(context.Background(), core.Address{WorkspaceID: ws.ID, CWD: tt.cwd})
		if err != nil {
			t.Fatalf("ResolveAddress(%q): %v", tt.cwd, err)
		}
		canonicalWant, err := filepath.EvalSymlinks(tt.want)
		if err != nil {
			t.Fatal(err)
		}
		if got.CWD != canonicalWant || got.WorkspaceID != ws.ID || got.LogicalCWD == "" {
			t.Fatalf("got=%#v want cwd=%q", got, canonicalWant)
		}
	}
}

func TestAddressRejectsUnknownWorkspaceAndEscapingSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	service, ws, _ := addressFixture(t, root)
	if _, err := service.ResolveAddress(context.Background(), core.Address{WorkspaceID: core.WorkspaceID("ws_01K00000000000000000000009")}); !errors.Is(err, ErrWorkspaceNotFound) {
		t.Fatalf("unknown err=%v", err)
	}
	if _, err := service.ResolveAddress(context.Background(), core.Address{WorkspaceID: ws.ID, CWD: "escape"}); !errors.Is(err, ErrAddressEscape) {
		t.Fatalf("escape err=%v", err)
	}
}

func TestAddressReconcilesMovedWorktreeBeforeFirstAdmission(t *testing.T) {
	oldRoot := filepath.Join(t.TempDir(), "old")
	newRoot := filepath.Join(t.TempDir(), "moved")
	if err := os.MkdirAll(newRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	service, ws, git := addressFixture(t, oldRoot)
	git.resolvedRoots[ws.GitDir] = newRoot
	got, err := service.ResolveAddress(context.Background(), core.Address{WorkspaceID: ws.ID})
	if err != nil {
		t.Fatal(err)
	}
	canonicalNewRoot, err := filepath.EvalSymlinks(newRoot)
	if err != nil {
		t.Fatal(err)
	}
	if got.CWD != canonicalNewRoot {
		t.Fatalf("resolved=%#v", got)
	}
	updated, err := service.Inspect(context.Background(), string(ws.ID))
	if err != nil {
		t.Fatal(err)
	}
	if updated.Root != newRoot {
		t.Fatalf("registry was not reconciled: %#v", updated)
	}
}

func TestExplicitAdmissionAddressClassifiesMissingRegisteredRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "gone")
	service, ws, git := addressFixture(t, root)
	delete(git.resolvedRoots, ws.GitDir)

	_, err := service.ResolveAdmissionAddress(context.Background(), core.Address{WorkspaceID: ws.ID})
	if !errors.Is(err, ErrWorkspaceRootMissing) {
		t.Fatalf("err=%v", err)
	}
	var stateErr *WorkspaceStateError
	if !errors.As(err, &stateErr) || stateErr.WorkspaceID != ws.ID || stateErr.Reason != "root_missing" {
		t.Fatalf("stateErr=%#v err=%v", stateErr, err)
	}
}

func TestExplicitAdmissionAddressClassifiesRegisteredRootReusedByDifferentWorktree(t *testing.T) {
	root := t.TempDir()
	service, ws, git := addressFixture(t, root)
	delete(git.resolvedRoots, ws.GitDir)
	git.observe(root, GitObservation{CommonDir: filepath.Join(root, ".git-new"), Root: root, GitDir: filepath.Join(root, ".git-new"), Branch: "main"})

	_, err := service.ResolveAdmissionAddress(context.Background(), core.Address{WorkspaceID: ws.ID})
	if !errors.Is(err, ErrWorkspaceStale) {
		t.Fatalf("err=%v", err)
	}
	var stateErr *WorkspaceStateError
	if !errors.As(err, &stateErr) || stateErr.WorkspaceID != ws.ID || stateErr.Reason != "root_mismatch" {
		t.Fatalf("stateErr=%#v err=%v", stateErr, err)
	}
}

func TestCWDOnlyAdmissionRecoversFromStaleOverlappingRegistryRecord(t *testing.T) {
	root := t.TempDir()
	service, stale, git := addressFixture(t, root)
	delete(git.resolvedRoots, stale.GitDir)
	newGitDir := filepath.Join(root, ".git-new")
	git.observe(root, GitObservation{CommonDir: newGitDir, Root: root, GitDir: newGitDir, Branch: "main"})
	git.resolvedRoots[newGitDir] = root

	got, err := service.ResolveAdmissionAddress(context.Background(), core.Address{CWD: root})
	if err != nil {
		t.Fatal(err)
	}
	if got.WorkspaceID == "" || got.WorkspaceID == stale.ID {
		t.Fatalf("stale=%s got=%#v", stale.ID, got)
	}
	workspaces, err := service.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(workspaces) != 2 {
		t.Fatalf("workspaces=%#v", workspaces)
	}
}

func TestAdmissionAddressLazyRegistersCWDAndReusesRegistryWithoutRediscovery(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	nested := filepath.Join(root, "pkg", "sub")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	gitDir := filepath.Join(root, ".git")
	registry := newMemoryRegistry()
	git := newFakeGit()
	git.observe(nested, GitObservation{CommonDir: gitDir, Root: root, GitDir: gitDir, Branch: "main"})
	git.resolvedRoots[gitDir] = root
	service := New(registry, git)

	first, err := service.ResolveAdmissionAddress(ctx, core.Address{CWD: nested})
	if err != nil {
		t.Fatal(err)
	}
	canonicalNested, err := filepath.EvalSymlinks(nested)
	if err != nil {
		t.Fatal(err)
	}
	if first.WorkspaceID == "" || first.CWD != canonicalNested || first.LogicalCWD != filepath.Join("pkg", "sub") {
		t.Fatalf("first=%#v", first)
	}
	if len(registry.repositories) != 1 || len(registry.workspaces) != 1 || git.inspectCalls != 1 {
		t.Fatalf("repositories=%d workspaces=%d inspectCalls=%d", len(registry.repositories), len(registry.workspaces), git.inspectCalls)
	}

	second, err := service.ResolveAdmissionAddress(ctx, core.Address{CWD: nested})
	if err != nil {
		t.Fatal(err)
	}
	if second.WorkspaceID != first.WorkspaceID || second.CWD != first.CWD || second.LogicalCWD != first.LogicalCWD {
		t.Fatalf("first=%#v second=%#v", first, second)
	}
	if git.inspectCalls != 1 {
		t.Fatalf("registered fast path rediscovered git: inspectCalls=%d", git.inspectCalls)
	}
}

func TestAdmissionAddressOutsideGitRemainsUnregistered(t *testing.T) {
	root := t.TempDir()
	service := New(newMemoryRegistry(), newFakeGit())
	got, err := service.ResolveAdmissionAddress(context.Background(), core.Address{CWD: root})
	if err != nil {
		t.Fatal(err)
	}
	if got.WorkspaceID != "" || got.CWD != root || got.LogicalCWD != root {
		t.Fatalf("got=%#v", got)
	}
}

func addressFixture(t *testing.T, root string) (*Service, core.Workspace, *fakeGit) {
	t.Helper()
	now := time.Now().UTC()
	registry := newMemoryRegistry()
	repo := core.Repository{SchemaVersion: core.SchemaVersion, ID: core.RepositoryID("repo_01K00000000000000000000000"), CommonDir: filepath.Join(filepath.Dir(root), ".git-common"), CreatedAt: now, LastSeenAt: now}
	ws := core.Workspace{SchemaVersion: core.SchemaVersion, ID: core.WorkspaceID("ws_01K00000000000000000000000"), RepositoryID: repo.ID, Label: "address", Root: root, GitDir: filepath.Join(repo.CommonDir, "worktrees", "address"), CreatedAt: now, LastSeenAt: now}
	registry.repositories[repo.ID] = repo
	registry.workspaces[ws.ID] = ws
	git := newFakeGit()
	git.resolvedRoots[ws.GitDir] = root
	return New(registry, git), ws, git
}
