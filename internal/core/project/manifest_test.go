package project

import (
	"strings"
	"testing"
)

func TestManifestMinimalValidAndCanonicalFingerprintStable(t *testing.T) {
	first := []byte(`schema_version = 1

[project]
name = "demo"

[commands.test]
argv = ["go", "test", "./..."]
cwd = "."
kind = "test"
cost = "medium"
source_scope = "full"

[verification.profiles.checkpoint]
steps = ["test"]
`)
	second := []byte(`# formatting and order do not change semantics
schema_version = 1

[verification.profiles.checkpoint]
steps = ["test"]

[commands.test]
source_scope = "full"
cost = "medium"
kind = "test"
cwd = "."
argv = ["go", "test", "./..."]

[project]
name = "demo"
`)
	a, err := Parse(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Parse(second)
	if err != nil {
		t.Fatal(err)
	}
	if a.Fingerprint == "" || a.Fingerprint != b.Fingerprint {
		t.Fatalf("fingerprints differ: %q %q", a.Fingerprint, b.Fingerprint)
	}
	if got := a.Manifest.Commands["test"].Argv; len(got) != 3 || got[2] != "./..." {
		t.Fatalf("argv=%q", got)
	}
}

func TestManifestCompleteSample(t *testing.T) {
	data := []byte(`schema_version = 1

[project]
name = "shellbeam"

[toolchains.go]
version_source = "go.mod"

[commands.format_check]
argv = ["make", "fmt-check"]
kind = "format"
cost = "fast"

[commands.test_affected]
shell = "go test ./..."
cwd = "."
kind = "test"
cost = "medium"
source_scope = "affected"
depends_on = ["format_check"]
mutates_source = false
external_effect = false
timeout_ms = 30000

[[commands.test_affected.expected_outputs]]
path = ".build/result.txt"
kind = "file"
digest = "sha256"
required = false

[verification.profiles.coding]
steps = ["format_check", "test_affected"]

[environment]
relevant_presence = ["CGO_ENABLED", "GOFLAGS"]

[[outputs]]
path = ".build"
kind = "directory"
role = "build-cache"
`)
	parsed, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Manifest.Commands) != 2 || len(parsed.Manifest.VerificationProfiles) != 1 {
		t.Fatalf("manifest=%#v", parsed.Manifest)
	}
	if parsed.Manifest.Commands["test_affected"].Shell != "go test ./..." {
		t.Fatalf("shell command lost: %#v", parsed.Manifest.Commands["test_affected"])
	}
}

func TestManifestRejectsUnknownFieldsAndUnsupportedVersion(t *testing.T) {
	cases := []struct {
		name string
		data string
		code string
	}{
		{"unknown top", "schema_version=1\nmystery=true\n", CodeSchemaError},
		{"unknown command field", "schema_version=1\n[commands.test]\nargv=[\"go\",\"test\"]\nwat=true\n", CodeSchemaError},
		{"unsupported", "schema_version=2\n", CodeUnsupported},
		{"missing schema version", "[project]\nname=\"demo\"\n", CodeSchemaError},
		{"duplicate profile", "schema_version=1\n[verification.profiles.coding]\nsteps=[]\n[verification.profiles.coding]\nsteps=[]\n", CodeParseError},
		{"duplicate command", "schema_version=1\n[commands.test]\nargv=[\"go\",\"test\"]\n[commands.test]\nargv=[\"go\",\"test\",\"./...\"]\n", CodeParseError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.data))
			if !HasCode(err, tc.code) {
				t.Fatalf("err=%v want code=%s", err, tc.code)
			}
		})
	}
}

func TestManifestRejectsPathsReferencesCyclesAndExecutionShape(t *testing.T) {
	cases := []struct {
		name string
		data string
		code string
	}{
		{"absolute cwd", "schema_version=1\n[commands.test]\nargv=[\"go\",\"test\"]\ncwd=\"/tmp\"\n", CodePathEscape},
		{"escaping cwd", "schema_version=1\n[commands.test]\nargv=[\"go\",\"test\"]\ncwd=\"../outside\"\n", CodePathEscape},
		{"bad profile reference", "schema_version=1\n[verification.profiles.coding]\nsteps=[\"missing\"]\n", CodeUnknownCommand},
		{"bad dependency", "schema_version=1\n[commands.a]\nargv=[\"true\"]\ndepends_on=[\"missing\"]\n", CodeUnknownCommand},
		{"cycle", "schema_version=1\n[commands.a]\nargv=[\"true\"]\ndepends_on=[\"b\"]\n[commands.b]\nargv=[\"true\"]\ndepends_on=[\"a\"]\n", CodeDependencyCycle},
		{"argv and shell", "schema_version=1\n[commands.test]\nargv=[\"true\"]\nshell=\"true\"\n", CodeSchemaError},
		{"empty argv", "schema_version=1\n[commands.test]\nargv=[]\n", CodeSchemaError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.data))
			if !HasCode(err, tc.code) {
				t.Fatalf("err=%v want code=%s", err, tc.code)
			}
		})
	}
}

func TestManifestLimitsAndInvalidUTF8(t *testing.T) {
	var commands strings.Builder
	commands.WriteString("schema_version=1\n")
	for i := 0; i < MaxCommands+1; i++ {
		commands.WriteString("[commands.c")
		commands.WriteString(strings.Repeat("x", i/10))
		commands.WriteString(string(rune('a' + i%26)))
		commands.WriteString("]\nargv=[\"true\"]\n")
	}
	if _, err := Parse([]byte(commands.String())); !HasCode(err, CodeLimitExceeded) {
		t.Fatalf("command limit err=%v", err)
	}
	if _, err := Parse([]byte{0xff, 0xfe}); !HasCode(err, CodeParseError) {
		t.Fatalf("invalid utf8 err=%v", err)
	}
}

func FuzzManifestParser(f *testing.F) {
	f.Add([]byte("schema_version=1\n"))
	f.Add([]byte("schema_version=1\n[commands.test]\nargv=[\"go\",\"test\"]\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > MaxManifestBytes {
			data = data[:MaxManifestBytes]
		}
		_, _ = Parse(data)
	})
}
