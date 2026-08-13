package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestForbiddenPackageName(t *testing.T) {
	if !forbiddenPath("internal/common/x.go") || forbiddenPath("internal/core/session/x.go") {
		t.Fatal("name policy")
	}
}

func TestFileLimit(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "x.go")
	if err := os.WriteFile(p, []byte("package x\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := checkFile(p); err != nil {
		t.Fatal(err)
	}
}

func TestSourceFingerprintIgnoresGitExcludedDirectorySymlink(t *testing.T) {
	root := t.TempDir()
	cmd := exec.Command("git", "init", "-q", root)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(root, "source.go"), []byte("package source\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("git", "-C", root, "add", "source.go")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, out)
	}
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "index"), []byte("one"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, ".codegraph")); err != nil {
		t.Fatal(err)
	}
	exclude := filepath.Join(root, ".git", "info", "exclude")
	if err := os.WriteFile(exclude, []byte(".codegraph\n"), 0600); err != nil {
		t.Fatal(err)
	}

	before, err := sourceFingerprint(root)
	if err != nil {
		t.Fatalf("fingerprint with excluded directory symlink: %v", err)
	}
	if err := os.WriteFile(filepath.Join(target, "index"), []byte("two"), 0600); err != nil {
		t.Fatal(err)
	}
	after, err := sourceFingerprint(root)
	if err != nil {
		t.Fatalf("fingerprint after metadata update: %v", err)
	}
	if before != after {
		t.Fatal("git-excluded workspace metadata changed source fingerprint")
	}
}

func TestSourceFingerprintDoesNotFollowDirectorySymlink(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "source.go"), []byte("package source\n"), 0600); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "index"), []byte("one"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "workspace-index")); err != nil {
		t.Fatal(err)
	}
	before, err := sourceFingerprint(root)
	if err != nil {
		t.Fatalf("fingerprint directory symlink: %v", err)
	}
	if err := os.WriteFile(filepath.Join(target, "index"), []byte("two"), 0600); err != nil {
		t.Fatal(err)
	}
	after, err := sourceFingerprint(root)
	if err != nil {
		t.Fatalf("fingerprint after symlink target update: %v", err)
	}
	if before != after {
		t.Fatal("fingerprint followed directory symlink target")
	}
}
