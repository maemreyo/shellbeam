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
)

func TestProviderIdentityConflictMatrixAndConstructorAreSideEffectFree(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	runtime := filepath.Join(t.TempDir(), "run")
	mustPrivateDir(t, state)
	mustPrivateDir(t, runtime)
	provider := New(state, runtime)
	if provider == nil {
		t.Fatal("provider is nil")
	}
	if got := provider.Identity(); got != (core.ProviderIdentity{ID: "localfs", Version: 1}) {
		t.Fatalf("identity=%#v", got)
	}
	want := core.ConflictDetection{RegularFile: core.ConflictBestEffort, Symlink: core.ConflictBestEffort, AbsentToFile: core.ConflictBestEffort, DirectoryTree: core.ConflictUnsupported}
	if got := provider.ConflictDetection(); got != want {
		t.Fatalf("conflict matrix=%#v want=%#v", got, want)
	}
	if _, err := os.Stat(filepath.Join(state, "checkpoint-content")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("constructor mutated provider state: %v", err)
	}
}

func TestCaptureReplayAcrossProviderRestartKeepsOpaqueRefs(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "a.txt"), []byte("alpha"), 0644); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(t.TempDir(), "state")
	runtime := filepath.Join(t.TempDir(), "run")
	mustPrivateDir(t, state)
	mustPrivateDir(t, runtime)
	request := captureRequest(workspace, []string{"a.txt"})
	provider := New(state, runtime)
	first, err := provider.Capture(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := New(state, runtime).Capture(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) || len(first.OpaqueEntryRefs) != 1 {
		t.Fatalf("restart replay changed result first=%#v second=%#v", first, second)
	}
}

func captureRequest(root string, paths []string) checkpointapp.CaptureRequest {
	return checkpointapp.CaptureRequest{
		CheckpointID:     "chk_01K00000000000000000000000",
		WorkspaceID:      "ws_01K00000000000000000000000",
		RepositoryID:     "repo_01K00000000000000000000000",
		ActivityID:       "PI-756",
		Root:             root,
		SourceGeneration: "gen_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Paths:            paths,
	}
}

func mustPrivateDir(t *testing.T, path string) {
	t.Helper()
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
}

func TestPrivateCaptureLayoutUsesUserOnlyModesAndNoFollowFiles(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "a.txt"), []byte("alpha"), 0755); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(t.TempDir(), "state")
	runtime := filepath.Join(t.TempDir(), "run")
	mustPrivateDir(t, state)
	mustPrivateDir(t, runtime)
	provider := New(state, runtime)
	result, err := provider.Capture(context.Background(), captureRequest(workspace, []string{"a.txt"}))
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{provider.contentRoot(), provider.checkpointsRoot(), provider.checkpointDir("chk_01K00000000000000000000000"), provider.entriesDir("chk_01K00000000000000000000000")} {
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 || info.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("unsafe private dir %s info=%#v err=%v", dir, info, err)
		}
	}
	for _, file := range []string{provider.manifestPath("chk_01K00000000000000000000000"), provider.completePath("chk_01K00000000000000000000000"), provider.entryDataPath("chk_01K00000000000000000000000", result.OpaqueEntryRefs[0])} {
		info, err := os.Lstat(file)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("unsafe private file %s info=%#v err=%v", file, info, err)
		}
	}

	entry := provider.entryDataPath("chk_01K00000000000000000000000", result.OpaqueEntryRefs[0])
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(entry); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, entry); err != nil {
		t.Fatal(err)
	}
	if _, err := New(state, runtime).Capture(context.Background(), captureRequest(workspace, []string{"a.txt"})); err == nil {
		t.Fatal("private content symlink accepted on replay")
	}
}

func TestPrivateCaptureRejectsUnsafeRootAndCorruptManifest(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "a.txt"), []byte("alpha"), 0644); err != nil {
		t.Fatal(err)
	}

	t.Run("symlink private parent", func(t *testing.T) {
		state := filepath.Join(t.TempDir(), "state")
		runtime := filepath.Join(t.TempDir(), "run")
		mustPrivateDir(t, state)
		mustPrivateDir(t, runtime)
		outside := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(state, "checkpoint-content")); err != nil {
			t.Fatal(err)
		}
		if _, err := New(state, runtime).Capture(context.Background(), captureRequest(workspace, []string{"a.txt"})); err == nil {
			t.Fatal("symlink private root accepted")
		}
	})

	t.Run("non-directory private parent", func(t *testing.T) {
		state := filepath.Join(t.TempDir(), "state")
		runtime := filepath.Join(t.TempDir(), "run")
		mustPrivateDir(t, state)
		mustPrivateDir(t, runtime)
		if err := os.WriteFile(filepath.Join(state, "checkpoint-content"), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := New(state, runtime).Capture(context.Background(), captureRequest(workspace, []string{"a.txt"})); err == nil {
			t.Fatal("file private root accepted")
		}
	})

	t.Run("corrupt manifest", func(t *testing.T) {
		state := filepath.Join(t.TempDir(), "state")
		runtime := filepath.Join(t.TempDir(), "run")
		mustPrivateDir(t, state)
		mustPrivateDir(t, runtime)
		provider := New(state, runtime)
		request := captureRequest(workspace, []string{"a.txt"})
		if _, err := provider.Capture(context.Background(), request); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(provider.manifestPath(request.CheckpointID), []byte(`{"schema_version":1,"unknown":true}`), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := New(state, runtime).Capture(context.Background(), request); err == nil {
			t.Fatal("corrupt manifest accepted")
		}
	})
}
