package structuredresult

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/maemreyo/shellbeam/internal/core/inputtrace"
)

func TestNormalizeFailureExcerptStripsANSIControlsAndPreservesText(t *testing.T) {
	raw := "before \x1b[31mred\x1b[0m after \x1b]0;title\x07done\t\r\nnext\x7f\u0085end"
	got, ok := NormalizeFailureExcerpt(raw, "jest", "/workspace/project")
	if !ok {
		t.Fatal("normalization rejected valid excerpt")
	}
	if want := "before red after done\nnextend"; got.Text != want {
		t.Fatalf("text=%q want=%q", got.Text, want)
	}
	if got.Namespace != "jest" || got.VocabularyVersion != 1 || got.Truncated || got.Redacted {
		t.Fatalf("metadata=%#v", got)
	}
}

func TestNormalizeFailureExcerptStripsOSCWithSTAndDropsIncompleteEscapes(t *testing.T) {
	for name, tc := range map[string]struct {
		raw  string
		want string
	}{
		"osc-st":         {raw: "left\x1b]title\x1b\\right", want: "leftright"},
		"incomplete-csi": {raw: "left\x1b[31", want: "left"},
		"unknown-esc":    {raw: "left\x1bXright", want: "leftXright"},
	} {
		t.Run(name, func(t *testing.T) {
			got, ok := NormalizeFailureExcerpt(tc.raw, "jest", "/workspace/project")
			if !ok || got.Text != tc.want {
				t.Fatalf("got=%#v ok=%v want=%q", got, ok, tc.want)
			}
		})
	}
}

func TestNormalizeFailureExcerptRejectsInvalidUTF8AndEmptyResult(t *testing.T) {
	if _, ok := NormalizeFailureExcerpt(string([]byte{'x', 0xff}), "jest", "/workspace/project"); ok {
		t.Fatal("invalid UTF-8 accepted")
	}
	if _, ok := NormalizeFailureExcerpt(" \t\r\x1b[31m ", "jest", "/workspace/project"); ok {
		t.Fatal("whitespace-only normalized excerpt accepted")
	}
}

func TestNormalizeFailureExcerptTruncatesOnRuneBoundary(t *testing.T) {
	raw := strings.Repeat("é", MaxFailureExcerptBytes)
	got, ok := NormalizeFailureExcerpt(raw, "jest", "/workspace/project")
	if !ok {
		t.Fatal("normalization rejected truncatable excerpt")
	}
	if !got.Truncated || len(got.Text) > MaxFailureExcerptBytes || !utf8.ValidString(got.Text) {
		t.Fatalf("truncation=%#v bytes=%d valid=%v", got, len(got.Text), utf8.ValidString(got.Text))
	}
	if len(got.Text) != MaxFailureExcerptBytes {
		t.Fatalf("bytes=%d want=%d", len(got.Text), MaxFailureExcerptBytes)
	}
}

func TestNormalizeFailureExcerptClassifiesAbsolutePathsBeforePersistence(t *testing.T) {
	root := "/workspace/project"

	inside, ok := NormalizeFailureExcerpt("boom at /workspace/project/src/a.ts:12:3", "jest", root)
	if !ok || inside.Text != "boom at src/a.ts:12:3" || inside.Redacted {
		t.Fatalf("inside=%#v ok=%v", inside, ok)
	}

	externalRaw := "/Users/alice/secrets/token.txt"
	external, ok := NormalizeFailureExcerpt("boom at "+externalRaw, "jest", root)
	if !ok || !external.Redacted || strings.Contains(external.Text, externalRaw) || !strings.Contains(external.Text, string(inputtrace.PathWorkspaceExternalRedacted)) {
		t.Fatalf("external=%#v ok=%v", external, ok)
	}

	systemRaw := "/usr/lib/libSystem.B.dylib"
	system, ok := NormalizeFailureExcerpt("loader "+systemRaw, "jest", root)
	if !ok || system.Redacted || strings.Contains(system.Text, systemRaw) || !strings.Contains(system.Text, string(inputtrace.PathSystemClassified)) {
		t.Fatalf("system=%#v ok=%v", system, ok)
	}
}

func TestNormalizeFailureExcerptWithoutPathDoesNotClaimRedaction(t *testing.T) {
	got, ok := NormalizeFailureExcerpt("expected true, received false", "vitest", "/workspace/project")
	if !ok || got.Text != "expected true, received false" || got.Redacted {
		t.Fatalf("got=%#v ok=%v", got, ok)
	}
}
