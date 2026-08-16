package localfs

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	core "github.com/maemreyo/shellbeam/internal/core/checkpoint"
)

func TestRestoreBestEffortRecheckDetectsVisibleConcurrentChangeWithoutOverwritingIt(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file.txt")
	if err := os.WriteFile(path, []byte("captured"), 0644); err != nil {
		t.Fatal(err)
	}
	provider := newRestoreTestProvider(t)
	if _, err := provider.Capture(context.Background(), captureRequest(root, []string{"file.txt"})); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("expected-current"), 0644); err != nil {
		t.Fatal(err)
	}

	observed := make(chan struct{})
	changed := make(chan struct{})
	provider.beforeRestoreMutation = func(rel string) {
		if rel != "file.txt" {
			return
		}
		close(observed)
		<-changed
	}
	go func() {
		<-observed
		_ = os.WriteFile(path, []byte("raced-current"), 0644)
		close(changed)
	}()

	got, err := provider.Restore(context.Background(), restoreRequest(root, "restore-race", []string{"file.txt"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Paths) != 1 || got.Paths[0].Outcome != core.RestoreConflict || got.Paths[0].Reason != "current_changed" {
		t.Fatalf("race outcome=%#v", got.Paths)
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "raced-current" {
		t.Fatalf("raced writer was overwritten raw=%q err=%v", raw, err)
	}
}
