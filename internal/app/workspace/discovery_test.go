package workspace

import (
	"context"
	"errors"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/workspace"
)

// An agent knows where it is working. Until now that was not enough: a
// directory nobody had registered produced no provenance at all, so the agent
// had to learn ShellBeam's workspace ids before its first command could return
// a usable answer. Discovery reports what git can see from the path itself,
// without recording anything -- registration stays something an operator does.

type stubRegistry struct {
	workspaces []core.Workspace
	err        error
}

func (s stubRegistry) ListWorkspaces(context.Context) ([]core.Workspace, error) {
	return s.workspaces, s.err
}
func (s stubRegistry) ListRepositories(context.Context) ([]core.Repository, error) {
	return nil, s.err
}

// The observer only reads; the writing half of the registry is here to satisfy
// the interface and must never be reached, because discovery records nothing.
func (s stubRegistry) SaveRepository(context.Context, core.Repository) error {
	panic("discovery must not write to the registry")
}
func (s stubRegistry) SaveWorkspace(context.Context, core.Workspace) error {
	panic("discovery must not write to the registry")
}
func (s stubRegistry) DeleteWorkspace(context.Context, core.WorkspaceID) error {
	panic("discovery must not write to the registry")
}

// discoveringSource answers both roles the observer needs: it can look at a
// path, and it can describe the workspace that path belongs to.
type discoveringSource struct {
	observation GitObservation
	err         error
	snapshotFor []core.Workspace
}

func (d *discoveringSource) Inspect(_ context.Context, path string) (GitObservation, error) {
	if d.err != nil {
		return GitObservation{}, d.err
	}
	_ = path
	return d.observation, nil
}

func (d *discoveringSource) Snapshot(_ context.Context, workspace core.Workspace) core.FastSnapshot {
	d.snapshotFor = append(d.snapshotFor, workspace)
	return core.FastSnapshot{
		SchemaVersion: core.SchemaVersion,
		Head:          "abc123", Ref: "refs/heads/main", Quality: core.QualityFresh,
	}
}

func observerWith(registry Registry, source SnapshotSource) *Observer {
	observer := NewObserver(registry, source)
	observer.now = func() time.Time { return time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC) }
	return observer
}

// TestUnregisteredDirectoriesStillReportProvenance is the onboarding step this
// removes: a bare cwd is enough to get an answer.
func TestUnregisteredDirectoriesStillReportProvenance(t *testing.T) {
	source := &discoveringSource{observation: GitObservation{
		CommonDir: "/repo/.git", Root: "/repo", GitDir: "/repo/.git", Branch: "main",
	}}
	observer := observerWith(stubRegistry{}, source)

	snapshot := observer.ObserveFresh(context.Background(), "/repo/pkg")
	if snapshot.Head != "abc123" || snapshot.Ref != "refs/heads/main" {
		t.Fatalf("an unregistered directory produced no provenance: %#v", snapshot)
	}
	// It was observed against the discovered worktree, not against nothing.
	if len(source.snapshotFor) != 1 || source.snapshotFor[0].Root != "/repo" {
		t.Fatalf("observed workspace = %#v", source.snapshotFor)
	}
}

// TestDiscoveryClaimsNoRegistration. Identifiers are what registration confers,
// and inventing one would make a directory an agent happened to name look like
// one an operator chose to track.
func TestDiscoveryClaimsNoRegistration(t *testing.T) {
	source := &discoveringSource{observation: GitObservation{
		CommonDir: "/repo/.git", Root: "/repo", GitDir: "/repo/.git", Branch: "main",
	}}
	observer := observerWith(stubRegistry{}, source)

	snapshot := observer.ObserveFresh(context.Background(), "/repo")
	if snapshot.WorkspaceID != "" || snapshot.RepositoryID != "" {
		t.Fatalf("discovery invented identifiers: %q %q", snapshot.WorkspaceID, snapshot.RepositoryID)
	}
	if snapshot.DiagnosticCode != "workspace_unregistered" {
		t.Fatalf("diagnostic = %q, want the caller told this is unregistered", snapshot.DiagnosticCode)
	}
	// And the binding a session records stays empty, so nothing downstream
	// treats this as a registered workspace.
	if binding := observer.Bind(context.Background(), "/repo"); binding.WorkspaceID != "" || binding.RepositoryID != "" {
		t.Fatalf("binding = %#v, want no registered identity", binding)
	}
}

