package mutationscope

import (
	"strings"
	"testing"
)

func TestNormalizeSelectorsCanonicalizesAndSorts(t *testing.T) {
	got, err := NormalizeSelectors([]string{"tests/auth/**", "**", "src/auth/file.go"})
	if err != nil {
		t.Fatalf("NormalizeSelectors: %v", err)
	}
	want := []string{"**", "src/auth/file.go", "tests/auth/**"}
	if len(got) != len(want) {
		t.Fatalf("got=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got=%v want=%v", got, want)
		}
	}
}

func TestNormalizeSelectorsRejectsDuplicatesAfterCanonicalization(t *testing.T) {
	if _, err := NormalizeSelectors([]string{"src/auth/**", "src/auth/**"}); err == nil {
		t.Fatal("duplicate selector accepted")
	}
}

func TestNormalizeSelectorsRejectsInvalidForms(t *testing.T) {
	invalid := []string{
		"", "/abs", `src\\auth`, "./src", "src/../auth", "src//auth",
		"src/*/auth", "src/?", "src/[ab]", "src/{a,b}", "src/**/tail",
		"src/***", "src/\x00auth", "src/\nauth", strings.Repeat("a", MaxSelectorBytes+1),
		string([]byte{0xff, 'a'}),
	}
	for _, input := range invalid {
		t.Run(strings.ReplaceAll(input, "/", "_"), func(t *testing.T) {
			if _, err := NormalizeSelectors([]string{input}); err == nil {
				t.Fatalf("invalid selector accepted: %q", input)
			}
		})
	}
}

func TestSelectorsOverlapMatrix(t *testing.T) {
	tests := []struct {
		name string
		a, b []string
		want bool
	}{
		{"whole", []string{"**"}, []string{"a/b"}, true},
		{"exact equal", []string{"a/b"}, []string{"a/b"}, true},
		{"exact disjoint", []string{"a/b"}, []string{"a/c"}, false},
		{"exact in subtree", []string{"a/b/c"}, []string{"a/b/**"}, true},
		{"subtree root exact", []string{"a/b/**"}, []string{"a/b"}, true},
		{"exact outside subtree", []string{"a/bc"}, []string{"a/b/**"}, false},
		{"subtree equal", []string{"a/**"}, []string{"a/**"}, true},
		{"subtree ancestor", []string{"a/**"}, []string{"a/b/**"}, true},
		{"subtree reverse ancestor", []string{"a/b/**"}, []string{"a/**"}, true},
		{"subtree disjoint", []string{"a/b/**"}, []string{"a/c/**"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := SelectorsOverlap(tc.a, tc.b); got != tc.want {
				t.Fatalf("got=%v want=%v", got, tc.want)
			}
			if got := SelectorsOverlap(tc.b, tc.a); got != tc.want {
				t.Fatalf("reverse got=%v want=%v", got, tc.want)
			}
		})
	}
}
