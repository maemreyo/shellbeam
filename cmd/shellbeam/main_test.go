package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/maemreyo/shellbeam/internal/buildinfo"
)

func TestRunVersionJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"version", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	var got buildinfo.Info
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Version == "" || got.Commit == "" || got.BuiltAt == "" {
		t.Fatalf("incomplete: %#v", got)
	}
}

func TestRunContract(t *testing.T) {
	tests := []struct {
		name string
		args []string
		code int
		want string
	}{
		{"version", []string{"version"}, 0, "shellbeam dev"},
		{"empty", nil, 2, "usage:"},
		{"unknown", []string{"wat"}, 2, "unknown command"},
		{"extra", []string{"version", "extra"}, 2, "usage:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out, err bytes.Buffer
			if got := run(tt.args, &out, &err); got != tt.code {
				t.Fatalf("code=%d", got)
			}
			if !strings.Contains(out.String()+err.String(), tt.want) {
				t.Fatalf("output=%q stderr=%q", out.String(), err.String())
			}
		})
	}
}
