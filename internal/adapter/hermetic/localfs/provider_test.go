//go:build linux || darwin

package localfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	app "github.com/maemreyo/shellbeam/internal/app/hermetic"
	core "github.com/maemreyo/shellbeam/internal/core/hermetic"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
	"golang.org/x/sys/unix"
)

func TestProviderCapturesExactAndRecursiveInputsWithoutHostMutation(t *testing.T) {
	root, private := captureFixture(t)
	beforeGoMod := mustRead(t, filepath.Join(root, "go.mod"))
	beforeSource := mustRead(t, filepath.Join(root, "internal", "x.go"))
	provider := New(private)
	view, err := provider.Capture(context.Background(), captureRequest(root, []string{"internal/**", "go.mod"}, core.DefaultCaptureLimits()))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(mustRead(t, filepath.Join(view.PrivateRoot, "go.mod"))); got != string(beforeGoMod) {
		t.Fatalf("captured go.mod=%q", got)
	}
	if got := string(mustRead(t, filepath.Join(view.PrivateRoot, "internal", "x.go"))); got != string(beforeSource) {
		t.Fatalf("captured source=%q", got)
	}
	if _, err := os.Stat(filepath.Join(view.PrivateRoot, "README.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("undeclared README exposed: %v", err)
	}
	info, err := os.Stat(filepath.Join(view.PrivateRoot, "internal", "run.sh"))
	if err != nil || info.Mode().Perm()&0o111 == 0 || info.Mode().Perm()&0o222 != 0 {
		t.Fatalf("captured executable mode=%v err=%v", info.Mode(), err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := string(mustRead(t, filepath.Join(view.PrivateRoot, "go.mod"))); got != string(beforeGoMod) {
		t.Fatalf("host mutation changed capture: %q", got)
	}
	paths := make([]string, 0, len(view.Manifest.Entries))
	for _, entry := range view.Manifest.Entries {
		paths = append(paths, entry.Path)
	}
	if !reflect.DeepEqual(paths, []string{"go.mod", "internal/run.sh", "internal/sub/y.go", "internal/x.go"}) {
		t.Fatalf("manifest paths=%v", paths)
	}
	if _, err := view.Manifest.Digest(); err != nil {
		t.Fatal(err)
	}
	captureDir := filepath.Dir(view.PrivateRoot)
	if err := provider.Discard(context.Background(), view); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(captureDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("discard left capture dir: %v", err)
	}
}

func TestProviderRejectsSymlinkSpecialAndNestedGitInputs(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*testing.T, string)
		sel   []string
	}{
		{"symlink", func(t *testing.T, root string) {
			outside := filepath.Join(t.TempDir(), "secret")
			mustWrite(t, outside, "secret", 0o644)
			if err := os.Symlink(outside, filepath.Join(root, "internal", "link")); err != nil {
				t.Fatal(err)
			}
		}, []string{"internal/**"}},
		{"fifo", func(t *testing.T, root string) {
			if err := unix.Mkfifo(filepath.Join(root, "internal", "pipe"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, []string{"internal/**"}},
		{"nested_git", func(t *testing.T, root string) {
			if err := os.MkdirAll(filepath.Join(root, "vendor", "pkg", ".git"), 0o755); err != nil {
				t.Fatal(err)
			}
			mustWrite(t, filepath.Join(root, "vendor", "pkg", "x.go"), "package x\n", 0o644)
		}, []string{"vendor/**"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, private := captureFixture(t)
			tc.setup(t, root)
			provider := New(private)
			if _, err := provider.Capture(context.Background(), captureRequest(root, tc.sel, core.DefaultCaptureLimits())); err == nil {
				t.Fatal("unsafe capture accepted")
			}
			assertNoCaptureResidue(t, private)
		})
	}
}

func TestProviderFailsClosedOnAllCaptureBudgets(t *testing.T) {
	root, private := captureFixture(t)
	cases := []core.CaptureLimits{
		{MaxPaths: 1, MaxFileBytes: core.MaxCaptureFileBytes, MaxTotalBytes: core.MaxCaptureTotalBytes, MaxWalkEntries: 8},
		{MaxPaths: 8, MaxFileBytes: 4, MaxTotalBytes: 32, MaxWalkEntries: 8},
		{MaxPaths: 8, MaxFileBytes: 32, MaxTotalBytes: 40, MaxWalkEntries: 8},
	}
	for i, limits := range cases {
		provider := New(private)
		if _, err := provider.Capture(context.Background(), captureRequest(root, []string{"internal/**", "go.mod"}, limits)); err == nil {
			t.Fatalf("budget case %d accepted", i)
		}
		assertNoCaptureResidue(t, private)
	}
}

func TestProviderFailsClosedWhenWalkBudgetIsExhaustedByDirectories(t *testing.T) {
	root, private := captureFixture(t)
	for i := 0; i < 10; i++ {
		if err := os.MkdirAll(filepath.Join(root, "internal", "empty", string(rune('a'+i))), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	limits := core.CaptureLimits{MaxPaths: 8, MaxFileBytes: 32, MaxTotalBytes: 64, MaxWalkEntries: 8}
	if _, err := New(private).Capture(context.Background(), captureRequest(root, []string{"internal/**"}, limits)); err == nil {
		t.Fatal("walk budget exhaustion was accepted")
	}
	assertNoCaptureResidue(t, private)
}

func TestProviderDetectsSourceMutationDuringCaptureAndCleansPrivateView(t *testing.T) {
	root, private := captureFixture(t)
	provider := New(private)
	provider.afterCopy = func(index int, _ string) error {
		if index == 1 {
			return os.WriteFile(filepath.Join(root, "internal", "x.go"), []byte("package changed\n"), 0o644)
		}
		return nil
	}
	if _, err := provider.Capture(context.Background(), captureRequest(root, []string{"internal/**", "go.mod"}, core.DefaultCaptureLimits())); err == nil {
		t.Fatal("source mutation during capture was accepted")
	}
	assertNoCaptureResidue(t, private)
}

func TestProviderDetectsMutationAfterCopiedFileAndCleansPrivateView(t *testing.T) {
	root, private := captureFixture(t)
	provider := New(private)
	provider.afterCopy = func(index int, copied string) error {
		if index == 1 {
			return os.WriteFile(filepath.Join(root, filepath.FromSlash(copied)), []byte("changed after copy\n"), 0o644)
		}
		return nil
	}
	if _, err := provider.Capture(context.Background(), captureRequest(root, []string{"go.mod", "internal/**"}, core.DefaultCaptureLimits())); err == nil {
		t.Fatal("mutation after copied file was accepted")
	}
	assertNoCaptureResidue(t, private)
}

func TestProviderOverlappingSelectorsDeduplicateAndPrivateRootMustStayOutsideWorkspace(t *testing.T) {
	root, private := captureFixture(t)
	provider := New(private)
	view, err := provider.Capture(context.Background(), captureRequest(root, []string{"internal/**", "internal/x.go"}, core.DefaultCaptureLimits()))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, entry := range view.Manifest.Entries {
		if seen[entry.Path] {
			t.Fatalf("duplicate entry %q", entry.Path)
		}
		seen[entry.Path] = true
	}
	if err := provider.Discard(context.Background(), view); err != nil {
		t.Fatal(err)
	}

	inside := filepath.Join(root, ".private-hermetic")
	provider = New(inside)
	if _, err := provider.Capture(context.Background(), captureRequest(root, []string{"go.mod"}, core.DefaultCaptureLimits())); err == nil {
		t.Fatal("private capture root inside host workspace accepted")
	}
	if _, err := os.Stat(inside); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unsafe private root mutated workspace: %v", err)
	}
}

func TestProviderCancellationDoesNoPrivateWork(t *testing.T) {
	root, private := captureFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New(private).Capture(ctx, captureRequest(root, []string{"go.mod"}, core.DefaultCaptureLimits())); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled capture err=%v", err)
	}
	assertNoCaptureResidue(t, private)
}

func captureFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	private := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "module example\n", 0o644)
	mustWrite(t, filepath.Join(root, "README.md"), "not selected\n", 0o644)
	mustWrite(t, filepath.Join(root, "internal", "x.go"), "package internal\n", 0o644)
	mustWrite(t, filepath.Join(root, "internal", "run.sh"), "#!/bin/sh\nexit 0\n", 0o755)
	mustWrite(t, filepath.Join(root, "internal", "sub", "y.go"), "package sub\n", 0o644)
	return filepath.Clean(root), filepath.Clean(private)
}

func captureRequest(root string, selectors []string, limits core.CaptureLimits) app.ProviderCaptureRequest {
	return app.ProviderCaptureRequest{
		WorkspaceID:  workspacecore.WorkspaceID("ws_01K00000000000000000000000"),
		RepositoryID: workspacecore.RepositoryID("repo_01K00000000000000000000000"),
		Root:         root, SourceGeneration: "gen_" + strings.Repeat("a", 64), Selectors: selectors, Limits: limits,
	}
}

func mustWrite(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func assertNoCaptureResidue(t *testing.T, private string) {
	t.Helper()
	entries, err := os.ReadDir(private)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "hcap_") {
			t.Fatalf("capture residue %q", entry.Name())
		}
	}
}
