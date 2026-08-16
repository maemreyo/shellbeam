package localfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	checkpointapp "github.com/maemreyo/shellbeam/internal/app/checkpoint"
	core "github.com/maemreyo/shellbeam/internal/core/checkpoint"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"golang.org/x/sys/unix"
)

func TestSelectExactFileSymlinkAbsentAndSubtreeDeterministically(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("file"), 0644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "b.go"), []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "a.go"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "src", "external-dir")); err != nil {
		t.Fatal(err)
	}

	provider := New(filepath.Join(t.TempDir(), "state"), filepath.Join(t.TempDir(), "run"))
	exact := captureRequest(root, []string{"absent.txt", "file.txt", "link"})
	selected, err := provider.selectCapture(context.Background(), exact, defaultSelectionLimits())
	if err != nil {
		t.Fatal(err)
	}
	wantExact := []selectedEntry{{Path: "absent.txt", Kind: entryAbsent}, {Path: "file.txt", Kind: entryFile}, {Path: "link", Kind: entrySymlink}}
	if !reflect.DeepEqual(selected.Entries, wantExact) {
		t.Fatalf("exact entries=%#v want=%#v", selected.Entries, wantExact)
	}

	subtree := captureRequest(root, []string{"src/**"})
	selected, err = provider.selectCapture(context.Background(), subtree, defaultSelectionLimits())
	if err != nil {
		t.Fatal(err)
	}
	wantPaths := []string{"src", "src/a.go", "src/b.go", "src/external-dir"}
	gotPaths := make([]string, 0, len(selected.Entries))
	for _, entry := range selected.Entries {
		gotPaths = append(gotPaths, entry.Path)
	}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("subtree paths=%v want=%v", gotPaths, wantPaths)
	}
	if selected.Entries[3].Kind != entrySymlink {
		t.Fatalf("symlink dir followed or misclassified: %#v", selected.Entries[3])
	}
}

func TestSelectExcludesGitStateRuntimeAndRejectsSubmoduleBoundary(t *testing.T) {
	workspace := t.TempDir()
	project := filepath.Join(workspace, "project")
	state := filepath.Join(project, ".shellbeam-state")
	runtime := filepath.Join(project, ".shellbeam-run")
	for _, dir := range []string{project, state, runtime, filepath.Join(project, ".git"), filepath.Join(project, "src")} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(project, "src", "main.go"), []byte("package main"), 0644); err != nil {
		t.Fatal(err)
	}
	provider := New(state, runtime)
	selected, err := provider.selectCapture(context.Background(), captureRequest(workspace, []string{"project/**"}), defaultSelectionLimits())
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range selected.Entries {
		if entry.Path == "project/.git" || entry.Path == "project/.shellbeam-state" || entry.Path == "project/.shellbeam-run" || filepath.Dir(entry.Path) == "project/.git" || filepath.Dir(entry.Path) == "project/.shellbeam-state" || filepath.Dir(entry.Path) == "project/.shellbeam-run" {
			t.Fatalf("excluded path captured: %#v", entry)
		}
	}
	if len(selected.Excluded) < 3 {
		t.Fatalf("exclusions=%#v", selected.Excluded)
	}

	submodule := filepath.Join(project, "src", "sub")
	if err := os.Mkdir(submodule, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(submodule, ".git"), []byte("gitdir: ../../.git/modules/sub\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err = provider.selectCapture(context.Background(), captureRequest(workspace, []string{"project/src/**"}), defaultSelectionLimits())
	if !localFailureIs(err, failure.CheckpointSubmoduleBoundaryUnsupported) {
		t.Fatalf("submodule boundary err=%v", err)
	}
}

func TestSelectRejectsSpecialFilesWholeWorkspaceAndAllBudgets(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	if err := os.Mkdir(src, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b", "c"} {
		if err := os.WriteFile(filepath.Join(src, name), []byte("xx"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	provider := New(filepath.Join(t.TempDir(), "state"), filepath.Join(t.TempDir(), "run"))
	if _, err := provider.selectCapture(context.Background(), captureRequest(root, []string{"**"}), defaultSelectionLimits()); !localFailureIs(err, failure.CheckpointScopeInvalid) {
		t.Fatalf("whole workspace selector err=%v", err)
	}

	fifo := filepath.Join(src, "fifo")
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.selectCapture(context.Background(), captureRequest(root, []string{"src/fifo"}), defaultSelectionLimits()); !localFailureIs(err, failure.CheckpointPathUnsupported) {
		t.Fatalf("special file err=%v", err)
	}
	if err := os.Remove(fifo); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name   string
		mutate func(*selectionLimits)
	}{
		{name: "walk", mutate: func(l *selectionLimits) { l.MaxWalkEntries = 2 }},
		{name: "entries", mutate: func(l *selectionLimits) { l.MaxCapturedEntries = 1 }},
		{name: "file-bytes", mutate: func(l *selectionLimits) { l.MaxRegularFileBytes = 1 }},
		{name: "total-bytes", mutate: func(l *selectionLimits) { l.MaxCheckpointBytes = 1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			limits := defaultSelectionLimits()
			tc.mutate(&limits)
			_, err := provider.selectCapture(context.Background(), captureRequest(root, []string{"src/**"}), limits)
			if !localFailureIs(err, failure.CheckpointBudgetExceeded) {
				t.Fatalf("budget err=%v", err)
			}
		})
	}

	large := filepath.Join(root, "large")
	file, err := os.Create(large)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(core.MaxRegularFileBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.selectCapture(context.Background(), captureRequest(root, []string{"large"}), defaultSelectionLimits()); !localFailureIs(err, failure.CheckpointBudgetExceeded) {
		t.Fatalf("hard file limit err=%v", err)
	}
}

func localFailureIs(err error, code failure.Code) bool {
	var typed *failure.Failure
	return errors.As(err, &typed) && typed.Code == code
}

var _ checkpointapp.Provider = (*providerInterfaceProbe)(nil)

type providerInterfaceProbe struct{}

func (*providerInterfaceProbe) Identity() core.ProviderIdentity { return core.ProviderIdentity{} }
func (*providerInterfaceProbe) ConflictDetection() core.ConflictDetection {
	return core.ConflictDetection{}
}
func (*providerInterfaceProbe) Capture(context.Context, checkpointapp.CaptureRequest) (checkpointapp.CaptureResult, error) {
	return checkpointapp.CaptureResult{}, nil
}
func (*providerInterfaceProbe) Restore(context.Context, checkpointapp.ProviderRestoreRequest) (checkpointapp.ProviderRestoreResult, error) {
	return checkpointapp.ProviderRestoreResult{}, nil
}
func (*providerInterfaceProbe) Inspect(context.Context, string) (checkpointapp.ProviderCheckpointStatus, error) {
	return checkpointapp.ProviderCheckpointStatus{}, nil
}
func (*providerInterfaceProbe) Sweep(context.Context, checkpointapp.SweepRequest) (checkpointapp.SweepResult, error) {
	return checkpointapp.SweepResult{}, nil
}

func TestSelectExactPathRejectsCrossingSubmoduleBoundary(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "src", "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, ".git"), []byte("gitdir: ../../.git/modules/sub\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "file.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	provider := New(filepath.Join(t.TempDir(), "state"), filepath.Join(t.TempDir(), "run"))
	_, err := provider.selectCapture(context.Background(), captureRequest(root, []string{"src/sub/file.txt"}), defaultSelectionLimits())
	if !localFailureIs(err, failure.CheckpointSubmoduleBoundaryUnsupported) {
		t.Fatalf("exact submodule boundary err=%v", err)
	}
}