// TestOrdinaryExecutionDoesNotReachForGit.
//
// Binding and cached observation run on the path every command takes. Asking
// git about each directory a command happens to use would be work the caller
// never requested and cannot see, so discovery is confined to the fresh
// observation a caller asks for on purpose.
func TestOrdinaryExecutionDoesNotReachForGit(t *testing.T) {
	source := &countingSource{discoveringSource: discoveringSource{observation: GitObservation{
		CommonDir: "/repo/.git", Root: "/repo", GitDir: "/repo/.git",
	}}}
	observer := observerWith(stubRegistry{}, source)

	observer.Bind(context.Background(), "/repo")
	observer.ObserveCached(context.Background(), "/repo")
	if source.inspects != 0 {
		t.Fatalf("the ordinary path inspected git %d times", source.inspects)
	}

	observer.ObserveFresh(context.Background(), "/repo")
	if source.inspects != 1 {
		t.Fatalf("an explicit fresh observation inspected git %d times, want once", source.inspects)
	}
}

type countingSource struct {
	discoveringSource
	inspects int
}

func (c *countingSource) Inspect(ctx context.Context, path string) (GitObservation, error) {
	c.inspects++
	return c.discoveringSource.Inspect(ctx, path)
}

func (c *countingSource) SnapshotCached(ctx context.Context, workspace core.Workspace) core.FastSnapshot {
	return c.discoveringSource.Snapshot(ctx, workspace)
}

// TestRegistrationRemainsTheAuthority: where a workspace is registered, it wins,
// identifiers and all.
func TestRegistrationRemainsTheAuthority(t *testing.T) {
	registered := core.Workspace{
		SchemaVersion: core.SchemaVersion, ID: "ws_01M04NBVQ4CXHVPCQBJHBMFEKQ",
		RepositoryID: "repo_01M04NBVQ4CXHVPCQBJHBMFEKQ", Root: "/repo", GitDir: "/repo/.git",
	}
	source := &discoveringSource{observation: GitObservation{
		CommonDir: "/elsewhere/.git", Root: "/elsewhere", GitDir: "/elsewhere/.git",
	}}
	observer := observerWith(stubRegistry{workspaces: []core.Workspace{registered}}, source)

	snapshot := observer.ObserveFresh(context.Background(), "/repo/pkg")
	if snapshot.WorkspaceID != registered.ID {
		t.Fatalf("registered workspace was not preferred: %#v", snapshot)
	}
	if snapshot.DiagnosticCode != "" {
		t.Fatalf("a registered workspace was flagged: %q", snapshot.DiagnosticCode)
	}
	if len(source.snapshotFor) != 1 || source.snapshotFor[0].Root != "/repo" {
		t.Fatalf("observed the discovered root instead of the registered one: %#v", source.snapshotFor)
	}
}

// TestDirectoriesOutsideAnyRepositoryStillRefuse. Discovery answers where there
// is something to discover; it does not invent a workspace for a path that is
// not in a repository at all.
func TestDirectoriesOutsideAnyRepositoryStillRefuse(t *testing.T) {
	source := &discoveringSource{err: errors.New("not a git repository")}
	observer := observerWith(stubRegistry{}, source)

	snapshot := observer.ObserveFresh(context.Background(), "/tmp")
	if snapshot.DiagnosticCode != "workspace_unregistered" {
		t.Fatalf("diagnostic = %q", snapshot.DiagnosticCode)
	}
	if snapshot.Head != "" {
		t.Fatalf("a directory outside any repository reported provenance: %#v", snapshot)
	}
}
