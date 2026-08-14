package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTransientMergeConflict(t *testing.T) {
	repo, topic := divergedRepository(t)
	if runGitMayFail(repo, "merge", topic) == nil {
		t.Fatal("merge unexpectedly succeeded")
	}
	got := snapshotRepository(t, repo)
	if !got.Transient.Merge || got.Dirty.Conflicted == 0 {
		t.Fatalf("snapshot=%#v", got)
	}
}

func TestTransientRebaseConflict(t *testing.T) {
	repo, topic := divergedRepository(t)
	runGit(t, repo, "checkout", topic)
	if runGitMayFail(repo, "rebase", "main") == nil {
		t.Fatal("rebase unexpectedly succeeded")
	}
	got := snapshotRepository(t, repo)
	if !got.Transient.Rebase || got.Dirty.Conflicted == 0 {
		t.Fatalf("snapshot=%#v", got)
	}
}

func TestTransientCherryPickConflict(t *testing.T) {
	repo, topic := divergedRepository(t)
	topicCommit := strings.TrimSpace(runGitOutput(t, repo, "rev-parse", topic))
	if runGitMayFail(repo, "cherry-pick", topicCommit) == nil {
		t.Fatal("cherry-pick unexpectedly succeeded")
	}
	got := snapshotRepository(t, repo)
	if !got.Transient.CherryPick || got.Dirty.Conflicted == 0 {
		t.Fatalf("snapshot=%#v", got)
	}
}

func TestTransientRevertConflict(t *testing.T) {
	repo := initRepository(t)
	if err := os.WriteFile(filepath.Join(repo, "README"), []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "README")
	runGit(t, repo, "commit", "-m", "first change")
	first := strings.TrimSpace(runGitOutput(t, repo, "rev-parse", "HEAD"))
	if err := os.WriteFile(filepath.Join(repo, "README"), []byte("second unrelated shape\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "README")
	runGit(t, repo, "commit", "-m", "second change")
	if runGitMayFail(repo, "revert", "--no-edit", first) == nil {
		t.Fatal("revert unexpectedly succeeded")
	}
	got := snapshotRepository(t, repo)
	if !got.Transient.Revert || got.Dirty.Conflicted == 0 {
		t.Fatalf("snapshot=%#v", got)
	}
}

func TestTransientBisect(t *testing.T) {
	repo := initRepository(t)
	for i := 0; i < 3; i++ {
		if err := os.WriteFile(filepath.Join(repo, "bisect.txt"), []byte(strings.Repeat("x", i+1)), 0o600); err != nil {
			t.Fatal(err)
		}
		runGit(t, repo, "add", "bisect.txt")
		runGit(t, repo, "commit", "-m", "bisect step")
	}
	runGit(t, repo, "bisect", "start", "HEAD", "HEAD~3")
	got := snapshotRepository(t, repo)
	if !got.Transient.Bisect {
		t.Fatalf("snapshot=%#v", got)
	}
	runGit(t, repo, "bisect", "reset")
}

func divergedRepository(t *testing.T) (string, string) {
	t.Helper()
	repo := initRepository(t)
	if err := os.WriteFile(filepath.Join(repo, "conflict.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "conflict.txt")
	runGit(t, repo, "commit", "-m", "conflict base")
	runGit(t, repo, "checkout", "-b", "topic")
	if err := os.WriteFile(filepath.Join(repo, "conflict.txt"), []byte("topic\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "conflict.txt")
	runGit(t, repo, "commit", "-m", "topic change")
	runGit(t, repo, "checkout", "main")
	if err := os.WriteFile(filepath.Join(repo, "conflict.txt"), []byte("main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "conflict.txt")
	runGit(t, repo, "commit", "-m", "main change")
	return repo, "topic"
}

func runGitMayFail(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return cmd.Run()
}

func runGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %q: %v", args, err)
	}
	return string(out)
}
