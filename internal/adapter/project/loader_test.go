package project

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	core "github.com/maemreyo/shellbeam/internal/core/project"
)

func TestManifestLoaderAbsentValidOversizedAndNoUpwardSearch(t *testing.T) {
	parent := t.TempDir()
	child := filepath.Join(parent, "child")
	if err := os.MkdirAll(filepath.Join(parent, ".shellbeam"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(child, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, ".shellbeam", "project.toml"), []byte("schema_version=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	loader := NewLoader()
	absent := loader.Load(context.Background(), child)
	if absent.State != core.LoadAbsent {
		t.Fatalf("upward search occurred: %#v", absent)
	}

	if err := os.MkdirAll(filepath.Join(child, ".shellbeam"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(child, ".shellbeam", "project.toml")
	if err := os.WriteFile(path, []byte("schema_version=1\n[commands.test]\nargv=[\"true\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	valid := loader.Load(context.Background(), child)
	if valid.State != core.LoadValid || valid.Parsed == nil || valid.ManifestDigest == "" {
		t.Fatalf("valid=%#v", valid)
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("x", core.MaxManifestBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	tooLarge := loader.Load(context.Background(), child)
	if tooLarge.State != core.LoadInvalid || tooLarge.Code != core.CodeTooLarge {
		t.Fatalf("tooLarge=%#v", tooLarge)
	}
}

func TestManifestLoaderRejectsEscapingSymlink(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "project.toml")
	if err := os.WriteFile(outside, []byte("schema_version=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".shellbeam"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, ".shellbeam", "project.toml")); err != nil {
		t.Fatal(err)
	}
	got := NewLoader().Load(context.Background(), root)
	if got.State != core.LoadInvalid || got.Code != core.CodePathEscape {
		t.Fatalf("symlink=%#v", got)
	}
}

func TestInspectionDoesNotExecuteManifestCommand(t *testing.T) {
	root := t.TempDir()
	sentinel := filepath.Join(root, "SENTINEL")
	if err := os.MkdirAll(filepath.Join(root, ".shellbeam"), 0o700); err != nil {
		t.Fatal(err)
	}
	data := "schema_version=1\n[commands.never_run]\nshell=\"touch " + sentinel + "\"\nkind=\"test\"\n"
	if err := os.WriteFile(filepath.Join(root, ".shellbeam", "project.toml"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	got := NewLoader().Load(context.Background(), root)
	if got.State != core.LoadValid {
		t.Fatalf("load=%#v", got)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("manifest command executed during load: %v", err)
	}
}

func TestLoaderDiscoveryFingerprintTracksExactManifestBytes(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".shellbeam"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".shellbeam", "project.toml")
	first := []byte("schema_version=1\n[commands.test]\nargv=[\"true\"]\n")
	second := []byte("# formatting-only change\nschema_version = 1\n[commands.test]\nargv = [\"true\"]\n")
	if err := os.WriteFile(path, first, 0o600); err != nil {
		t.Fatal(err)
	}
	loader := NewLoader()
	a := loader.Load(context.Background(), root)
	if err := os.WriteFile(path, second, 0o600); err != nil {
		t.Fatal(err)
	}
	b := loader.Load(context.Background(), root)
	if a.State != core.LoadValid || b.State != core.LoadValid || a.Parsed == nil || b.Parsed == nil {
		t.Fatalf("a=%#v b=%#v", a, b)
	}
	if a.Parsed.Fingerprint != b.Parsed.Fingerprint {
		t.Fatalf("canonical semantic fingerprint changed: %s %s", a.Parsed.Fingerprint, b.Parsed.Fingerprint)
	}
	if a.DiscoveryFingerprint == b.DiscoveryFingerprint {
		t.Fatal("exact-byte discovery fingerprint did not change")
	}
	if a.DiscoveryFingerprint != a.ManifestDigest || b.DiscoveryFingerprint != b.ManifestDigest {
		t.Fatalf("discovery/digest drift: a=%#v b=%#v", a, b)
	}
}
