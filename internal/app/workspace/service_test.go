package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestInspectReconcilesWorkspaceAndReturnsTypedMissingRoot(t *testing.T) {
	root := t.TempDir()
	service, ws, git := addressFixture(t, root)
	delete(git.resolvedRoots, ws.GitDir)
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}

	_, err := service.Inspect(context.Background(), string(ws.ID))
	if !errors.Is(err, failure.WorkspaceRootMissing) || !errors.Is(err, ErrWorkspaceRootMissing) {
		t.Fatalf("err=%v", err)
	}
	public := failure.Public(err)
	if public.Details["workspace_id"] != string(ws.ID) || public.Details["reason"] != "root_missing" {
		t.Fatalf("public=%#v", public)
	}
}

func TestInspectReconcilesMovedRegisteredWorkspaceRoot(t *testing.T) {
	oldRoot := t.TempDir()
	newRoot := t.TempDir()
	service, ws, git := addressFixture(t, oldRoot)
	git.resolvedRoots[ws.GitDir] = newRoot

	got, err := service.Inspect(context.Background(), string(ws.ID))
	if err != nil {
		t.Fatal(err)
	}
	if got.Root != newRoot {
		t.Fatalf("got=%#v", got)
	}
}

func TestWorkspaceAttachReusesGitNativeIdentityAcrossMove(t *testing.T) {
	ctx := context.Background()
	registry := newMemoryRegistry()
	git := newFakeGit()
	git.observe("/input/link", GitObservation{CommonDir: "/repo/.git", Root: "/repo", GitDir: "/repo/.git/worktrees/feature", Branch: "feature"})
	git.observe("/moved", GitObservation{CommonDir: "/repo/.git", Root: "/moved", GitDir: "/repo/.git/worktrees/feature", Branch: "feature"})
	service := New(registry, git)

	first, err := service.Attach(ctx, "/input/link", "feature")
	if err != nil {
		t.Fatal(err)
	}
	moved, err := service.Attach(ctx, "/moved", "")
	if err != nil {
		t.Fatal(err)
	}
	if moved.ID != first.ID || moved.RepositoryID != first.RepositoryID {
		t.Fatalf("identity changed after move: first=%#v moved=%#v", first, moved)
	}
	if moved.Root != "/moved" || moved.Label != "feature" {
		t.Fatalf("moved=%#v", moved)
	}
}

func TestWorkspaceAttachReusesRepositoryAndResolvesLabelCollision(t *testing.T) {
	ctx := context.Background()
	registry := newMemoryRegistry()
	git := newFakeGit()
	git.observe("/one", GitObservation{CommonDir: "/repo/.git", Root: "/one", GitDir: "/repo/.git/worktrees/one"})
	git.observe("/two", GitObservation{CommonDir: "/repo/.git", Root: "/two", GitDir: "/repo/.git/worktrees/two"})
	service := New(registry, git)

	first, err := service.Attach(ctx, "/one", "odd/review: label")
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Attach(ctx, "/two", "odd/review: label")
	if err != nil {
		t.Fatal(err)
	}
	if first.RepositoryID != second.RepositoryID {
		t.Fatalf("repository not reused: first=%s second=%s", first.RepositoryID, second.RepositoryID)
	}
	if first.Label != "odd/review: label" {
		t.Fatalf("unconventional label rejected: %q", first.Label)
	}
	if second.Label == first.Label || !strings.HasPrefix(second.Label, first.Label+"-") {
		t.Fatalf("collision was not deterministically suffixed: %q", second.Label)
	}
	again, err := service.Attach(ctx, "/two", "odd/review: label")
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != second.ID || again.Label != second.Label {
		t.Fatalf("reattach changed resolved identity: second=%#v again=%#v", second, again)
	}
}

