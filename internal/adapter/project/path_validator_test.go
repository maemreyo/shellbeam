package project

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode"

	core "github.com/maemreyo/shellbeam/internal/core/project"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestRepoPathValidatorBindsExistingPathInsideWorkspace(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pkg", "file.txt"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	validator := NewRepoPathValidator()
	ws := adapterProjectWorkspace(root)
	fileDef := core.ParameterDefinition{Kind: core.ParameterRepoPath, Required: true, Exists: core.PathExistsFile}
	got, err := validator.ValidatePath(context.Background(), ws, fileDef, "pkg/../pkg/file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got.Value != "pkg/file.txt" || got.ObservationQuality != core.PathObservationExactAtBind || got.ProviderID != "" || got.ProviderVersion != 0 {
		t.Fatalf("validation=%#v", got)
	}
	dirDef := core.ParameterDefinition{Kind: core.ParameterRepoPath, Required: true, Exists: core.PathExistsDirectory}
	if got, err := validator.ValidatePath(context.Background(), ws, dirDef, "pkg"); err != nil || got.Value != "pkg" {
		t.Fatalf("dir validation=%#v err=%v", got, err)
	}
}

func TestRepoPathValidatorRejectsEscapeTypeMismatchMissingAndOptionShape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "dir"), 0o700); err != nil {
		t.Fatal(err)
	}
	validator := NewRepoPathValidator()
	ws := adapterProjectWorkspace(root)
	cases := []struct {
		value string
		def   core.ParameterDefinition
	}{
		{"../escape", core.ParameterDefinition{Kind: core.ParameterRepoPath, Required: true, Exists: core.PathExistsAny}},
		{"/absolute", core.ParameterDefinition{Kind: core.ParameterRepoPath, Required: true, Exists: core.PathExistsAny}},
		{"-flag", core.ParameterDefinition{Kind: core.ParameterRepoPath, Required: true, Exists: core.PathExistsAny}},
		{"missing", core.ParameterDefinition{Kind: core.ParameterRepoPath, Required: true, Exists: core.PathExistsAny}},
		{"escape", core.ParameterDefinition{Kind: core.ParameterRepoPath, Required: true, Exists: core.PathExistsAny}},
		{"dir", core.ParameterDefinition{Kind: core.ParameterRepoPath, Required: true, Exists: core.PathExistsFile}},
	}
	for _, tc := range cases {
		if got, err := validator.ValidatePath(context.Background(), ws, tc.def, tc.value); err == nil {
			t.Fatalf("accepted value=%q validation=%#v", tc.value, got)
		}
	}
}

func TestRepoPathBindingIsObservationNotRuntimeConfinement(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "target")
	if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	validator := NewRepoPathValidator()
	got, err := validator.ValidatePath(context.Background(), adapterProjectWorkspace(root), core.ParameterDefinition{Kind: core.ParameterRepoPath, Required: true, Exists: core.PathExistsFile}, "target")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "later"), path); err != nil {
		t.Fatal(err)
	}
	if got.Value != "target" || got.ObservationQuality != core.PathObservationExactAtBind {
		t.Fatalf("bind-time observation mutated: %#v", got)
	}
}

func adapterProjectWorkspace(root string) workspace.Workspace {
	now := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	return workspace.Workspace{
		SchemaVersion: workspace.SchemaVersion,
		ID:            workspace.WorkspaceID("ws_01K00000000000000000000000"),
		RepositoryID:  workspace.RepositoryID("repo_01K00000000000000000000000"),
		Label:         "repo", Root: root, GitDir: filepath.Join(root, ".git"), CreatedAt: now, LastSeenAt: now,
	}
}

func FuzzNormalizeRepoRelative(f *testing.F) {
	for _, seed := range []struct {
		value string
		allow bool
	}{{"pkg/file.go", false}, {"pkg/../pkg/file.go", false}, {"../escape", false}, {"/abs", false}, {"-flag", false}, {"-flag", true}, {"line\nbreak", true}} {
		f.Add(seed.value, seed.allow)
	}
	f.Fuzz(func(t *testing.T, value string, allowLeadingDash bool) {
		got, err := normalizeRepoRelative(value, allowLeadingDash)
		if err != nil {
			return
		}
		if got == "" || strings.HasPrefix(got, "/") || got == ".." || strings.HasPrefix(got, "../") || strings.Contains(got, "\\") {
			t.Fatalf("unsafe normalized path %q from %q", got, value)
		}
		if !allowLeadingDash && strings.HasPrefix(got, "-") {
			t.Fatalf("option-shaped path accepted: %q", got)
		}
		for _, r := range got {
			if r == 0 || unicode.IsControl(r) {
				t.Fatalf("control character survived normalization: %q", got)
			}
		}
	})
}
