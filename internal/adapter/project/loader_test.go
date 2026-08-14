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