func TestWorkspaceCreateReusesExistingRefWithoutDuplicateWorktree(t *testing.T) {
	ctx := context.Background()
	registry := newMemoryRegistry()
	git := newFakeGit()
	git.observe("/repo", GitObservation{CommonDir: "/repo/.git", Root: "/repo", GitDir: "/repo/.git", Branch: "main"})
	git.observe("/existing", GitObservation{CommonDir: "/repo/.git", Root: "/existing", GitDir: "/repo/.git/worktrees/existing", Branch: "feature"})
	git.worktrees["/repo/.git"] = []GitWorktree{{Root: "/repo", Branch: "refs/heads/main"}, {Root: "/existing", Branch: "refs/heads/feature"}}
	service := New(registry, git)

	got, err := service.Create(ctx, CreateRequest{Repository: "/repo", Ref: "feature", Label: "feature"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Root != "/existing" || len(git.addCalls) != 0 {
		t.Fatalf("got=%#v addCalls=%#v", got, git.addCalls)
	}
	if git.listCalls != 1 {
		t.Fatalf("worktree list calls=%d want 1", git.listCalls)
	}
}

func TestWorkspaceCreateUsesExplicitPathAndDefaultSiblingTemplate(t *testing.T) {
	ctx := context.Background()
	for _, tt := range []struct {
		name string
		req  CreateRequest
		want string
	}{
		{name: "explicit", req: CreateRequest{Repository: "/src/repo", Ref: "feature", Path: "/chosen/worktree", Label: "feature"}, want: "/chosen/worktree"},
		{name: "default sibling", req: CreateRequest{Repository: "/src/repo", Ref: "feature", Label: "feature"}, want: "/src/repo-worktrees/feature"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			registry := newMemoryRegistry()
			git := newFakeGit()
			git.observe("/src/repo", GitObservation{CommonDir: "/src/repo/.git", Root: "/src/repo", GitDir: "/src/repo/.git", Branch: "main"})
			service := New(registry, git)
			got, err := service.Create(ctx, tt.req)
			if err != nil {
				t.Fatal(err)
			}
			if got.Root != tt.want || len(git.addCalls) != 1 || git.addCalls[0].path != tt.want || git.addCalls[0].ref != "feature" {
				t.Fatalf("got=%#v addCalls=%#v", got, git.addCalls)
			}
		})
	}
}

func TestWorkspaceCreateSuffixesOccupiedUnrelatedCandidateDeterministically(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	repoPath := filepath.Join(base, "repo")
	candidate := filepath.Join(base, "repo-worktrees", "feature")
	if err := os.MkdirAll(candidate, 0o700); err != nil {
		t.Fatal(err)
	}
	registry := newMemoryRegistry()
	git := newFakeGit()
	git.observe(repoPath, GitObservation{CommonDir: filepath.Join(repoPath, ".git"), Root: repoPath, GitDir: filepath.Join(repoPath, ".git"), Branch: "main"})
	service := New(registry, git)

	got, err := service.Create(ctx, CreateRequest{Repository: repoPath, Ref: "feature", Label: "feature"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Root == candidate || !strings.HasPrefix(got.Root, candidate+"-") {
		t.Fatalf("occupied candidate not suffixed: got=%q candidate=%q", got.Root, candidate)
	}
	if len(git.addCalls) != 1 || git.addCalls[0].path != got.Root {
		t.Fatalf("addCalls=%#v got=%#v", git.addCalls, got)
	}
}

func TestWorkspaceCreateReusesIntendedWorktreeOccupyingDefaultCandidate(t *testing.T) {
	ctx := context.Background()
	registry := newMemoryRegistry()
	git := newFakeGit()
	git.observe("/src/repo", GitObservation{CommonDir: "/src/repo/.git", Root: "/src/repo", GitDir: "/src/repo/.git", Branch: "main"})
	candidate := "/src/repo-worktrees/feature"
	git.observe(candidate, GitObservation{CommonDir: "/src/repo/.git", Root: candidate, GitDir: "/src/repo/.git/worktrees/feature", Branch: "feature"})
	git.worktrees["/src/repo/.git"] = []GitWorktree{{Root: "/src/repo", Branch: "refs/heads/main"}, {Root: candidate, Branch: "refs/heads/other"}}
	service := New(registry, git)

	got, err := service.Create(ctx, CreateRequest{Repository: "/src/repo", Ref: "feature", Label: "feature"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Root != candidate || len(git.addCalls) != 0 {
		t.Fatalf("got=%#v addCalls=%#v", got, git.addCalls)
	}
}

func TestWorkspaceRenamePreservesIdentityAndResolvesCollision(t *testing.T) {
	ctx := context.Background()
	registry := newMemoryRegistry()
	git := newFakeGit()
	git.observe("/one", GitObservation{CommonDir: "/repo/.git", Root: "/one", GitDir: "/repo/.git/worktrees/one"})
	git.observe("/two", GitObservation{CommonDir: "/repo/.git", Root: "/two", GitDir: "/repo/.git/worktrees/two"})
	service := New(registry, git)
	first, _ := service.Attach(ctx, "/one", "first")
	second, _ := service.Attach(ctx, "/two", "second")

	renamed, err := service.Rename(ctx, string(second.ID), "first")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.ID != second.ID || renamed.RepositoryID != second.RepositoryID || renamed.Root != second.Root || renamed.GitDir != second.GitDir {
		t.Fatalf("rename changed identity: before=%#v after=%#v", second, renamed)
	}
	if renamed.Label == first.Label || !strings.HasPrefix(renamed.Label, "first-") {
		t.Fatalf("collision label=%q", renamed.Label)
	}
}

func TestForgetOnlyRemovesRegistryMetadata(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	marker := filepath.Join(root, "marker")
	if err := os.WriteFile(marker, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	registry := newMemoryRegistry()
	git := newFakeGit()
	git.observe(root, GitObservation{CommonDir: "/repo/.git", Root: root, GitDir: "/repo/.git/worktrees/keep"})
	service := New(registry, git)
	ws, err := service.Attach(ctx, root, "keep")
	if err != nil {
		t.Fatal(err)
	}

	forgotten, err := service.Forget(ctx, string(ws.ID))
	if err != nil {
		t.Fatal(err)
	}
	if forgotten.ID != ws.ID || len(registry.workspaces) != 0 || len(git.removeCalls) != 0 {
		t.Fatalf("forgotten=%#v workspaces=%#v removeCalls=%#v", forgotten, registry.workspaces, git.removeCalls)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("forget touched filesystem: %v", err)
	}
}

func TestRemoveDirtyRequiresForceThenUsesGitAndForgets(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	registry := newMemoryRegistry()
	git := newFakeGit()
	git.observe(root, GitObservation{CommonDir: "/repo/.git", Root: root, GitDir: "/repo/.git/worktrees/dirty"})
	git.worktrees["/repo/.git"] = []GitWorktree{{Root: root, Branch: "refs/heads/dirty"}}
	git.dirty[root] = true
	service := New(registry, git)
	ws, err := service.Attach(ctx, root, "dirty")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.Remove(ctx, string(ws.ID), false); !errors.Is(err, ErrDirtyWorktree) {
		t.Fatalf("remove dirty err=%v", err)
	}
	if len(git.removeCalls) != 0 || len(registry.workspaces) != 1 {
		t.Fatalf("dirty remove mutated state: calls=%#v workspaces=%#v", git.removeCalls, registry.workspaces)
	}
	removed, err := service.Remove(ctx, string(ws.ID), true)
	if err != nil {
		t.Fatal(err)
	}
	if removed.ID != ws.ID || len(git.removeCalls) != 1 || !git.removeCalls[0].force || len(registry.workspaces) != 0 {
		t.Fatalf("removed=%#v calls=%#v workspaces=%#v", removed, git.removeCalls, registry.workspaces)
	}
}

func TestRemoveCleanUsesGitThenForgets(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	registry := newMemoryRegistry()
	git := newFakeGit()
	git.observe(root, GitObservation{CommonDir: "/repo/.git", Root: root, GitDir: "/repo/.git/worktrees/clean"})
	git.worktrees["/repo/.git"] = []GitWorktree{{Root: root, Branch: "refs/heads/clean"}}
	service := New(registry, git)
	ws, err := service.Attach(ctx, root, "clean")
	if err != nil {
		t.Fatal(err)
	}

	removed, err := service.Remove(ctx, string(ws.ID), false)
	if err != nil {
		t.Fatal(err)
	}
	if removed.ID != ws.ID || len(git.removeCalls) != 1 || git.removeCalls[0].force || len(registry.workspaces) != 0 {
		t.Fatalf("removed=%#v calls=%#v workspaces=%#v", removed, git.removeCalls, registry.workspaces)
	}
}

func TestRemoveFailsClosedForPlainNonWorktreeDirectory(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	marker := filepath.Join(root, "marker")
	if err := os.WriteFile(marker, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	registry := newMemoryRegistry()
	git := newFakeGit()
	git.observe(root, GitObservation{CommonDir: "/repo/.git", Root: root, GitDir: "/repo/.git/worktrees/stale"})
	service := New(registry, git)
	ws, err := service.Attach(ctx, root, "stale")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.Remove(ctx, string(ws.ID), true); !errors.Is(err, ErrUnsafeWorktree) {
		t.Fatalf("remove plain dir err=%v", err)
	}
	if len(git.removeCalls) != 0 || len(registry.workspaces) != 1 {
		t.Fatalf("unsafe remove mutated state: calls=%#v workspaces=%#v", git.removeCalls, registry.workspaces)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("unsafe remove touched filesystem: %v", err)
	}
}

type memoryRegistry struct {
	repositories map[core.RepositoryID]core.Repository
	workspaces   map[core.WorkspaceID]core.Workspace
}

func newMemoryRegistry() *memoryRegistry {
	return &memoryRegistry{repositories: map[core.RepositoryID]core.Repository{}, workspaces: map[core.WorkspaceID]core.Workspace{}}
}

func (m *memoryRegistry) SaveRepository(_ context.Context, record core.Repository) error {
	if err := record.Validate(); err != nil {
		return err
	}
	m.repositories[record.ID] = record
	return nil
}

func (m *memoryRegistry) SaveWorkspace(_ context.Context, record core.Workspace) error {
	if err := record.Validate(); err != nil {
		return err
	}
	m.workspaces[record.ID] = record
	return nil
}

func (m *memoryRegistry) ListRepositories(context.Context) ([]core.Repository, error) {
	out := make([]core.Repository, 0, len(m.repositories))
	for _, record := range m.repositories {
		out = append(out, record)
	}
	return out, nil
}

func (m *memoryRegistry) ListWorkspaces(context.Context) ([]core.Workspace, error) {
	out := make([]core.Workspace, 0, len(m.workspaces))
	for _, record := range m.workspaces {
		out = append(out, record)
	}
	return out, nil
}

func (m *memoryRegistry) DeleteWorkspace(_ context.Context, id core.WorkspaceID) error {
	if _, ok := m.workspaces[id]; !ok {
		return errors.New("not found")
	}
	delete(m.workspaces, id)
	return nil
}

type fakeGit struct {
	observations  map[string]GitObservation
	worktrees     map[string][]GitWorktree
	dirty         map[string]bool
	addCalls      []addCall
	removeCalls   []removeCall
	listCalls     int
	inspectCalls  int
	resolvedRoots map[string]string
}

type addCall struct{ commonDir, path, ref string }
type removeCall struct {
	commonDir string
	path      string
	force     bool
}

func newFakeGit() *fakeGit {
	return &fakeGit{observations: map[string]GitObservation{}, worktrees: map[string][]GitWorktree{}, dirty: map[string]bool{}, resolvedRoots: map[string]string{}}
}

func (f *fakeGit) observe(input string, observation GitObservation) {
	f.observations[input] = observation
}

func (f *fakeGit) Inspect(_ context.Context, path string) (GitObservation, error) {
	f.inspectCalls++
	observation, ok := f.observations[path]
	if !ok {
		return GitObservation{}, errors.New("not a git worktree")
	}
	return observation, nil
}

func (f *fakeGit) ListWorktrees(_ context.Context, commonDir string) ([]GitWorktree, error) {
	f.listCalls++
	return append([]GitWorktree(nil), f.worktrees[commonDir]...), nil
}

func (f *fakeGit) AddWorktree(_ context.Context, commonDir, path, ref string) error {
	f.addCalls = append(f.addCalls, addCall{commonDir: commonDir, path: path, ref: ref})
	gitDir := filepath.Join(commonDir, "worktrees", filepath.Base(path))
	f.observations[path] = GitObservation{CommonDir: commonDir, Root: path, GitDir: gitDir, Branch: ref}
	branch := ref
	if branch != "" && !strings.HasPrefix(branch, "refs/") {
		branch = "refs/heads/" + branch
	}
	f.worktrees[commonDir] = append(f.worktrees[commonDir], GitWorktree{Root: path, Branch: branch})
	return nil
}

func (f *fakeGit) RemoveWorktree(_ context.Context, commonDir, path string, force bool) error {
	f.removeCalls = append(f.removeCalls, removeCall{commonDir: commonDir, path: path, force: force})
	return nil
}

func (f *fakeGit) Dirty(_ context.Context, path string) (bool, error) { return f.dirty[path], nil }

func (f *fakeGit) ResolveWorktreeRoot(_ context.Context, gitDir string) (string, error) {
	root, ok := f.resolvedRoots[gitDir]
	if !ok {
		return "", errors.New("worktree root unavailable")
	}
	return root, nil
}
