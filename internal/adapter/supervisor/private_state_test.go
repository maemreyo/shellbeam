package supervisor

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreparePrivateStateCreatesUserOnlyLayoutAndRoundTripsCapability(t *testing.T) {
	runtimeRoot := filepath.Join(t.TempDir(), "runtime")
	capability, err := NewCapability()
	if err != nil {
		t.Fatal(err)
	}
	layout, err := PreparePrivateState(runtimeRoot, "persistent-session-a", "generation-a", capability)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{runtimeRoot, layout.SupervisorsDir, layout.SessionDir} {
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
			t.Fatalf("unsafe dir %s info=%v err=%v", path, info, err)
		}
	}
	for _, path := range []string{layout.CapabilityPath, layout.MetadataPath} {
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
			t.Fatalf("unsafe file %s info=%v err=%v", path, info, err)
		}
	}
	loaded, err := LoadCapability(layout)
	if err != nil || !bytes.Equal(loaded.bytes(), capability.bytes()) {
		t.Fatalf("loaded capability mismatch err=%v", err)
	}
	metadata, err := LoadMetadata(layout)
	if err != nil || metadata.SessionID != "persistent-session-a" || metadata.GenerationID != "generation-a" || metadata.ProtocolVersion != ProtocolVersion {
		t.Fatalf("metadata=%#v err=%v", metadata, err)
	}
}

func TestPreparePrivateStateRejectsUnsafeExistingPathAndCapabilityReplacement(t *testing.T) {
	base := t.TempDir()
	unsafeRoot := filepath.Join(base, "unsafe")
	if err := os.Mkdir(unsafeRoot, 0755); err != nil {
		t.Fatal(err)
	}
	capability, err := NewCapability()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PreparePrivateState(unsafeRoot, "persistent-session-a", "generation-a", capability); err == nil {
		t.Fatal("unsafe existing runtime permissions accepted")
	}

	runtimeRoot := filepath.Join(base, "runtime")
	layout, err := PreparePrivateState(runtimeRoot, "persistent-session-a", "generation-a", capability)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(base, "secret-target")
	if err := os.WriteFile(target, []byte(strings.Repeat("x", CapabilityBytes)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(layout.CapabilityPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, layout.CapabilityPath); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCapability(layout); err == nil {
		t.Fatal("symlink capability accepted")
	}
}

func TestPreparePrivateStateRejectsSessionDirectorySymlink(t *testing.T) {
	root := filepath.Join(t.TempDir(), "runtime")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	supervisors := filepath.Join(root, "supervisors")
	if err := os.Mkdir(supervisors, 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(supervisors, "persistent-session-a")); err != nil {
		t.Fatal(err)
	}
	capability, err := NewCapability()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PreparePrivateState(root, "persistent-session-a", "generation-a", capability); err == nil {
		t.Fatal("session directory symlink accepted")
	}
}
