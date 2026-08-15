package project

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoPackageValidatorAcceptsRepositoryLocalPackageForms(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal", "app"), 0o700); err != nil {
		t.Fatal(err)
	}
	validator := NewGoPackageValidator()
	ws := adapterProjectWorkspace(root)
	for input, want := range map[string]string{
		".":              ".",
		"./internal/app": "./internal/app",
		"./internal/...": "./internal/...",
		"./...":          "./...",
	} {
		got, err := validator.ValidatePackage(context.Background(), ws, "go", input)
		if err != nil {
			t.Fatalf("%q: %v", input, err)
		}
		if got.Value != want || got.ProviderID != "go-repo-package" || got.ProviderVersion != 1 {
			t.Fatalf("%q validation=%#v", input, got)
		}
	}
}

func TestGoPackageValidatorRejectsUnsafeUnavailableOrMissingPackages(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "pkg"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	validator := NewGoPackageValidator()
	ws := adapterProjectWorkspace(root)
	for _, input := range []string{"/abs", "../x", "./../x", "-run", "./missing", "./escape", "./pkg/../../x", "./pkg/../pkg", "./\x00bad"} {
		if got, err := validator.ValidatePackage(context.Background(), ws, "go", input); err == nil {
			t.Fatalf("accepted %q: %#v", input, got)
		}
	}
	if got, err := validator.ValidatePackage(context.Background(), ws, "node", "./pkg"); err == nil || got.ProviderID != "" {
		t.Fatalf("unsupported provider got=%#v err=%v", got, err)
	}
}

func FuzzNormalizeGoPackage(f *testing.F) {
	for _, seed := range []string{".", "./pkg", "./pkg/...", "./...", "../x", "./pkg/../pkg", "-run", "line\nbreak"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		normalized, base, err := normalizeGoPackage(value)
		if err != nil {
			return
		}
		if normalized != "." && !strings.HasPrefix(normalized, "./") {
			t.Fatalf("non dot-relative package %q from %q", normalized, value)
		}
		if base == "" || base == ".." || strings.HasPrefix(base, "../") || strings.HasPrefix(normalized, "-") {
			t.Fatalf("unsafe package normalized=%q base=%q from %q", normalized, base, value)
		}
		for _, segment := range strings.Split(strings.TrimPrefix(normalized, "./"), "/") {
			if segment == ".." {
				t.Fatalf("parent traversal survived normalization: %q", normalized)
			}
		}
	})
}
