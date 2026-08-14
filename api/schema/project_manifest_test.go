package schema

import (
	"encoding/json"
	"testing"

	coreproject "github.com/maemreyo/shellbeam/internal/core/project"
)

func TestManifestSchemaIsEmbeddedAndClosed(t *testing.T) {
	schema := resolvedSchema(t, ProjectManifestV1)
	valid := map[string]any{
		"schema_version": 1.0,
		"commands": map[string]any{
			"test": map[string]any{"argv": []any{"go", "test", "./..."}, "kind": "test"},
		},
	}
	if err := schema.Validate(valid); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
	invalid := map[string]any{"schema_version": 1.0, "mystery": true}
	if err := schema.Validate(invalid); err == nil {
		t.Fatal("unknown manifest property accepted")
	}
}

func TestManifestInspectWireSchemas(t *testing.T) {
	workspaceID := "ws_01K00000000000000000000000"
	project := map[string]any{
		"status":                "valid",
		"schema_version":        1.0,
		"manifest_digest":       "abc",
		"discovery_fingerprint": "def",
		"confidence":            "high",
		"provenance":            "workspace_manifest",
		"manifest":              map[string]any{"schema_version": 1.0},
	}
	cases := []struct {
		schema  Name
		payload map[string]any
	}{
		{MCPInputV2, map[string]any{"action": "inspect.project", "workspace_id": workspaceID}},
		{MCPOutputV2, map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.project", "project": project}},
		{IPCV2, map[string]any{"ipc_version": 2.0, "kind": "request", "request_id": "p", "action": "inspect.project", "workspace_id": workspaceID}},
		{IPCV2, map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "p", "action": "inspect.project", "ok": true, "project": project}},
	}
	for _, tc := range cases {
		if err := resolvedSchema(t, tc.schema).Validate(tc.payload); err != nil {
			t.Errorf("schema %s rejected inspect.project payload %v: %v", tc.schema, tc.payload, err)
		}
	}
}

func TestManifestInspectWireSchemasAcceptNormalizedCoreManifest(t *testing.T) {
	parsed, err := coreproject.Parse([]byte(`schema_version = 1
[project]
name = "demo"

[toolchains.go]
version_source = "go.mod"

[commands.test]
argv = ["go", "test", "./..."]
kind = "test"
cost = "medium"
source_scope = "full"

[[commands.test.expected_outputs]]
path = ".build/result.txt"
kind = "file"

[verification.profiles.checkpoint]
steps = ["test"]

[environment]
relevant_presence = ["GOFLAGS"]

[[outputs]]
path = ".build"
kind = "directory"
role = "build-cache"
`))
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(parsed.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	project := map[string]any{
		"status":                "valid",
		"schema_version":        1.0,
		"manifest_digest":       coreproject.RawDigest([]byte("fixture")),
		"discovery_fingerprint": parsed.Fingerprint,
		"confidence":            "high",
		"provenance":            "workspace_manifest",
		"manifest":              manifest,
	}
	payloads := []struct {
		schema Name
		value  map[string]any
	}{
		{MCPOutputV2, map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.project", "project": project}},
		{IPCV2, map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "p", "action": "inspect.project", "ok": true, "project": project}},
	}
	for _, payload := range payloads {
		if err := resolvedSchema(t, payload.schema).Validate(payload.value); err != nil {
			t.Errorf("schema %s rejected normalized manifest: %v\npayload=%v", payload.schema, err, payload.value)
		}
	}
}
