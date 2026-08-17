package media

import (
	"strings"
	"testing"
)

func TestParseLogicalPathPreservesRawAndComponents(t *testing.T) {
	raw := "artifacts/settings.png"
	got, err := ParseLogicalPath(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Raw != raw || len(got.Components) != 2 || got.Components[0] != "artifacts" || got.Components[1] != "settings.png" {
		t.Fatalf("parsed=%#v", got)
	}
	other, err := ParseLogicalPath(raw)
	if err != nil {
		t.Fatal(err)
	}
	got.Components[0] = "mutated"
	if other.Components[0] != "artifacts" {
		t.Fatalf("component slices alias: %#v %#v", got, other)
	}
}

func TestParseLogicalPathRejectsUnsafeRawForms(t *testing.T) {
	invalid := map[string]string{
		"empty":           "",
		"absolute":        "/a/b",
		"backslash":       `a\\b`,
		"nul":             "a\x00b",
		"empty-component": "a//b",
		"dot":             "a/./b",
		"dotdot":          "a/../b",
		"trailing-slash":  "a/b/",
		"invalid-utf8":    string([]byte{0xff}),
	}
	for name, raw := range invalid {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseLogicalPath(raw); err == nil {
				t.Fatalf("accepted %q", raw)
			}
		})
	}
}

func TestParseLogicalPathByteAndComponentBounds(t *testing.T) {
	if _, err := ParseLogicalPath(strings.Repeat("a", MaxPathBytes)); err != nil {
		t.Fatalf("max bytes rejected: %v", err)
	}
	if _, err := ParseLogicalPath(strings.Repeat("a", MaxPathBytes+1)); err == nil {
		t.Fatal("max bytes + 1 accepted")
	}
	if _, err := ParseLogicalPath(strings.Repeat("é", MaxPathBytes/2)); err != nil {
		t.Fatalf("exact UTF-8 byte bound rejected: %v", err)
	}
	if _, err := ParseLogicalPath(strings.Repeat("é", MaxPathBytes/2+1)); err == nil {
		t.Fatal("UTF-8 byte overflow accepted")
	}
	parts := make([]string, MaxPathComponents)
	for i := range parts {
		parts[i] = "a"
	}
	if _, err := ParseLogicalPath(strings.Join(parts, "/")); err != nil {
		t.Fatalf("max components rejected: %v", err)
	}
	parts = append(parts, "a")
	if _, err := ParseLogicalPath(strings.Join(parts, "/")); err == nil {
		t.Fatal("max components + 1 accepted")
	}
}

func TestValidateCWDBounds(t *testing.T) {
	valid := []string{"/", "/tmp", "/" + strings.Repeat("a", MaxCWDBytes-1)}
	for _, cwd := range valid {
		if err := ValidateCWD(cwd); err != nil {
			t.Fatalf("valid cwd %q: %v", cwd, err)
		}
	}
	invalid := []string{"", "relative", "a/b", "/a\x00b", string([]byte{'/', 0xff}), "/" + strings.Repeat("a", MaxCWDBytes)}
	for _, cwd := range invalid {
		if err := ValidateCWD(cwd); err == nil {
			t.Fatalf("invalid cwd accepted: %q", cwd)
		}
	}
}
