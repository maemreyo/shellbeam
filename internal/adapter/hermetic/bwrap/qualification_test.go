package bwrap

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestToolchainManifestDigestIsCanonicalAndSensitiveToBytesLinksAndModes(t *testing.T) {
	root := buildReadonlyToolchainFixture(t)
	first, err := toolchainManifestDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := toolchainManifestDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("nondeterministic manifest: %s != %s", first, second)
	}

	mustChmodTreeWritable(t, root)
	if err := os.WriteFile(filepath.Join(root, "usr/bin/sh"), []byte("changed"), 0o555); err != nil {
		t.Fatal(err)
	}
	freezeFixture(t, root)
	changedBytes, err := toolchainManifestDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	if changedBytes == first {
		t.Fatal("toolchain byte drift did not change manifest digest")
	}

	mustChmodTreeWritable(t, root)
	if err := os.Remove(filepath.Join(root, "bin")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("usr/bin-alt", filepath.Join(root, "bin")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "usr/bin-alt"), 0o755); err != nil {
		t.Fatal(err)
	}
	freezeFixture(t, root)
	changedLink, err := toolchainManifestDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	if changedLink == changedBytes {
		t.Fatal("toolchain link drift did not change manifest digest")
	}
}

func TestToolchainManifestRejectsWritableTreeAndEscapingSymlink(t *testing.T) {
	root := buildReadonlyToolchainFixture(t)
	if err := os.Chmod(filepath.Join(root, "usr/bin/sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := toolchainManifestDigest(root); err == nil {
		t.Fatal("writable toolchain file accepted")
	}

	root = buildReadonlyToolchainFixture(t)
	mustChmodTreeWritable(t, root)
	if err := os.Remove(filepath.Join(root, "bin")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../outside", filepath.Join(root, "bin")); err != nil {
		t.Fatal(err)
	}
	freezeFixture(t, root)
	if _, err := toolchainManifestDigest(root); err == nil {
		t.Fatal("escaping toolchain symlink accepted")
	}
}

func TestRuntimeManifestDigestMatchesFrozenA0Shape(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a.so")
	b := filepath.Join(root, "b.so")
	if err := os.WriteFile(a, []byte("alpha"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("beta"), 0o444); err != nil {
		t.Fatal(err)
	}
	got, err := runtimeManifestDigest([]string{b, a, a})
	if err != nil {
		t.Fatal(err)
	}
	lines := []string{shaLine(t, a), shaLine(t, b)}
	payload := strings.Join(lines, "")
	sum := sha256.Sum256([]byte(payload))
	want := hex.EncodeToString(sum[:])
	if got != want {
		t.Fatalf("runtime manifest digest=%s want=%s payload=%q", got, want, payload)
	}
}

func TestToolchainExecutableRejectsEscapesAndRequiresExecutableRegularFile(t *testing.T) {
	root := buildReadonlyToolchainFixture(t)
	if !toolchainExecutable(root, "/bin/sh") {
		t.Fatal("qualified /bin/sh unavailable")
	}
	if toolchainExecutable(root, "/../../outside") {
		t.Fatal("toolchain escape accepted")
	}
	if toolchainExecutable(root, "/work/input") {
		t.Fatal("directory accepted as executable")
	}
}

func buildReadonlyToolchainFixture(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "toolchain")
	t.Cleanup(func() {
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err == nil && info.IsDir() {
				_ = os.Chmod(path, 0o700)
			}
			if err == nil && info.Mode().IsRegular() {
				_ = os.Chmod(path, info.Mode().Perm()|0o200)
			}
			return nil
		})
	})
	for _, rel := range []string{"usr/bin", "dev", "tmp", "work/input", "work/scratch"} {
		if err := os.MkdirAll(filepath.Join(root, rel), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "usr/bin/sh"), []byte("shell"), 0o555); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("usr/bin", filepath.Join(root, "bin")); err != nil {
		t.Fatal(err)
	}
	freezeFixture(t, root)
	return root
}

func freezeFixture(t *testing.T, root string) {
	t.Helper()
	var dirs []string
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			dirs = append(dirs, path)
			return nil
		}
		if info.Mode().IsRegular() {
			return os.Chmod(path, info.Mode().Perm()&^0o222)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		if err := os.Chmod(dirs[i], 0o555); err != nil {
			t.Fatal(err)
		}
	}
}

func mustChmodTreeWritable(t *testing.T, root string) {
	t.Helper()
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return os.Chmod(path, 0o755)
		}
		if info.Mode().IsRegular() {
			return os.Chmod(path, info.Mode().Perm()|0o200)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func shaLine(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]) + "  " + path + "\n"
}
