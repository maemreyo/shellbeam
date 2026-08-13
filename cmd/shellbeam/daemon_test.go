package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func TestSecondDaemonDoesNotReconcileLiveOwnersState(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "shellbeam-daemon-race-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	stateDir := filepath.Join(root, "state")
	runtimeDir := filepath.Join(root, "run")
	args := []string{"--state-dir", stateDir, "--runtime-dir", runtimeDir, "--shell", "/bin/sh"}

	ctx, cancel := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() { firstDone <- runDaemon(ctx, args) }()
	waitForPath(t, filepath.Join(runtimeDir, "daemon.sock"))

	repo, err := storeadapter.Open(stateDir, storeadapter.Limits{
		MaxSessions: 4, MaxSessionOutput: 1024, MaxTotalState: 1 << 20, ControlReserve: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	reservation := operation.Reservation{
		SchemaVersion: 1, OperationID: "live-operation", SessionID: "live-session",
		Fingerprint: "live-fingerprint", Command: "sleep 60", CWD: root,
		Shell: "/bin/sh", DaemonIncarnation: "live-daemon",
	}
	if _, _, result := repo.ReserveOperation(context.Background(), reservation); result.Err != nil {
		t.Fatal(result.Err)
	}
	before := sessionTree(t, filepath.Join(stateDir, "sessions", "live-session"))

	err = runDaemon(context.Background(), args)
	if err == nil || err.Error() != "daemon_already_running" {
		t.Fatalf("second daemon error = %v", err)
	}
	after := sessionTree(t, filepath.Join(stateDir, "sessions", "live-session"))
	if !bytes.Equal(before, after) {
		t.Fatalf("second daemon modified live state\nbefore: %q\nafter:  %q", before, after)
	}

	cancel()
	select {
	case err = <-firstDone:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("first daemon shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first daemon did not stop")
	}
}

func waitForPath(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Lstat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("path did not appear: %s", path)
}

func sessionTree(t *testing.T, root string) []byte {
	t.Helper()
	var paths []string
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			paths = append(paths, path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	var out []byte
	for _, path := range paths {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, rel...)
		out = append(out, 0)
		out = append(out, b...)
		out = append(out, 0)
	}
	return out
}
