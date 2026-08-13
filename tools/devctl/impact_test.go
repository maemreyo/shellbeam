package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSelectImpact(t *testing.T) {
	cfg := impactConfig{
		Version: 1,
		Mappings: []impactMapping{
			{Glob: "api/schema/**", Suites: []string{"./api/schema", "./tests/contract"}},
			{Glob: "docs/**", Suites: []string{"contract:markdown"}},
		},
		Global: []string{"go.mod", "go.sum", "tools/devctl/**", "dev/**"},
	}
	tests := []struct {
		name       string
		changed    []string
		wantMode   string
		wantSuites []string
	}{
		{"docs only", []string{"docs/guide.md"}, "affected", []string{"contract:markdown"}},
		{"schema", []string{"api/schema/mcp-input-v2.json"}, "affected", []string{"./api/schema", "./tests/contract"}},
		{"package", []string{"internal/core/workspace/id.go"}, "affected", []string{"./internal/core/workspace"}},
		{"global", []string{"go.mod"}, "global", []string{"./..."}},
		{"none", nil, "empty", nil},
		{"deleted package file", []string{"internal/core/workspace/deleted.go"}, "affected", []string{"./internal/core/workspace"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectImpact(cfg, tt.changed)
			if got.Mode != tt.wantMode {
				t.Fatalf("mode=%q want %q", got.Mode, tt.wantMode)
			}
			if !reflect.DeepEqual(got.Suites, tt.wantSuites) {
				t.Fatalf("suites=%v want %v", got.Suites, tt.wantSuites)
			}
		})
	}
}

func TestDirtyTestDoesNotSelectAllOnNoChange(t *testing.T) {
	got := selectImpact(impactConfig{Version: 1}, nil)
	if got.Mode != "empty" || len(got.Suites) != 0 {
		t.Fatalf("empty dirty selection = %#v, want no suites", got)
	}
}

func TestSelectImpactOverlapsAreDeduplicatedAndDeterministic(t *testing.T) {
	cfg := impactConfig{
		Version: 1,
		Mappings: []impactMapping{
			{Glob: "internal/**", Suites: []string{"./tests/contract", "./internal/core/workspace"}},
			{Glob: "internal/core/**", Suites: []string{"./api/schema", "./internal/core/workspace"}},
		},
	}
	got := selectImpact(cfg, []string{"internal/core/workspace/z.go", "internal/core/workspace/a.go"})
	want := []string{"./api/schema", "./internal/core/workspace", "./tests/contract"}
	if !reflect.DeepEqual(got.Suites, want) {
		t.Fatalf("suites=%v want %v", got.Suites, want)
	}
	if len(got.Reasons) != 4 {
		t.Fatalf("reasons=%#v, want one per suite/mapping pair", got.Reasons)
	}
	for _, reason := range got.Reasons {
		if !reflect.DeepEqual(reason.Paths, []string{"internal/core/workspace/a.go", "internal/core/workspace/z.go"}) {
			t.Fatalf("reason paths not sorted/deterministic: %#v", reason)
		}
	}
}

func TestParseImpactConfigRejectsMalformedTOML(t *testing.T) {
	if _, err := parseImpactConfig([]byte("version = [")); err == nil {
		t.Fatal("expected malformed TOML error")
	}
}

func TestChangedFilesIncludesBothSidesOfRename(t *testing.T) {
	repo := newImpactTestRepo(t)
	oldPath := filepath.Join(repo, "internal", "old", "x.go")
	if err := os.MkdirAll(filepath.Dir(oldPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldPath, []byte("package old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitImpact(t, repo, "add", ".")
	gitImpact(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "base")
	if err := os.MkdirAll(filepath.Join(repo, "internal", "new"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitImpact(t, repo, "mv", "internal/old/x.go", "internal/new/x.go")

	got, err := changedFilesIn(repo, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"internal/new/x.go", "internal/old/x.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("changed=%v want %v", got, want)
	}
}

func TestChangedFilesBaseRefFailure(t *testing.T) {
	repo := newImpactTestRepo(t)
	if _, err := changedFilesIn(repo, "refs/heads/does-not-exist"); err == nil {
		t.Fatal("expected invalid base ref to fail")
	}
}

func newImpactTestRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	gitImpact(t, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitImpact(t, repo, "add", "README.md")
	gitImpact(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "initial")
	return repo
}

func gitImpact(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func TestRunDirtyWithNoChangeDoesNotInvokeGo(t *testing.T) {
	repo := newImpactTestRepo(t)
	if err := os.MkdirAll(filepath.Join(repo, "dev"), 0o755); err != nil {
		t.Fatal(err)
	}
	config := "version = 1\n\nglobal = [\"go.mod\", \"go.sum\"]\n"
	if err := os.WriteFile(filepath.Join(repo, "dev", "test-impact.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	gitImpact(t, repo, "add", "dev/test-impact.toml")
	gitImpact(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "impact config")

	logPath := filepath.Join(t.TempDir(), "go-called")
	binDir := t.TempDir()
	fakeGo := filepath.Join(binDir, "go")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + shellQuoteTest(logPath) + "\nexit 97\n"
	if err := os.WriteFile(fakeGo, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Chdir(repo)

	code, err := run([]string{"test", "--dirty", "--base", "HEAD", "--json"})
	if err != nil || code != 0 {
		t.Fatalf("run code=%d err=%v", code, err)
	}
	if data, err := os.ReadFile(logPath); err == nil {
		t.Fatalf("go was invoked for empty dirty selection: %s", data)
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func shellQuoteTest(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func TestRepositoryImpactConfigParses(t *testing.T) {
	cfg, err := loadImpactConfig("../../dev/test-impact.toml")
	if err != nil {
		t.Fatal(err)
	}
	got := selectImpact(cfg, []string{"tools/devctl/impact.go"})
	if got.Mode != "affected" || !reflect.DeepEqual(got.Suites, []string{"./tools/devctl"}) {
		t.Fatalf("devctl impact=%#v", got)
	}
}
