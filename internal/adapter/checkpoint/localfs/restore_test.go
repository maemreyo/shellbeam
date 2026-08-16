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
)

func TestRestoreRegularSymlinkAbsentNoopAndDirectoryUnsupported(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "file.sh")
	if err := os.WriteFile(file, []byte("captured\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(file, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target-original", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "dir"), 0755); err != nil {
		t.Fatal(err)
	}

	provider := newRestoreTestProvider(t)
	paths := []string{"dir", "file.sh", "gone", "link"}
	if _, err := provider.Capture(context.Background(), captureRequest(root, paths)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("changed\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(file, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target-changed", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "gone"), []byte("created later"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := provider.Restore(context.Background(), restoreRequest(root, "restore-1", paths))
	if err != nil {
		t.Fatal(err)
	}
	want := []core.RestorePathResult{
		{Path: "dir", Outcome: core.RestoreUnsupported, Reason: "directory_tree"},
		{Path: "file.sh", Outcome: core.RestoreRestored},
		{Path: "gone", Outcome: core.RestoreRestored},
		{Path: "link", Outcome: core.RestoreRestored},
	}
	if !reflect.DeepEqual(got.Paths, want) {
		t.Fatalf("restore paths=%#v want=%#v", got.Paths, want)
	}
	raw, err := os.ReadFile(file)
	if err != nil || string(raw) != "captured\n" {
		t.Fatalf("regular restore raw=%q err=%v", raw, err)
	}
	info, err := os.Stat(file)
	if err != nil || info.Mode().Perm()&0111 == 0 {
		t.Fatalf("executable mode not restored mode=%v err=%v", info.Mode(), err)
	}
	link, err := os.Readlink(filepath.Join(root, "link"))
	if err != nil || link != "target-original" {
		t.Fatalf("symlink restore=%q err=%v", link, err)
	}
	if _, err := os.Lstat(filepath.Join(root, "gone")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("captured absent path still exists err=%v", err)
	}
}

func TestRestoreNoopWhenCurrentAlreadyMatchesCapturedState(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("same"), 0644); err != nil {
		t.Fatal(err)
	}
	provider := newRestoreTestProvider(t)
	if _, err := provider.Capture(context.Background(), captureRequest(root, []string{"file.txt"})); err != nil {
		t.Fatal(err)
	}
	got, err := provider.Restore(context.Background(), restoreRequest(root, "restore-noop", []string{"file.txt"}))
	if err != nil {
		t.Fatal(err)
	}
	want := []core.RestorePathResult{{Path: "file.txt", Outcome: core.RestoreNoop}}
	if !reflect.DeepEqual(got.Paths, want) {
		t.Fatalf("noop=%#v want=%#v", got.Paths, want)
	}
}

func TestRestoreSameIDResumesOnlyUnfinishedPathsAndRejectsConflictingIntent(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("captured-"+name), 0644); err != nil {
			t.Fatal(err)
		}
	}
	provider := newRestoreTestProvider(t)
	paths := []string{"a.txt", "b.txt"}
	if _, err := provider.Capture(context.Background(), captureRequest(root, paths)); err != nil {
		t.Fatal(err)
	}
	for _, name := range paths {
		if err := os.WriteFile(filepath.Join(root, name), []byte("changed-"+name), 0644); err != nil {
			t.Fatal(err)
		}
	}
	provider.afterRestorePath = func(ordinal int) error {
		if ordinal == 0 {
			return errors.New("injected crash")
		}
		return nil
	}
	req := restoreRequest(root, "restore-resume", paths)
	if _, err := provider.Restore(context.Background(), req); err == nil {
		t.Fatal("injected crash accepted")
	}
	if raw, _ := os.ReadFile(filepath.Join(root, "a.txt")); string(raw) != "captured-a.txt" {
		t.Fatalf("first path was not finalized before crash: %q", raw)
	}
	if raw, _ := os.ReadFile(filepath.Join(root, "b.txt")); string(raw) != "changed-b.txt" {
		t.Fatalf("unfinished path mutated before retry: %q", raw)
	}

	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("post-crash-user-edit"), 0644); err != nil {
		t.Fatal(err)
	}
	provider.afterRestorePath = nil
	got, err := provider.Restore(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Paths) != 2 || got.Paths[0].Outcome != core.RestoreRestored || got.Paths[1].Outcome != core.RestoreRestored {
		t.Fatalf("resume results=%#v", got.Paths)
	}
	if raw, _ := os.ReadFile(filepath.Join(root, "a.txt")); string(raw) != "post-crash-user-edit" {
		t.Fatalf("finalized first path was re-applied: %q", raw)
	}
	if raw, _ := os.ReadFile(filepath.Join(root, "b.txt")); string(raw) != "captured-b.txt" {
		t.Fatalf("unfinished path not restored: %q", raw)
	}

	conflict := restoreRequest(root, "restore-resume", []string{"a.txt"})
	if _, err := provider.Restore(context.Background(), conflict); !localFailureIs(err, failure.CheckpointRestoreRequestConflict) {
		t.Fatalf("conflicting restore intent err=%v", err)
	}
}

