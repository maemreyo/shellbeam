package verification

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	core "github.com/maemreyo/shellbeam/internal/core/verification"
	"github.com/maemreyo/shellbeam/internal/core/workspace"
)

func writeGoFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
func goWorkspace(root string) workspace.Workspace {
	return workspace.Workspace{ID: workspace.WorkspaceID("ws_01K00000000000000000000000"), RepositoryID: workspace.RepositoryID("repo_01K00000000000000000000000"), Root: root}
}
func goGen() string { return "gen_1111111111111111111111111111111111111111111111111111111111111111" }

func TestGoRelationsDerivesTransitiveReverseImportersWithoutSubprocess(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, root, "go.mod", "module example.com/repo\n\ngo 1.24\n")
	writeGoFile(t, root, "a/a.go", "package a\n")
	writeGoFile(t, root, "b/b.go", "package b\nimport _ \"example.com/repo/a\"\n")
	writeGoFile(t, root, "c/c.go", "package c\nimport _ \"example.com/repo/b\"\n")
	got := NewGoRelationProvider(DefaultGoRelationLimits()).Derive(context.Background(), goWorkspace(root), goGen(), []string{"a/a.go"})
	if len(got.Domains) != 1 || got.Domains[0].Kind != core.DomainGoImportGraph || got.Domains[0].Coverage != core.CoverageComplete {
		t.Fatalf("domains=%#v diagnostics=%v", got.Domains, got.Diagnostics)
	}
	targets := map[string]bool{}
	for _, r := range got.Relations {
		targets[r.To.Value] = true
		if r.Basis != core.BasisImportGraph || r.DerivationAuthority != core.AuthorityMechanical {
			t.Fatalf("relation=%#v", r)
		}
	}
	if !targets["b"] || !targets["c"] {
		t.Fatalf("targets=%v relations=%#v", targets, got.Relations)
	}
}

func TestGoRelationsNestedModuleAndParseFailureDegradeCoverage(t *testing.T) {
	for name, setup := range map[string]func(*testing.T, string){"nested": func(t *testing.T, r string) {
		writeGoFile(t, r, "go.mod", "module example.com/repo\n")
		writeGoFile(t, r, "nested/go.mod", "module example.com/nested\n")
		writeGoFile(t, r, "a/a.go", "package a\n")
	}, "parse": func(t *testing.T, r string) {
		writeGoFile(t, r, "go.mod", "module example.com/repo\n")
		writeGoFile(t, r, "a/a.go", "package !!!\n")
	}} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			setup(t, root)
			got := NewGoRelationProvider(DefaultGoRelationLimits()).Derive(context.Background(), goWorkspace(root), goGen(), []string{"a/a.go"})
			if len(got.Domains) != 1 || got.Domains[0].Coverage == core.CoverageComplete || len(got.Diagnostics) == 0 {
				t.Fatalf("got=%#v", got)
			}
		})
	}
}

func TestGoRelationsBoundsPreserveDiscoveredFactsButDegradeCoverage(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, root, "go.mod", "module example.com/repo\n")
	writeGoFile(t, root, "a/a.go", "package a\n")
	writeGoFile(t, root, "b/b.go", "package b\nimport _ \"example.com/repo/a\"\n")
	limits := DefaultGoRelationLimits()
	limits.MaxFiles = 1
	got := NewGoRelationProvider(limits).Derive(context.Background(), goWorkspace(root), goGen(), []string{"a/a.go"})
	if len(got.Domains) != 1 || got.Domains[0].Coverage == core.CoverageComplete || len(got.Diagnostics) == 0 {
		t.Fatalf("got=%#v", got)
	}
}

func TestGoRelationsNonGoChangeInventsNoImportRelation(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, root, "go.mod", "module example.com/repo\n")
	writeGoFile(t, root, "a/a.go", "package a\n")
	got := NewGoRelationProvider(DefaultGoRelationLimits()).Derive(context.Background(), goWorkspace(root), goGen(), []string{"README.md"})
	if len(got.Relations) != 0 {
		t.Fatalf("relations=%#v", got.Relations)
	}
	if len(got.Domains) != 1 {
		t.Fatalf("domains=%#v", got.Domains)
	}
}

func TestGoRelationsDeletedPackageStillFindsImporterByDeterministicModulePath(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, root, "go.mod", "module example.com/repo\n")
	writeGoFile(t, root, "b/b.go", "package b\nimport _ \"example.com/repo/a\"\n")
	got := NewGoRelationProvider(DefaultGoRelationLimits()).Derive(context.Background(), goWorkspace(root), goGen(), []string{"a/a.go"})
	found := false
	for _, r := range got.Relations {
		if r.To.Kind == core.SubjectPath && r.To.Value == "b" {
			found = true
		}
	}
	if !found {
		t.Fatalf("deleted package importer missing: %#v", got)
	}
}
