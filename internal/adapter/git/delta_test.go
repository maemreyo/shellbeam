package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestDeltaTrackedModification(t *testing.T) {
	root, workspace := newDeltaRepository(t)
	appendDeltaFile(t, filepath.Join(root, "tracked.txt"), "changed\n")

	sample := New().SampleDelta(context.Background(), workspace, core.DeltaLimits{})
	if sample.Completeness != core.SelectionComplete || sample.Freshness != core.SampleFreshlySampled {
		t.Fatalf("sample=%#v", sample)
	}
	if sample.Ref != "refs/heads/main" || sample.Head == "" || sample.Detached || sample.Unborn {
		t.Fatalf("branch metadata=%#v", sample)
	}
	if len(sample.Changes) != 1 {
		t.Fatalf("changes=%#v", sample.Changes)
	}
	change := sample.Changes[0]
	if change.PathTransition != core.PathModified || change.NewPath != "tracked.txt" || change.SourceTransition != core.SourceBytesChanged || change.VCSTransition != core.VCSOther {
		t.Fatalf("change=%#v", change)
	}
}

func newDeltaRepository(t *testing.T) (string, core.Workspace) {
	t.Helper()
	root := t.TempDir()
	runDeltaGit(t, root, "init", "-q")
	runDeltaGit(t, root, "symbolic-ref", "HEAD", "refs/heads/main")
	runDeltaGit(t, root, "config", "user.email", "delta@example.invalid")
	runDeltaGit(t, root, "config", "user.name", "Delta Test")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runDeltaGit(t, root, "add", "tracked.txt")
	runDeltaGit(t, root, "commit", "-qm", "base")
	return root, deltaWorkspace(root)
}

func runDeltaGit(t *testing.T, root string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return out
}

func appendDeltaFile(t *testing.T, path, text string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString(text); err != nil {
		t.Fatal(err)
	}
}

func TestDeltaStagedOnlyChange(t *testing.T) {
	root, workspace := newDeltaRepository(t)
	appendDeltaFile(t, filepath.Join(root, "tracked.txt"), "staged\n")
	runDeltaGit(t, root, "add", "tracked.txt")
	sample := New().SampleDelta(context.Background(), workspace, core.DeltaLimits{})
	change := requireDeltaChange(t, sample, "tracked.txt")
	if change.PathTransition != core.PathModified || change.VCSTransition != core.VCSStaged {
		t.Fatalf("change=%#v", change)
	}
}

func TestDeltaNestedUntrackedFile(t *testing.T) {
	root, workspace := newDeltaRepository(t)
	path := filepath.Join(root, "new", "nested", "untracked.txt")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	writeDeltaFile(t, path, "new\n")
	sample := New().SampleDelta(context.Background(), workspace, core.DeltaLimits{})
	change := requireDeltaChange(t, sample, "new/nested/untracked.txt")
	if change.PathTransition != core.PathAdded || !change.Untracked || change.SourceTransition != core.SourceAvailabilityChanged {
		t.Fatalf("change=%#v", change)
	}
}

func TestDeltaDeletion(t *testing.T) {
	root, workspace := newDeltaRepository(t)
	if err := os.Remove(filepath.Join(root, "tracked.txt")); err != nil {
		t.Fatal(err)
	}
	sample := New().SampleDelta(context.Background(), workspace, core.DeltaLimits{})
	change := requireDeltaChange(t, sample, "tracked.txt")
	if change.PathTransition != core.PathDeleted || change.OldPath != "tracked.txt" || change.NewPath != "" {
		t.Fatalf("change=%#v", change)
	}
}

func TestDeltaRenameIsDeletePlusAdd(t *testing.T) {
	root, workspace := newDeltaRepository(t)
	if err := os.Rename(filepath.Join(root, "tracked.txt"), filepath.Join(root, "renamed.txt")); err != nil {
		t.Fatal(err)
	}
	runDeltaGit(t, root, "add", "-A")
	sample := New().SampleDelta(context.Background(), workspace, core.DeltaLimits{})
	if len(sample.Changes) != 2 {
		t.Fatalf("changes=%#v", sample.Changes)
	}
	old := requireDeltaChange(t, sample, "tracked.txt")
	newFile := requireDeltaChange(t, sample, "renamed.txt")
	if old.PathTransition != core.PathDeleted || newFile.PathTransition != core.PathAdded {
		t.Fatalf("old=%#v new=%#v", old, newFile)
	}
}

