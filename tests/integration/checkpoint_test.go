//go:build linux || darwin

package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	checkpointadapter "github.com/maemreyo/shellbeam/internal/adapter/checkpoint/localfs"
	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	checkpointapp "github.com/maemreyo/shellbeam/internal/app/checkpoint"
	checkpointcore "github.com/maemreyo/shellbeam/internal/core/checkpoint"
	"github.com/maemreyo/shellbeam/internal/core/failure"
)

const (
	e26IntegrationWorkspace  = "ws_01K00000000000000000000070"
	e26IntegrationRepository = "repo_01K00000000000000000000070"
)

func TestE26CheckpointCrashSafetyAcceptance(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(t.TempDir(), "state")
	runtime := filepath.Join(t.TempDir(), "run")
	if err := os.Mkdir(runtime, 0o700); err != nil {
		t.Fatal(err)
	}
	store := openE26IntegrationStore(t, state)
	source := &e26IntegrationWorkspaceSource{root: root}
	provider := checkpointadapter.New(state, runtime)
	service := checkpointapp.New(store, source, provider)
	secret := "e26-integration-secret"
	if err := os.WriteFile(filepath.Join(root, "secret.txt"), []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}

	request := checkpointcore.CreateRequest{CreateID: "e26-int-create", WorkspaceID: e26IntegrationWorkspace, Paths: []string{"secret.txt"}}
	first, err := service.Create(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	store = openE26IntegrationStore(t, state)
	service = checkpointapp.New(store, source, checkpointadapter.New(state, runtime))
	replayed, err := service.Create(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, replayed) {
		t.Fatalf("create replay changed metadata first=%#v replay=%#v", first, replayed)
	}
	listed, err := store.ListCheckpointMetadata(context.Background())
	if err != nil || len(listed) != 1 {
		t.Fatalf("checkpoint metadata=%#v err=%v", listed, err)
	}
	assertE26IntegrationPrivacy(t, state, first, secret)

	if err := os.WriteFile(filepath.Join(root, "secret.txt"), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	restoreRequest := checkpointcore.RestoreRequest{RestoreID: "e26-int-restore", CheckpointID: first.CheckpointID, Paths: []string{"secret.txt"}}
	restored, err := service.Restore(context.Background(), restoreRequest)
	if err != nil {
		t.Fatal(err)
	}
	if !restored.Complete || len(restored.Paths) != 1 || restored.Paths[0].Outcome != checkpointcore.RestoreRestored {
		t.Fatalf("restore=%#v", restored)
	}
	store = openE26IntegrationStore(t, state)
	service = checkpointapp.New(store, source, checkpointadapter.New(state, runtime))
	replayedRestore, err := service.Restore(context.Background(), restoreRequest)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored, replayedRestore) {
		t.Fatalf("restore replay changed truth first=%#v replay=%#v", restored, replayedRestore)
	}

	t.Run("provider-safety-matrix", func(t *testing.T) { testE26ProviderSafetyMatrix(t) })
	t.Run("retention-expiry", func(t *testing.T) { testE26RetentionExpiry(t) })
}

func testE26ProviderSafetyMatrix(t *testing.T) {
	t.Run("truthful-conflict-capability", func(t *testing.T) {
		provider, _, _ := newE26IntegrationProvider(t)
		got := provider.ConflictDetection()
		if got.RegularFile != checkpointcore.ConflictBestEffort || got.Symlink != checkpointcore.ConflictBestEffort || got.AbsentToFile != checkpointcore.ConflictBestEffort || got.DirectoryTree != checkpointcore.ConflictUnsupported {
			t.Fatalf("conflict matrix=%#v", got)
		}
	})
	t.Run("special-file", func(t *testing.T) {
		provider, root, _ := newE26IntegrationProvider(t)
		fifo := filepath.Join(root, "fifo")
		if err := syscall.Mkfifo(fifo, 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := provider.Capture(context.Background(), e26CaptureRequest(root, "chk_01K00000000000000000000071", []string{"fifo"}))
		assertE26Failure(t, err, failure.CheckpointPathUnsupported)
	})
	t.Run("symlink-parent-no-follow", func(t *testing.T) {
		provider, root, _ := newE26IntegrationProvider(t)
		outside := t.TempDir()
		if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("outside"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
			t.Fatal(err)
		}
		_, err := provider.Capture(context.Background(), e26CaptureRequest(root, "chk_01K00000000000000000000072", []string{"escape/secret"}))
		if err == nil {
			t.Fatal("symlink parent accepted")
		}
		raw, readErr := os.ReadFile(filepath.Join(outside, "secret"))
		if readErr != nil || string(raw) != "outside" {
			t.Fatalf("outside mutated raw=%q err=%v", raw, readErr)
		}
	})
	t.Run("nested-submodule", func(t *testing.T) {
		provider, root, _ := newE26IntegrationProvider(t)
		nested := filepath.Join(root, "sub")
		if err := os.MkdirAll(nested, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(nested, ".git"), []byte("gitdir: ../.git/modules/sub\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(nested, "file"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := provider.Capture(context.Background(), e26CaptureRequest(root, "chk_01K00000000000000000000073", []string{"sub/file"}))
		assertE26Failure(t, err, failure.CheckpointSubmoduleBoundaryUnsupported)
	})
	t.Run("oversize-regular", testE26OversizeRegular)
	t.Run("directory-restore-unsupported", func(t *testing.T) {
		provider, root, _ := newE26IntegrationProvider(t)
		if err := os.Mkdir(filepath.Join(root, "dir"), 0o755); err != nil {
			t.Fatal(err)
		}
		request := e26CaptureRequest(root, "chk_01K00000000000000000000075", []string{"dir"})
		if _, err := provider.Capture(context.Background(), request); err != nil {
			t.Fatal(err)
		}
		got, err := provider.Restore(context.Background(), checkpointapp.ProviderRestoreRequest{RestoreID: "e26-dir-restore", CheckpointID: request.CheckpointID, WorkspaceID: e26IntegrationWorkspace, Root: root, Paths: []string{"dir"}})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Paths) != 1 || got.Paths[0].Outcome != checkpointcore.RestoreUnsupported || got.Paths[0].Reason != "directory_tree" {
			t.Fatalf("directory restore=%#v", got.Paths)
		}
	})
}

func testE26OversizeRegular(t *testing.T) {
	provider, root, _ := newE26IntegrationProvider(t)
	large := filepath.Join(root, "large")
	file, err := os.OpenFile(large, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(checkpointcore.MaxRegularFileBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	_ = file.Close()
	_, err = provider.Capture(context.Background(), e26CaptureRequest(root, "chk_01K00000000000000000000074", []string{"large"}))
	assertE26Failure(t, err, failure.CheckpointBudgetExceeded)
}

func testE26RetentionExpiry(t *testing.T) {
	provider, root, _ := newE26IntegrationProvider(t)
	path := filepath.Join(root, "file.txt")
	if err := os.WriteFile(path, []byte("captured"), 0o600); err != nil {
		t.Fatal(err)
	}
	request := e26CaptureRequest(root, "chk_01K00000000000000000000076", []string{"file.txt"})
	if _, err := provider.Capture(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	sweep, err := provider.Sweep(context.Background(), checkpointapp.SweepRequest{Now: time.Now().UTC().Add(8 * 24 * time.Hour), MaxCheckpoints: checkpointcore.MaxRetainedCheckpoints, MaxBytes: checkpointcore.MaxPrivateProviderBytes, MaxAge: checkpointcore.MaxRetentionAge})
	if err != nil {
		t.Fatal(err)
	}
	if !containsE26String(sweep.ExpiredCheckpointIDs, request.CheckpointID) {
		t.Fatalf("expiry sweep=%#v", sweep)
	}
	status, err := provider.Inspect(context.Background(), request.CheckpointID)
	if err != nil || status.RetentionState != checkpointcore.RetentionExpired || status.Available {
		t.Fatalf("expired status=%#v err=%v", status, err)
	}
	_, err = provider.Restore(context.Background(), checkpointapp.ProviderRestoreRequest{RestoreID: "e26-expired-restore", CheckpointID: request.CheckpointID, WorkspaceID: e26IntegrationWorkspace, Root: root, Paths: []string{"file.txt"}})
	assertE26Failure(t, err, failure.CheckpointExpired)
}

type e26IntegrationWorkspaceSource struct {
	root       string
	generation int
}

func (s *e26IntegrationWorkspaceSource) ResolveFresh(context.Context, string) (checkpointapp.WorkspaceContext, error) {
	digit := "a"
	if s.generation > 0 {
		digit = "b"
	}
	return checkpointapp.WorkspaceContext{WorkspaceID: e26IntegrationWorkspace, RepositoryID: e26IntegrationRepository, Root: s.root, SourceGeneration: "gen_" + strings.Repeat(digit, 64)}, nil
}
func (s *e26IntegrationWorkspaceSource) InvalidateAfterMutation(context.Context, string) error {
	s.generation++
	return nil
}

func openE26IntegrationStore(t *testing.T, state string) *storeadapter.Repository {
	t.Helper()
	store, err := storeadapter.Open(state, storeadapter.Limits{MaxSessions: 8, MaxSessionOutput: 1 << 20, MaxTotalState: 32 << 20, ControlReserve: 1024})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func newE26IntegrationProvider(t *testing.T) (*checkpointadapter.Provider, string, string) {
	t.Helper()
	state := filepath.Join(t.TempDir(), "state")
	runtime := filepath.Join(t.TempDir(), "run")
	root := t.TempDir()
	if err := os.Mkdir(state, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(runtime, 0o700); err != nil {
		t.Fatal(err)
	}
	return checkpointadapter.New(state, runtime), root, state
}

func e26CaptureRequest(root, id string, paths []string) checkpointapp.CaptureRequest {
	return checkpointapp.CaptureRequest{CheckpointID: id, WorkspaceID: e26IntegrationWorkspace, RepositoryID: e26IntegrationRepository, Root: root, SourceGeneration: "gen_" + strings.Repeat("a", 64), Paths: paths}
}

func assertE26Failure(t *testing.T, err error, want failure.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected %s", want)
	}
	got := failure.Public(err)
	if got.Code != want {
		t.Fatalf("failure code=%s want=%s err=%v", got.Code, want, err)
	}
}

func assertE26IntegrationPrivacy(t *testing.T, state string, checkpoint checkpointcore.Checkpoint, secret string) {
	t.Helper()
	encoded, err := json.Marshal(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(secret))
	hash := hex.EncodeToString(sum[:])
	for _, forbidden := range []string{secret, hash} {
		if bytes.Contains(encoded, []byte(forbidden)) {
			t.Fatalf("public checkpoint leaked %q: %s", forbidden, encoded)
		}
	}
	var public bytes.Buffer
	err = filepath.WalkDir(state, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == filepath.Join(state, "checkpoint-content") {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		public.Write(raw)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{secret, hash} {
		if bytes.Contains(public.Bytes(), []byte(forbidden)) {
			t.Fatalf("public durable state leaked %q", forbidden)
		}
	}
}

func containsE26String(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
