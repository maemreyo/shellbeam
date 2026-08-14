package sourcefs

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	app "github.com/maemreyo/shellbeam/internal/app/codeintel"
	core "github.com/maemreyo/shellbeam/internal/core/codeintel"
	"github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestBinderBindsExactSavedUTF8Source(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte("package main\n// Tiếng Việt 😀 a\u0301\n")
	if err := os.WriteFile(filepath.Join(root, "src", "main.go"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	binder := newTestBinder(t, 1<<20)
	bound, err := binder.Bind(t.Context(), testWorkspace(root), "src/main.go")
	if err != nil {
		t.Fatal(err)
	}
	if string(bound.Bytes) != string(content) {
		t.Fatalf("bound bytes differ: %q", bound.Bytes)
	}
	if bound.Ref.Origin != core.SourceWorkspace || bound.Ref.LogicalPath != "src/main.go" || bound.Ref.DisplayIdentity != "src/main.go" {
		t.Fatalf("unexpected source ref: %#v", bound.Ref)
	}
	if strings.Contains(bound.Ref.DisplayIdentity, root) {
		t.Fatalf("absolute root leaked in display identity: %q", bound.Ref.DisplayIdentity)
	}
	if _, err := core.DisplayPositionToByteOffset(bound.Bytes, 2, 12); err != nil {
		t.Fatalf("Vietnamese/emoji source is not canonically addressable: %v", err)
	}
	resolved, state := binder.Resolve(bound.Ref.ID)
	if state != app.SourceRefCurrent || string(resolved.Bytes) != string(content) {
		t.Fatalf("resolve state=%q bytes=%q", state, resolved.Bytes)
	}
}

func TestBinderRejectsUnsafePathsBeforeFilesystemAccess(t *testing.T) {
	binder := newTestBinder(t, 1<<20)
	workspace := testWorkspace(t.TempDir())
	for _, path := range []string{"../escape.go", "/private/main.go", "bad\x00.go", "bad\n.go", "./main.go", "a//b.go"} {
		t.Run(strings.ReplaceAll(path, "/", "_"), func(t *testing.T) {
			if _, err := binder.Bind(t.Context(), workspace, path); app.ErrorCode(err) != app.CodePositionInvalid {
				t.Fatalf("path %q error=%v code=%q", path, err, app.ErrorCode(err))
			}
		})
	}
}

func TestBinderRejectsOversizedAndNonUTF8Source(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "large.go"), []byte("12345"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "invalid.go"), []byte{0xff, 0xfe}, 0o644); err != nil {
		t.Fatal(err)
	}
	binder := newTestBinder(t, 4)
	if _, err := binder.Bind(t.Context(), testWorkspace(root), "large.go"); app.ErrorCode(err) != app.CodeQueryBudgetExceeded {
		t.Fatalf("oversized code=%q err=%v", app.ErrorCode(err), err)
	}
	binder = newTestBinder(t, 64)
	if _, err := binder.Bind(t.Context(), testWorkspace(root), "invalid.go"); app.ErrorCode(err) != app.CodeUnsupportedEncoding {
		t.Fatalf("encoding code=%q err=%v", app.ErrorCode(err), err)
	}
}

func TestBinderDoesNotFollowFinalOrIntermediateSymlink(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	if err := os.WriteFile(filepath.Join(external, "secret.go"), []byte("package secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(external, "secret.go"), filepath.Join(root, "final.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	binder := newTestBinder(t, 1<<20)
	for _, path := range []string{"final.go", "linked/secret.go"} {
		if _, err := binder.Bind(t.Context(), testWorkspace(root), path); app.ErrorCode(err) != app.CodeSourceRefUnavailable {
			t.Fatalf("symlink %q code=%q err=%v", path, app.ErrorCode(err), err)
		}
	}
}

func TestBinderRejectsSpecialFiles(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "sbci-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	fifo := filepath.Join(root, "source.fifo")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(root, "source.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	binder := newTestBinder(t, 1<<20)
	for _, path := range []string{"source.fifo", "source.sock"} {
		if _, err := binder.Bind(t.Context(), testWorkspace(root), path); app.ErrorCode(err) != app.CodeSourceRefUnavailable {
			t.Fatalf("special %q code=%q err=%v", path, app.ErrorCode(err), err)
		}
	}
}

func TestBinderDetectsSourceReplacementBeforeClaimingExactRef(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	if err := os.WriteFile(path, []byte("package old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(root, "replacement.go")
	if err := os.WriteFile(replacement, []byte("package new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	binder := newTestBinder(t, 1<<20)
	binder.afterRead = func() {
		if err := os.Rename(replacement, path); err != nil {
			t.Fatalf("replace source: %v", err)
		}
		binder.afterRead = nil
	}
	if _, err := binder.Bind(t.Context(), testWorkspace(root), "main.go"); app.ErrorCode(err) != app.CodeSourceChanged || !app.Retryable(err) {
		t.Fatalf("replacement code=%q retryable=%v err=%v", app.ErrorCode(err), app.Retryable(err), err)
	}
}

func newTestBinder(t *testing.T, maxSourceBytes int64) *Binder {
	t.Helper()
	store, err := app.NewSourceStore(app.SourceStoreConfig{
		MaxEntries:       8,
		MaxRetainedBytes: 4 << 20,
		TTL:              time.Minute,
		MaxTombstones:    8,
	})
	if err != nil {
		t.Fatal(err)
	}
	binder, err := NewBinder(store, maxSourceBytes)
	if err != nil {
		t.Fatal(err)
	}
	return binder
}

func testWorkspace(root string) workspace.Workspace {
	now := time.Unix(1_700_000_000, 0)
	return workspace.Workspace{
		SchemaVersion: workspace.SchemaVersion,
		ID:            workspace.WorkspaceID("ws_01K00000000000000000000000"),
		RepositoryID:  workspace.RepositoryID("repo_01K00000000000000000000000"),
		Label:         "codeintel",
		Root:          root,
		GitDir:        filepath.Join(root, ".git"),
		CreatedAt:     now,
		LastSeenAt:    now,
	}
}
