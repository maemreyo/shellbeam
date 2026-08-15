package environment

import "testing"

func TestParseProbeVersionUsesExactToolSpecificGrammar(t *testing.T) {
	tests := []struct {
		kind   string
		output string
		want   string
	}{
		{"go", "go1.26.5\n", "go1.26.5"},
		{"node", "v22.14.0\n", "v22.14.0"},
		{"python", "Python 3.13.5\n", "3.13.5"},
		{"java", "openjdk version \"21.0.7\" 2025-04-15\nOpenJDK Runtime Environment\n", "21.0.7"},
		{"rust", "rustc 1.88.0 (6b00bc388 2025-06-23)\n", "1.88.0"},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			got, err := parseProbeVersion(tt.kind, []byte(tt.output))
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("version=%q want %q", got, tt.want)
			}
		})
	}
}

func TestParseProbeVersionRejectsMalformedOrAmbiguousOutput(t *testing.T) {
	tests := []struct {
		kind   string
		output string
	}{
		{"go", "go version go1.26.5 darwin/arm64\n"},
		{"node", "node v22.14.0\n"},
		{"python", "Python 3.13.5\nPython 3.12.0\n"},
		{"java", "openjdk 21.0.7\n"},
		{"rust", "rustc version 1.88.0\n"},
		{"ruby", "ruby 3.4.0\n"},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			if got, err := parseProbeVersion(tt.kind, []byte(tt.output)); err == nil {
				t.Fatalf("accepted malformed output as %q", got)
			}
		})
	}
}