func TestDeltaUnmergedRecord(t *testing.T) {
	root, workspace := newDeltaRepository(t)
	runDeltaGit(t, root, "checkout", "-qb", "topic")
	writeDeltaFile(t, filepath.Join(root, "tracked.txt"), "topic\n")
	runDeltaGit(t, root, "commit", "-qam", "topic")
	runDeltaGit(t, root, "checkout", "-q", "main")
	writeDeltaFile(t, filepath.Join(root, "tracked.txt"), "main\n")
	runDeltaGit(t, root, "commit", "-qam", "main")
	runDeltaGitFailure(t, root, "merge", "topic")
	sample := New().SampleDelta(context.Background(), workspace, core.DeltaLimits{})
	change := requireDeltaChange(t, sample, "tracked.txt")
	if change.PathTransition != core.PathUnmerged {
		t.Fatalf("change=%#v sample=%#v", change, sample)
	}
}

func TestDeltaSubmoduleRemainsOpaque(t *testing.T) {
	root, workspace := newDeltaRepository(t)
	sub := t.TempDir()
	runDeltaGit(t, sub, "init", "-q")
	runDeltaGit(t, sub, "config", "user.email", "delta@example.invalid")
	runDeltaGit(t, sub, "config", "user.name", "Delta Test")
	writeDeltaFile(t, filepath.Join(sub, "sub.txt"), "base\n")
	runDeltaGit(t, sub, "add", "sub.txt")
	runDeltaGit(t, sub, "commit", "-qm", "sub-base")
	runDeltaGit(t, root, "-c", "protocol.file.allow=always", "submodule", "add", "-q", sub, "sub")
	runDeltaGit(t, root, "commit", "-qam", "add-submodule")
	appendDeltaFile(t, filepath.Join(root, "sub", "sub.txt"), "dirty\n")
	sample := New().SampleDelta(context.Background(), workspace, core.DeltaLimits{})
	change := requireDeltaChange(t, sample, "sub")
	if !change.Submodule || change.PathTransition != core.PathModified || change.SourceTransition != core.SourceIdentityChanged {
		t.Fatalf("change=%#v", change)
	}
}

func TestDeltaDetachedAndUnbornBranchMetadata(t *testing.T) {
	t.Run("detached", func(t *testing.T) {
		root, workspace := newDeltaRepository(t)
		runDeltaGit(t, root, "checkout", "-q", "--detach", "HEAD")
		sample := New().SampleDelta(context.Background(), workspace, core.DeltaLimits{})
		if !sample.Detached || sample.Unborn || sample.Ref != "" || sample.Head == "" {
			t.Fatalf("sample=%#v", sample)
		}
	})
	t.Run("unborn", func(t *testing.T) {
		root := t.TempDir()
		runDeltaGit(t, root, "init", "-q")
		runDeltaGit(t, root, "symbolic-ref", "HEAD", "refs/heads/main")
		writeDeltaFile(t, filepath.Join(root, "new.txt"), "new\n")
		workspace := deltaWorkspace(root)
		sample := New().SampleDelta(context.Background(), workspace, core.DeltaLimits{})
		if !sample.Unborn || sample.Detached || sample.Head != "" || sample.Ref != "refs/heads/main" {
			t.Fatalf("sample=%#v", sample)
		}
	})
}

func TestDeltaPathLimitReturnsPartialPrefix(t *testing.T) {
	root, workspace := newDeltaRepository(t)
	for i := 0; i < 5; i++ {
		writeDeltaFile(t, filepath.Join(root, fmt.Sprintf("u-%d.txt", i)), "new\n")
	}
	limits := core.DeltaLimits{MaxPaths: 3, MaxOutputBytes: 256 << 10, TimeoutMS: 150}
	sample := New().SampleDelta(context.Background(), workspace, limits)
	if sample.Completeness != core.SelectionPartial || len(sample.Changes) != 3 || sample.RecordsObserved != 5 || sample.DiagnosticCode != "path_limit_exceeded" {
		t.Fatalf("sample=%#v", sample)
	}
}

