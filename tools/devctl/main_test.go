package main

import (
	"os"
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
