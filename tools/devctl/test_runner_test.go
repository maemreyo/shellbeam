package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunGoTestIsolatesShellbeamPerformancePackage(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "go.log")
	binDir := t.TempDir()
	fakeGo := filepath.Join(binDir, "go")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + shellQuoteTest(logPath) + "\nexit 0\n"
	if err := os.WriteFile(fakeGo, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	packages := []string{
		"github.com/maemreyo/shellbeam/internal/adapter/store",
		"github.com/maemreyo/shellbeam/cmd/shellbeam",
		"github.com/maemreyo/shellbeam/internal/app/daemon",
	}
	if err := runGoTest(packages, false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.FieldsFunc(strings.TrimSpace(string(data)), func(r rune) bool { return r == '\n' })
	want := []string{
		"test github.com/maemreyo/shellbeam/internal/adapter/store github.com/maemreyo/shellbeam/internal/app/daemon",
		"test github.com/maemreyo/shellbeam/cmd/shellbeam",
	}
	if len(lines) != len(want) {
		t.Fatalf("go calls=%q want %q", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("go calls=%q want %q", lines, want)
		}
	}
}

func TestShellbeamPerformancePackageRecognition(t *testing.T) {
	for _, tc := range []struct {
		pkg  string
		want bool
	}{
		{pkg: "./cmd/shellbeam", want: true},
		{pkg: "github.com/maemreyo/shellbeam/cmd/shellbeam", want: true},
		{pkg: "github.com/maemreyo/shellbeam/internal/app/daemon", want: false},
	} {
		if got := isShellbeamPerformancePackage(tc.pkg); got != tc.want {
			t.Fatalf("isShellbeamPerformancePackage(%q)=%v want %v", tc.pkg, got, tc.want)
		}
	}
}
