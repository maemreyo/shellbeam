package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type sourceFixtureManifest struct {
	SchemaVersion int `json:"schema_version"`
	ValidV2       []struct {
		ID             string `json:"id"`
		Kind           string `json:"kind"`
		JSON           string `json:"json"`
		SemanticSHA256 string `json:"semantic_sha256"`
	} `json:"valid_v2"`
	LegacyV1 []struct {
		ID             string `json:"id"`
		Path           string `json:"path"`
		SemanticSHA256 string `json:"semantic_sha256"`
	} `json:"legacy_v1"`
}

func TestCandidateSourceContract(t *testing.T) {
	raw, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	for _, want := range []string{
		`jsonv2 "github.com/go-json-experiment/json"`,
		"jsonv2.RejectUnknownMembers(true)",
		"go1.26-pinned-json-library-boundary",
		"module_version",
		"canonicalSemanticDigest",
		"fixture_manifest_sha256",
		"candidate_mode",
		"command",
		"exit_status",
		"GoVersionCommand",
		`output("go", "version")`,
		`exec.Command("go", "env", "GOEXPERIMENT", "GOOS", "GOARCH", "CGO_ENABLED")`,
		`output("go", "list", "-m"`,
		"invalid-utf8", "duplicate-names", "unknown-names", "wrong-case", "trailing-json",
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("candidate source missing %q", want)
		}
	}
}

func TestFixtureManifestIsFrozenSemanticCorpus(t *testing.T) {
	raw, err := os.ReadFile("testdata/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var m sourceFixtureManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m.SchemaVersion != 2 {
		t.Fatalf("schema_version=%d want 2", m.SchemaVersion)
	}
	if len(m.ValidV2) < 12 {
		t.Fatalf("valid_v2=%d want >=12", len(m.ValidV2))
	}
	seen := map[string]bool{}
	ipc, mcp := 0, 0
	for _, f := range m.ValidV2 {
		if f.ID == "" || seen[f.ID] {
			t.Fatalf("bad/duplicate id %q", f.ID)
		}
		seen[f.ID] = true
		if len(f.SemanticSHA256) != 64 {
			t.Fatalf("%s semantic sha missing", f.ID)
		}
		switch f.Kind {
		case "ipc-v2":
			ipc++
			if !strings.Contains(f.JSON, `"ipc_version":2`) || !strings.Contains(f.JSON, `"kind":"`) {
				t.Fatalf("%s is not full ipc-v2 envelope: %s", f.ID, f.JSON)
			}
		case "mcp-v2":
			mcp++
		default:
			t.Fatalf("%s invalid kind %q", f.ID, f.Kind)
		}
	}
	if ipc < 8 || mcp < 4 {
		t.Fatalf("corpus coverage ipc=%d mcp=%d", ipc, mcp)
	}
	if len(m.LegacyV1) != 4 {
		t.Fatalf("legacy_v1=%d want 4", len(m.LegacyV1))
	}
	for _, f := range m.LegacyV1 {
		if f.ID == "" || f.Path == "" || len(f.SemanticSHA256) != 64 {
			t.Fatalf("bad legacy fixture %+v", f)
		}
	}
}
