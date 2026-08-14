package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommitGateRejectsUndefinedIdentifier(t *testing.T) {
	repo := newCommitGateRepo(t)
	writeCommitGateFile(t, repo, "internal/sample/sample.go", "package sample\n\nvar Value = MissingIdentifier\n")
	gitImpact(t, repo, "add", "internal/sample/sample.go")
	t.Chdir(repo)

	code, err := run([]string{"commit-gate", "--json"})
	if err == nil || code == 0 {
		t.Fatalf("commit gate accepted compiler error: code=%d err=%v", code, err)
	}
	if !strings.Contains(err.Error(), "go test") || !strings.Contains(err.Error(), "undefined: MissingIdentifier") {
		t.Fatalf("commit gate error does not preserve compiler evidence: %v", err)
	}
}

func TestCommitGateRunsAffectedTestAndVetWithoutFullSuite(t *testing.T) {
	repo := newCommitGateRepo(t)
	writeCommitGateFile(t, repo, "internal/sample/sample.go", "package sample\n\nvar Value = 1\n")
	gitImpact(t, repo, "add", "internal/sample/sample.go")

	logPath := filepath.Join(t.TempDir(), "go.log")
	binDir := t.TempDir()
	fakeGo := filepath.Join(binDir, "go")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + shellQuoteTest(logPath) + "\nexit 0\n"
	if err := os.WriteFile(fakeGo, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Chdir(repo)

	code, err := run([]string{"commit-gate", "--json"})
	if err != nil || code != 0 {
		t.Fatalf("commit gate code=%d err=%v", code, err)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.FieldsFunc(strings.TrimSpace(string(data)), func(r rune) bool { return r == '\n' })
	want := []string{"test ./internal/sample", "vet ./internal/sample"}
	if len(lines) != len(want) {
		t.Fatalf("go calls=%q want %q", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("go calls=%q want %q", lines, want)
		}
		if strings.Contains(lines[i], "./...") {
			t.Fatalf("commit gate broadened to full suite: %q", lines[i])
		}
	}
}

func TestTrackedPreCommitHookRunsCommitGate(t *testing.T) {
	data, err := os.ReadFile("../../.githooks/pre-commit")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "devctl commit-gate") {
		t.Fatalf("pre-commit hook does not invoke commit gate: %q", text)
	}
	if strings.Contains(text, "git rev-parse --local-env-vars") {
		t.Fatalf("pre-commit hook must preserve staged-index Git context for devctl: %q", text)
	}
}

func TestCommitGateChildEnvironmentDropsGitRepositoryState(t *testing.T) {
	blocked := []string{
		"GIT_ALTERNATE_OBJECT_DIRECTORIES",
		"GIT_CONFIG",
		"GIT_CONFIG_PARAMETERS",
		"GIT_CONFIG_COUNT",
		"GIT_OBJECT_DIRECTORY",
		"GIT_DIR",
		"GIT_WORK_TREE",
		"GIT_IMPLICIT_WORK_TREE",
		"GIT_GRAFT_FILE",
		"GIT_INDEX_FILE",
		"GIT_NO_REPLACE_OBJECTS",
		"GIT_REPLACE_REF_BASE",
		"GIT_PREFIX",
		"GIT_SHALLOW_FILE",
		"GIT_COMMON_DIR",
	}
	env := []string{"PATH=/bin", "HOME=/tmp/home"}
	for _, key := range blocked {
		env = append(env, key+"=/tmp/value")
	}
	got := cleanGitChildEnv(env)
	joined := "\n" + strings.Join(got, "\n") + "\n"
	for _, key := range blocked {
		if strings.Contains(joined, "\n"+key+"=") {
			t.Fatalf("child environment retained %s: %v", key, got)
		}
	}
	for _, keep := range []string{"PATH=/bin", "HOME=/tmp/home"} {
		if !strings.Contains(joined, "\n"+keep+"\n") {
			t.Fatalf("child environment dropped unrelated %s: %v", keep, got)
		}
	}
}

func newCommitGateRepo(t *testing.T) string {
	t.Helper()
	repo := newImpactTestRepo(t)
	writeCommitGateFile(t, repo, "go.mod", "module example.invalid/commitgate\n\ngo 1.26.0\n")
	writeCommitGateFile(t, repo, "dev/test-impact.toml", "version = 1\nglobal = [\"go.mod\", \"go.sum\"]\n")
	gitImpact(t, repo, "add", "go.mod", "dev/test-impact.toml")
	gitImpact(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "devctl config")
	return repo
}

func writeCommitGateFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
