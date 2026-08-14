package workspace

import (
	"context"
	"errors"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestSnapshotObserverSelectsMostSpecificRegisteredWorkspace(t *testing.T) {
	now := time.Now().UTC()
	outer := observerWorkspace("ws_01K00000000000000000000000", "/repo", now)
	inner := observerWorkspace("ws_01K00000000000000000000001", "/repo/worktrees/feature", now)
	registry := &observerRegistry{workspaces: []core.Workspace{outer, inner}}
	source := &observerSource{snapshot: core.FastSnapshot{SchemaVersion: core.SnapshotSchemaVersion, Quality: core.QualityFresh, ObservedAt: now}}
	observer := NewObserver(registry, source)

	got := observer.Observe(context.Background(), "/repo/worktrees/feature/internal/pkg")
	if source.calls != 1 || source.last.ID != inner.ID {
		t.Fatalf("calls=%d last=%#v", source.calls, source.last)
	}
	if got.WorkspaceID != inner.ID || got.RepositoryID != inner.RepositoryID {
		t.Fatalf("snapshot=%#v", got)
	}
}

func TestSnapshotObserverUnregisteredWorkspaceIsUnavailableWithoutGitCall(t *testing.T) {
	observer := NewObserver(&observerRegistry{}, &observerSource{})
	got := observer.Observe(context.Background(), "/unregistered/path")
	if got.Quality != core.QualityUnavailable || got.DiagnosticCode != "workspace_unregistered" || got.ObservedAt.IsZero() {
		t.Fatalf("snapshot=%#v", got)
	}
}

func TestSnapshotObserverRegistryFailureIsCauseSafeUnavailable(t *testing.T) {
	source := &observerSource{}
	observer := NewObserver(&observerRegistry{err: errors.New("private /secret/path token=abc")}, source)
	got := observer.Observe(context.Background(), "/repo")
	if got.Quality != core.QualityUnavailable || got.DiagnosticCode != "workspace_registry_unavailable" {
		t.Fatalf("snapshot=%#v", got)
	}
	if source.calls != 0 {
		t.Fatalf("source calls=%d", source.calls)
	}
}

type observerRegistry struct {
	workspaces []core.Workspace
	err        error
}

func (r *observerRegistry) SaveRepository(context.Context, core.Repository) error { return nil }
func (r *observerRegistry) SaveWorkspace(context.Context, core.Workspace) error   { return nil }
func (r *observerRegistry) ListRepositories(context.Context) ([]core.Repository, error) {
	return nil, r.err
}
func (r *observerRegistry) ListWorkspaces(context.Context) ([]core.Workspace, error) {
	return append([]core.Workspace(nil), r.workspaces...), r.err
}
func (r *observerRegistry) DeleteWorkspace(context.Context, core.WorkspaceID) error { return nil }

type observerSource struct {
	snapshot core.FastSnapshot
	calls    int
	last     core.Workspace
}

func (s *observerSource) Snapshot(_ context.Context, workspace core.Workspace) core.FastSnapshot {
	s.calls++
	s.last = workspace
	got := s.snapshot
	got.RepositoryID = workspace.RepositoryID
	got.WorkspaceID = workspace.ID
	return got
}

func observerWorkspace(id, root string, now time.Time) core.Workspace {
	return core.Workspace{SchemaVersion: core.SchemaVersion, ID: core.WorkspaceID(id), RepositoryID: core.RepositoryID("repo_01K00000000000000000000000"), Label: "observer", Root: root, GitDir: root + "/.git", CreatedAt: now, LastSeenAt: now}
}
