package store

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestOpenRejectsUnsafeRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix only")
	}
	parent := t.TempDir()
	unsafe := filepath.Join(parent, "unsafe")
	if err := os.Mkdir(unsafe, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(unsafe, Limits{MaxSessions: 1, MaxSessionOutput: 1, MaxTotalState: 10, ControlReserve: 1}); err == nil {
		t.Fatal("unsafe mode accepted")
	}
	target := filepath.Join(parent, "target")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(parent, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(link, Limits{MaxSessions: 1, MaxSessionOutput: 1, MaxTotalState: 10, ControlReserve: 1}); err == nil {
		t.Fatal("symlink accepted")
	}
}

func TestOpenRejectsObservationObligationSymlink(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(filepath.Join(root, "observations"), 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "obligations-target")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "observations", "obligations")); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(root, Limits{MaxSessions: 1, MaxSessionOutput: 1024, MaxTotalState: 1 << 20, ControlReserve: 128}); err == nil {
		t.Fatal("observation obligation symlink accepted")
	}
}
