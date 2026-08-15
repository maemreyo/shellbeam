package project

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestManifestV2RequirementsAndTypedParametersParse(t *testing.T) {
	data := []byte(`schema_version = 2

[toolchains.go]
version_source = "go.mod"

[requirements.toolchains.go]
required = true

[requirements.executables.git]
required = true

[requirements.executables.docker]
required = false

[requirements.environment]
required_presence = ["DATABASE_URL"]
optional_presence = ["AWS_PROFILE"]

[commands.test_package]
argv = ["go", "test", "{package}", "-run", "{test_name}"]
cwd = "."

[commands.test_package.params.package]
kind = "repo_package"
provider = "go"
required = true

[commands.test_package.params.test_name]
kind = "string"
required = false
default = "."
`)
	parsed, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Manifest.SchemaVersion != ManifestSchemaV2 {
		t.Fatalf("schema=%d", parsed.Manifest.SchemaVersion)
	}
	if got := parsed.Manifest.Requirements.Toolchains["go"]; !got.Required {
		t.Fatalf("toolchain requirement=%#v", got)
	}
	if got := parsed.Manifest.Requirements.Executables["docker"]; got.Required {
		t.Fatalf("optional executable became required: %#v", got)
	}
	if got := parsed.Manifest.Requirements.Environment.RequiredPresence; len(got) != 1 || got[0] != "DATABASE_URL" {
		t.Fatalf("required env=%v", got)
	}
	command := parsed.Manifest.Commands["test_package"]
	if len(command.Params) != 2 || command.Params["package"].Kind != ParameterRepoPackage || command.Params["package"].Provider != "go" {
		t.Fatalf("params=%#v", command.Params)
	}
	if got := command.Params["test_name"]; got.Required || got.Default != "." || got.Kind != ParameterString {
		t.Fatalf("default param=%#v", got)
	}
}

func TestManifestV2NormalizesParameterDefaults(t *testing.T) {
	parsed, err := Parse([]byte(`schema_version = 2
[commands.inspect]
argv = ["tool", "{path}", "{count}"]
[commands.inspect.params.path]
kind = "repo_path"
[commands.inspect.params.count]
kind = "integer"
default = "3"
min = 1
max = 10
`))
	if err != nil {
		t.Fatal(err)
	}
	path := parsed.Manifest.Commands["inspect"].Params["path"]
	if !path.Required || path.Exists != PathExistsAny {
		t.Fatalf("path defaults=%#v", path)
	}
	count := parsed.Manifest.Commands["inspect"].Params["count"]
	if count.Required || count.Default != "3" || count.Min == nil || *count.Min != 1 || count.Max == nil || *count.Max != 10 {
		t.Fatalf("integer defaults=%#v", count)
	}
}

func TestManifestV1RejectsV2OnlyFields(t *testing.T) {
	for name, data := range map[string]string{
		"requirements": "schema_version=1\n[requirements.executables.git]\nrequired=true\n",
		"params":       "schema_version=1\n[commands.test]\nargv=[\"go\",\"test\",\"{package}\"]\n[commands.test.params.package]\nkind=\"string\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(data))
			if !HasCode(err, CodeSchemaError) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestManifestV2RejectsUnsafeParameterizedShapes(t *testing.T) {
	cases := map[string]string{
		"parameterized shell": `schema_version=2
[commands.test]
shell="go test {package}"
[commands.test.params.package]
kind="string"
`,
		"partial placeholder": `schema_version=2
[commands.test]
argv=["go","test","./{package}/..."]
[commands.test.params.package]
kind="string"
`,
		"undefined placeholder": `schema_version=2
[commands.test]
argv=["go","test","{package}"]
`,
		"unused parameter": `schema_version=2
[commands.test]
argv=["go","test","./..."]
[commands.test.params.package]
kind="string"
`,
		"duplicate placeholder": `schema_version=2
[commands.test]
argv=["tool","{name}","{name}"]
[commands.test.params.name]
kind="string"
`,
		"unknown parameter field": `schema_version=2
[commands.test]
argv=["tool","{name}"]
[commands.test.params.name]
kind="string"
mystery=true
`,
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(data))
			if !HasCode(err, CodeSchemaError) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestManifestV2RejectsUnsupportedParameterDefinitions(t *testing.T) {
	cases := map[string]string{
		"bad kind": `schema_version=2
[commands.test]
argv=["tool","{x}"]
[commands.test.params.x]
kind="boolean"
`,
		"repo package no provider": `schema_version=2
[commands.test]
argv=["tool","{x}"]
[commands.test.params.x]
kind="repo_package"
`,
		"enum no values": `schema_version=2
[commands.test]
argv=["tool","{x}"]
[commands.test.params.x]
kind="enum"
`,
		"string with integer bounds": `schema_version=2
[commands.test]
argv=["tool","{x}"]
[commands.test.params.x]
kind="string"
min=1
`,
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(data))
			if !HasCode(err, CodeSchemaError) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestManifestV2RejectsDuplicateEnvironmentRequirements(t *testing.T) {
	_, err := Parse([]byte(`schema_version=2
[requirements.environment]
optional_presence=["AWS_PROFILE","AWS_PROFILE"]
`))
	if !HasCode(err, CodeSchemaError) {
		t.Fatalf("err=%v", err)
	}
}

func TestManifestV1CanonicalJSONOmitsV2OnlyFields(t *testing.T) {
	parsed, err := Parse([]byte(`schema_version=1
[commands.test]
argv=["true"]
`))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(parsed.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if strings.Contains(text, `"requirements"`) || strings.Contains(text, `"params"`) {
		t.Fatalf("v1 canonical JSON gained v2 fields: %s", text)
	}
}
