package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	core "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestWorkspaceCLIAPIAttachInspectRenameListForget(t *testing.T) {
	repo := initWorkspaceCLIRepo(t)
	stateDir := filepath.Join(t.TempDir(), "state")

	out, errOut, code := runWorkspaceCLI(t, "workspace", "attach", repo, "--label", "primary", "--state-dir", stateDir, "--json")
	if code != 0 {
		t.Fatalf("attach code=%d stderr=%q", code, errOut)
	}
	var attached core.Workspace
	if err := json.Unmarshal([]byte(out), &attached); err != nil {
		t.Fatal(err)
	}
	if attached.Label != "primary" || attached.Root != repo {
		t.Fatalf("attached=%#v", attached)
	}

	out, errOut, code = runWorkspaceCLI(t, "workspace", "inspect", string(attached.ID), "--state-dir", stateDir, "--json")
	if code != 0 {
		t.Fatalf("inspect code=%d stderr=%q", code, errOut)
	}
	var inspected core.Workspace
	if err := json.Unmarshal([]byte(out), &inspected); err != nil {
		t.Fatal(err)
	}
	if inspected.ID != attached.ID || inspected.RepositoryID != attached.RepositoryID {
		t.Fatalf("inspect=%#v attach=%#v", inspected, attached)
	}

	out, errOut, code = runWorkspaceCLI(t, "workspace", "rename", string(attached.ID), "odd/review: label", "--state-dir", stateDir, "--json")
	if code != 0 {
		t.Fatalf("rename code=%d stderr=%q", code, errOut)
	}
	var renamed core.Workspace
	if err := json.Unmarshal([]byte(out), &renamed); err != nil {
		t.Fatal(err)
	}
	if renamed.ID != attached.ID || renamed.Label != "odd/review: label" {
		t.Fatalf("renamed=%#v", renamed)
	}

	out, errOut, code = runWorkspaceCLI(t, "workspace", "list", "--state-dir", stateDir, "--json")
	if code != 0 {
		t.Fatalf("list code=%d stderr=%q", code, errOut)
	}
	var listed []core.Workspace
	if err := json.Unmarshal([]byte(out), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != attached.ID || listed[0].Label != renamed.Label {
		t.Fatalf("listed=%#v", listed)
	}

	_, errOut, code = runWorkspaceCLI(t, "workspace", "forget", renamed.Label, "--state-dir", stateDir, "--json")
	if code != 0 {
		t.Fatalf("forget code=%d stderr=%q", code, errOut)
	}
	if _, err := os.Stat(repo); err != nil {
		t.Fatalf("forget touched repository: %v", err)
	}
	out, errOut, code = runWorkspaceCLI(t, "workspace", "list", "--state-dir", stateDir, "--json")
	if code != 0 {
		t.Fatalf("list after forget code=%d stderr=%q", code, errOut)
	}
	if err := json.Unmarshal([]byte(out), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Fatalf("listed after forget=%#v", listed)
	}
}

func TestWorkspaceCLICreateAndCleanRemove(t *testing.T) {
	repo := initWorkspaceCLIRepo(t)
	runWorkspaceGit(t, repo, "branch", "feature")
	stateDir := filepath.Join(t.TempDir(), "state")
	worktree := filepath.Join(t.TempDir(), "feature-worktree")

	out, errOut, code := runWorkspaceCLI(t, "workspace", "create", repo, "--ref", "feature", "--path", worktree, "--label", "feature", "--state-dir", stateDir, "--json")
	if code != 0 {
		t.Fatalf("create code=%d stderr=%q", code, errOut)
	}
	var created core.Workspace
	if err := json.Unmarshal([]byte(out), &created); err != nil {
		t.Fatal(err)
	}
	canonicalWorktree, err := filepath.EvalSymlinks(worktree)
	if err != nil {
		t.Fatal(err)
	}
	if created.Label != "feature" || created.Root != canonicalWorktree {
		t.Fatalf("created=%#v expected_root=%q", created, canonicalWorktree)
	}
	if _, err := os.Stat(filepath.Join(worktree, ".git")); err != nil {
		t.Fatalf("worktree not created: %v", err)
	}

	_, errOut, code = runWorkspaceCLI(t, "workspace", "remove", "feature", "--state-dir", stateDir, "--json")
	if code != 0 {
		t.Fatalf("remove code=%d stderr=%q", code, errOut)
	}
	if _, err := os.Stat(worktree); !os.IsNotExist(err) {
		t.Fatalf("worktree still exists or stat failed: %v", err)
	}
}

func TestWorkspaceCLIDirtyRemoveRequiresForce(t *testing.T) {
	repo := initWorkspaceCLIRepo(t)
	runWorkspaceGit(t, repo, "branch", "dirty-feature")
	stateDir := filepath.Join(t.TempDir(), "state")
	worktree := filepath.Join(t.TempDir(), "dirty-worktree")

	_, errOut, code := runWorkspaceCLI(t, "workspace", "create", repo, "--ref", "dirty-feature", "--path", worktree, "--label", "dirty", "--state-dir", stateDir, "--json")
	if code != 0 {
		t.Fatalf("create code=%d stderr=%q", code, errOut)
	}
	if err := os.WriteFile(filepath.Join(worktree, "dirty.txt"), []byte("dirty\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, errOut, code = runWorkspaceCLI(t, "workspace", "remove", "dirty", "--state-dir", stateDir, "--json")
	if code == 0 || !strings.Contains(errOut, "workspace_dirty_requires_force") {
		t.Fatalf("dirty remove code=%d stderr=%q", code, errOut)
	}
	if _, err := os.Stat(worktree); err != nil {
		t.Fatalf("dirty remove touched worktree: %v", err)
	}

	_, errOut, code = runWorkspaceCLI(t, "workspace", "remove", "dirty", "--force", "--state-dir", stateDir, "--json")
	if code != 0 {
		t.Fatalf("force remove code=%d stderr=%q", code, errOut)
	}
	if _, err := os.Stat(worktree); !os.IsNotExist(err) {
		t.Fatalf("forced worktree still exists or stat failed: %v", err)
	}
}

func runWorkspaceCLI(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr)
	return stdout.String(), stderr.String(), code
}

func initWorkspaceCLIRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	runWorkspaceGit(t, "", "init", repo)
	runWorkspaceGit(t, repo, "config", "user.email", "shellbeam@example.invalid")
	runWorkspaceGit(t, repo, "config", "user.name", "ShellBeam Test")
	if err := os.WriteFile(filepath.Join(repo, "README"), []byte("test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runWorkspaceGit(t, repo, "add", "README")
	runWorkspaceGit(t, repo, "commit", "-m", "initial")
	canonical, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}

func runWorkspaceGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %q: %v\n%s", args, err, output)
	}
}
