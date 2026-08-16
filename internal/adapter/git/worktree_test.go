package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorktreeInspectNormalLinkedSymlinkAndMoveContinuity(t *testing.T) {
	repo := initRepository(t)
	adapter := New()
	ctx := context.Background()
	primary, err := adapter.Inspect(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	if primary.Bare || primary.Root != repo || !filepath.IsAbs(primary.CommonDir) || !filepath.IsAbs(primary.GitDir) {
		t.Fatalf("primary=%#v", primary)
	}
	link := filepath.Join(t.TempDir(), "repo-link")
	if err := os.Symlink(repo, link); err != nil {
		t.Fatal(err)
	}
	throughLink, err := adapter.Inspect(ctx, link)
	if err != nil {
		t.Fatal(err)
	}
	if throughLink.CommonDir != primary.CommonDir || throughLink.Root != primary.Root || throughLink.GitDir != primary.GitDir {
		t.Fatalf("symlink observation=%#v primary=%#v", throughLink, primary)
	}
	worktree := filepath.Join(t.TempDir(), "linked")
	runGit(t, repo, "worktree", "add", "-b", "feature-test", worktree)
	linked, err := adapter.Inspect(ctx, worktree)
	if err != nil {
		t.Fatal(err)
	}
	if linked.CommonDir != primary.CommonDir || linked.GitDir == primary.GitDir || linked.Root != canonicalPath(t, worktree) || linked.Branch != "feature-test" {
		t.Fatalf("linked=%#v primary=%#v", linked, primary)
	}
	moved := filepath.Join(filepath.Dir(worktree), "moved")
	runGit(t, repo, "worktree", "move", worktree, moved)
	afterMove, err := adapter.Inspect(ctx, moved)
	if err != nil {
		t.Fatal(err)
	}
	if afterMove.GitDir != linked.GitDir || afterMove.CommonDir != linked.CommonDir || afterMove.Root != canonicalPath(t, moved) {
		t.Fatalf("move continuity before=%#v after=%#v", linked, afterMove)
	}
}

func TestAgentExecutionA1ResolveWorktreeRootIsIndependentOfProcessCWD(t *testing.T) {
	repo := initRepository(t)
	adapter := New()
	ctx := context.Background()
	primary, err := adapter.Inspect(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	got, err := adapter.ResolveWorktreeRoot(ctx, primary.GitDir)
	if err != nil {
		t.Fatal(err)
	}
	if got != repo {
		t.Fatalf("primary root=%q want %q", got, repo)
	}

	linkedPath := filepath.Join(t.TempDir(), "linked")
	runGit(t, repo, "worktree", "add", "-b", "resolve-root", linkedPath)
	linked, err := adapter.Inspect(ctx, linkedPath)
	if err != nil {
		t.Fatal(err)
	}
	got, err = adapter.ResolveWorktreeRoot(ctx, linked.GitDir)
	if err != nil {
		t.Fatal(err)
	}
	if got != canonicalPath(t, linkedPath) {
		t.Fatalf("linked root=%q want %q", got, canonicalPath(t, linkedPath))
	}

	moved := filepath.Join(filepath.Dir(linkedPath), "moved")
	runGit(t, repo, "worktree", "move", linkedPath, moved)
	got, err = adapter.ResolveWorktreeRoot(ctx, linked.GitDir)
	if err != nil {
		t.Fatal(err)
	}
	if got != canonicalPath(t, moved) {
		t.Fatalf("moved root=%q want %q", got, canonicalPath(t, moved))
	}
}

func TestWorktreeInspectBareRepositoryAndListPorcelain(t *testing.T) {
	bare := filepath.Join(t.TempDir(), "bare.git")
	runGit(t, "", "init", "--bare", bare)
	adapter := New()
	obs, err := adapter.Inspect(context.Background(), bare)
	if err != nil {
		t.Fatal(err)
	}
	if !obs.Bare || obs.CommonDir != canonicalPath(t, bare) || obs.GitDir != canonicalPath(t, bare) || obs.Root != canonicalPath(t, bare) {
		t.Fatalf("bare=%#v", obs)
	}

	repo := initRepository(t)
	linked := filepath.Join(t.TempDir(), "linked")
	runGit(t, repo, "worktree", "add", "-b", "list-test", linked)
	primary, err := adapter.Inspect(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	got, err := adapter.ListWorktrees(context.Background(), primary.CommonDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Root == "" || got[1].Root == "" {
		t.Fatalf("worktrees=%#v", got)
	}
}

func TestWorktreeRemoveDoesNotPruneAndSeparatesPath(t *testing.T) {
	runner := &recordingRunner{}
	adapter := Repository{runner: runner}
	if err := adapter.RemoveWorktree(context.Background(), "/repo/.git", "/tmp/work tree", true); err != nil {
		t.Fatal(err)
	}
	want := []string{"--git-dir", "/repo/.git", "worktree", "remove", "--force", "--", "/tmp/work tree"}
	if !equalArgs(runner.args, want) {
		t.Fatalf("args=%q want=%q", runner.args, want)
	}
	for _, arg := range runner.args {
		if arg == "prune" {
			t.Fatal("remove invoked prune")
		}
	}
}

type recordingRunner struct{ args []string }

func (r *recordingRunner) Run(_ context.Context, args ...string) ([]byte, []byte, error) {
	r.args = append([]string(nil), args...)
	return nil, nil, nil
}

func equalArgs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestInitRepositoryUsesMainBranchRegardlessOfGitDefault(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(configPath, []byte(`[init]
	defaultBranch = trunk
`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", configPath)

	repo := initRepository(t)
	branch := strings.TrimSpace(runGitOutput(t, repo, "rev-parse", "--abbrev-ref", "HEAD"))
	if branch != "main" {
		t.Fatalf("branch=%q want main", branch)
	}
}

func initRepository(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	runGit(t, "", "init", "-b", "main", repo)
	runGit(t, repo, "config", "user.email", "shellbeam@example.invalid")
	runGit(t, repo, "config", "user.name", "ShellBeam Test")
	if err := os.WriteFile(filepath.Join(repo, "README"), []byte("test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "README")
	runGit(t, repo, "commit", "-m", "initial")
	root, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %q: %v\n%s", args, err, out)
	}
}

func canonicalPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	abs, err := filepath.Abs(resolved)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(abs)
}
