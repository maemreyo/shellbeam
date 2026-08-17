package localfs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestProbeIsNoCreateAndRejectsUnsafeExistingPrivateRoot(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(state, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Probe(state); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(state, "checkpoint-content")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("probe created checkpoint-content err=%v", err)
	}
	content := filepath.Join(state, "checkpoint-content")
	if err := os.Mkdir(content, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(content, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Probe(state); err == nil {
		t.Fatal("unsafe checkpoint-content root passed health probe")
	}
}