func requireDeltaChange(t *testing.T, sample core.DeltaSample, path string) core.ChangeRecord {
	t.Helper()
	for _, change := range sample.Changes {
		if change.NewPath == path || change.OldPath == path {
			return change
		}
	}
	t.Fatalf("path %q not found in %#v", path, sample.Changes)
	return core.ChangeRecord{}
}

func writeDeltaFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runDeltaGitFailure(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("git %v unexpectedly succeeded\n%s", args, out)
	}
}

func deltaWorkspace(root string) core.Workspace {
	now := time.Now().UTC()
	return core.Workspace{SchemaVersion: core.SchemaVersion, ID: core.WorkspaceID("ws_01K00000000000000000000000"), RepositoryID: core.RepositoryID("repo_01K00000000000000000000000"), Label: "delta", Root: root, GitDir: filepath.Join(root, ".git"), Branch: "main", CreatedAt: now, LastSeenAt: now}
}

func TestDeltaUsesBoundedRunnerAndReturnsPartialOnOutputLimit(t *testing.T) {
	root, workspace := newDeltaRepository(t)
	output := []byte("# branch.oid 0123456789012345678901234567890123456789\x00# branch.head main\x00? one.txt\x00? two.txt\x00")
	runner := &deltaBoundedRunner{output: output}
	repository := newRepository(runner, SnapshotOptions{})
	limits := core.DeltaLimits{MaxPaths: 256, MaxOutputBytes: 90, TimeoutMS: 150}

	sample := repository.SampleDelta(context.Background(), workspace, limits)
	if !runner.usedBounded || runner.limit != limits.MaxOutputBytes {
		t.Fatalf("runner bounded=%v limit=%d", runner.usedBounded, runner.limit)
	}
	wantArgs := []string{"--no-optional-locks", "-C", root, "status", "--porcelain=v2", "-z", "--branch", "--no-renames", "--untracked-files=all"}
	if !slices.Equal(runner.args, wantArgs) {
		t.Fatalf("args=%q want=%q", runner.args, wantArgs)
	}
	if sample.Completeness != core.SelectionPartial || sample.DiagnosticCode != "output_limit_exceeded" || sample.BytesObserved > int64(limits.MaxOutputBytes) {
		t.Fatalf("sample=%#v", sample)
	}
}

func TestDeltaTimeoutIsUnavailableWithBoundedDiagnostic(t *testing.T) {
	_, workspace := newDeltaRepository(t)
	runner := &deltaBlockingRunner{}
	repository := newRepository(runner, SnapshotOptions{})
	limits := core.DeltaLimits{MaxPaths: 1, MaxOutputBytes: 1024, TimeoutMS: 5}
	sample := repository.SampleDelta(context.Background(), workspace, limits)
	if sample.Completeness != core.SelectionUnavailable || sample.DiagnosticCode != "git_status_timeout" {
		t.Fatalf("sample=%#v", sample)
	}
}

type deltaBoundedRunner struct {
	output      []byte
	usedBounded bool
	limit       int
	args        []string
}

func (r *deltaBoundedRunner) Run(context.Context, ...string) ([]byte, []byte, error) {
	return nil, nil, errors.New("unbounded runner used")
}

func (r *deltaBoundedRunner) RunBounded(_ context.Context, limit int, args ...string) ([]byte, []byte, error) {
	r.usedBounded = true
	r.limit = limit
	r.args = append([]string(nil), args...)
	if len(r.output) > limit {
		return append([]byte(nil), r.output[:limit]...), nil, errOutputLimit
	}
	return append([]byte(nil), r.output...), nil, nil
}

type deltaBlockingRunner struct{}

func (*deltaBlockingRunner) Run(ctx context.Context, _ ...string) ([]byte, []byte, error) {
	<-ctx.Done()
	return nil, nil, ctx.Err()
}