func TestRestoreCorruptPrivateLedgerFailsClosed(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("captured"), 0644); err != nil {
		t.Fatal(err)
	}
	provider := newRestoreTestProvider(t)
	if _, err := provider.Capture(context.Background(), captureRequest(root, []string{"a.txt"})); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("changed"), 0644); err != nil {
		t.Fatal(err)
	}
	provider.afterRestorePath = func(int) error { return errors.New("stop") }
	req := restoreRequest(root, "restore-corrupt", []string{"a.txt"})
	if _, err := provider.Restore(context.Background(), req); err == nil {
		t.Fatal("expected injected stop")
	}
	claim := filepath.Join(provider.checkpointDir(req.CheckpointID), "restores", req.RestoreID, "claim.json")
	if err := os.WriteFile(claim, []byte(`{"schema_version":1,"unknown":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	provider.afterRestorePath = nil
	if _, err := provider.Restore(context.Background(), req); err == nil {
		t.Fatal("corrupt private restore ledger accepted")
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("sentinel"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Restore(context.Background(), req); err == nil {
		t.Fatal("corrupt ledger accepted on second retry")
	}
	raw, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(raw) != "sentinel" {
		t.Fatalf("corrupt ledger allowed mutation: %q", raw)
	}
}

func restoreRequest(root, restoreID string, paths []string) checkpointapp.ProviderRestoreRequest {
	return checkpointapp.ProviderRestoreRequest{
		RestoreID: restoreID, CheckpointID: "chk_01K00000000000000000000000",
		WorkspaceID: "ws_01K00000000000000000000000", Root: root, Paths: append([]string(nil), paths...),
	}
}

func newRestoreTestProvider(t *testing.T) *Provider {
	t.Helper()
	state := filepath.Join(t.TempDir(), "state")
	runtime := filepath.Join(t.TempDir(), "run")
	mustPrivateDir(t, state)
	mustPrivateDir(t, runtime)
	return New(state, runtime)
}

func TestRestoreRejectsSubmoduleBoundaryCreatedAfterCapture(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "src", "sub")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(nested, "file.txt")
	if err := os.WriteFile(path, []byte("captured"), 0644); err != nil {
		t.Fatal(err)
	}
	provider := newRestoreTestProvider(t)
	if _, err := provider.Capture(
		context.Background(),
		captureRequest(root, []string{"src/sub/file.txt"}),
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("changed"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(nested, ".git"),
		[]byte("gitdir: ../../.git/modules/sub\n"),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	_, err := provider.Restore(
		context.Background(),
		restoreRequest(root, "restore-submodule", []string{"src/sub/file.txt"}),
	)
	if !localFailureIs(err, failure.CheckpointSubmoduleBoundaryUnsupported) {
		t.Fatalf("submodule boundary err=%v", err)
	}
	raw, readErr := os.ReadFile(path)
	if readErr != nil || string(raw) != "changed" {
		t.Fatalf("submodule boundary allowed mutation raw=%q err=%v", raw, readErr)
	}
}
