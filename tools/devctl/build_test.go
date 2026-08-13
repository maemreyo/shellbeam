package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildPublicationUsesIsolatedStagingAndPublishesAtomically(t *testing.T) {
	root := t.TempDir()
	published := filepath.Join(root, ".build", "shellbeam")
	if err := os.MkdirAll(filepath.Dir(published), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(published, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}

	got, err := buildPublication(root, "source-digest", "build-123", func(output string) error {
		wantDir := filepath.Join(root, ".build", "workspaces", "source-digest", "build-123")
		if filepath.Dir(output) != wantDir {
			t.Fatalf("build output dir=%q want %q", filepath.Dir(output), wantDir)
		}
		return os.WriteFile(output, []byte("new"), 0o700)
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.StagingDir != filepath.Join(root, ".build", "workspaces", "source-digest", "build-123") {
		t.Fatalf("staging=%q", got.StagingDir)
	}
	if got.PublishedPath != published || got.CacheMode != "go-native" {
		t.Fatalf("build evidence=%#v", got)
	}
	if data, err := os.ReadFile(published); err != nil || string(data) != "new" {
		t.Fatalf("published=%q err=%v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(got.StagingDir, "shellbeam")); err != nil || string(data) != "new" {
		t.Fatalf("staging artifact=%q err=%v", data, err)
	}
}

func TestBuildPublicationFailureDoesNotClobberPublishedBinary(t *testing.T) {
	root := t.TempDir()
	published := filepath.Join(root, ".build", "shellbeam")
	if err := os.MkdirAll(filepath.Dir(published), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(published, []byte("known-good"), 0o700); err != nil {
		t.Fatal(err)
	}

	_, err := buildPublication(root, "digest", "failed-build", func(output string) error {
		if err := os.WriteFile(output, []byte("partial"), 0o700); err != nil {
			return err
		}
		return errors.New("compiler failed")
	})
	if err == nil || !strings.Contains(err.Error(), "compiler failed") {
		t.Fatalf("err=%v", err)
	}
	if data, err := os.ReadFile(published); err != nil || string(data) != "known-good" {
		t.Fatalf("published changed after failed build: %q err=%v", data, err)
	}
}

func TestGoBuildArgsAreIncremental(t *testing.T) {
	args := goBuildArgs("/tmp/stage/shellbeam")
	joined := " " + strings.Join(args, " ") + " "
	if strings.Contains(joined, " -a ") {
		t.Fatalf("build forces cache bypass: %v", args)
	}
	if len(args) < 2 || args[0] != "build" {
		t.Fatalf("unexpected args: %v", args)
	}
	foundOutput := false
	for i := range args {
		if args[i] == "-o" && i+1 < len(args) && args[i+1] == "/tmp/stage/shellbeam" {
			foundOutput = true
		}
	}
	if !foundOutput {
		t.Fatalf("staging output missing: %v", args)
	}
}

func TestRunDirtyBuildPublishesFromSourceBoundStaging(t *testing.T) {
	repo := newImpactTestRepo(t)
	if err := os.MkdirAll(filepath.Join(repo, "dev"), 0o755); err != nil {
		t.Fatal(err)
	}
	config := "version = 1\nglobal = [\"go.mod\", \"go.sum\"]\n\n[[mapping]]\nglob = \"cmd/**\"\nsuites = [\"./cmd/shellbeam\"]\n"
	if err := os.WriteFile(filepath.Join(repo, "dev", "test-impact.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	gitImpact(t, repo, "add", "dev/test-impact.toml")
	gitImpact(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "impact config")
	if err := os.MkdirAll(filepath.Join(repo, "cmd", "shellbeam"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "cmd", "shellbeam", "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	binDir := t.TempDir()
	fakeGo := filepath.Join(binDir, "go")
	script := `#!/bin/sh
out=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-o" ]; then out="$2"; shift 2; continue; fi
  shift
done
[ -n "$out" ] || exit 96
printf 'fake-binary' > "$out"
chmod 700 "$out"
`
	if err := os.WriteFile(fakeGo, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Chdir(repo)

	code, err := run([]string{"build", "--dirty", "--base", "HEAD", "--json"})
	if err != nil || code != 0 {
		t.Fatalf("run code=%d err=%v", code, err)
	}
	published := filepath.Join(repo, ".build", "shellbeam")
	if data, err := os.ReadFile(published); err != nil || string(data) != "fake-binary" {
		t.Fatalf("published=%q err=%v", data, err)
	}
	matches, err := filepath.Glob(filepath.Join(repo, ".build", "workspaces", "*", "*", "shellbeam"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("staged artifacts=%v err=%v", matches, err)
	}
}
