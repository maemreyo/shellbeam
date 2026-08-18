//go:build darwin || linux

package delegatedtmux

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeSocketPathIsDeterministicShortAndPrivate(t *testing.T) {
	base, err := os.MkdirTemp("/tmp", "sb5-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	a, err := ensureRuntimeSocket(base, "dtmux_abcdef")
	if err != nil {
		t.Fatal(err)
	}
	b, err := ensureRuntimeSocket(base, "dtmux_abcdef")
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("socket paths differ: %q %q", a, b)
	}
	if len(a) >= 100 {
		t.Fatalf("socket path too long: %d %s", len(a), a)
	}
	info, err := os.Stat(filepath.Dir(a))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("runtime mode=%#o", info.Mode().Perm())
	}
	if _, err := ensureRuntimeSocket(base, "../bad"); err == nil {
		t.Fatal("runtime traversal accepted")
	}
}
