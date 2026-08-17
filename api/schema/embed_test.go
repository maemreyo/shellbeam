package schema

import (
	"encoding/json"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

func TestLoadRejectsUnknown(t *testing.T) {
	if _, err := Load("other.json"); err == nil {
		t.Fatal("expected error")
	}
}

func TestIPCV2SchemaValidatesRealPayloads(t *testing.T) {
	data, err := Load(IPCV2)
	if err != nil {
		t.Fatal(err)
	}
	var s jsonschema.Schema
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	rs, err := s.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	valid := []map[string]any{
		{"ipc_version": 2, "kind": "request", "request_id": "x", "action": "start", "operation_id": "op", "command": "echo hi", "cwd": "/tmp"},
		{"ipc_version": 2, "kind": "request", "request_id": "x", "action": "poll", "session_id": "s"},
		{"ipc_version": 2, "kind": "request", "request_id": "x", "action": "write", "session_id": "s", "input_offset": 0, "chars": "hi"},
		{"ipc_version": 2, "kind": "request", "request_id": "x", "action": "kill", "session_id": "s", "kill_id": "k", "signal": "TERM"},
		{"ipc_version": 2, "kind": "request", "request_id": "x", "action": "inspect.server"},
		{"ipc_version": 2, "kind": "response", "request_id": "x", "action": "inspect.server", "ok": true, "server": map[string]any{"shellbeam_protocol_version": 2, "receipt_schema_versions": []any{1.0}, "project_manifest_schema_versions": []any{}, "features": map[string]any{"argv_mode": "unavailable"}, "limits": map[string]any{"command_bytes": 1.0, "response_bytes": 2.0, "session_output_bytes": 3.0, "runtime_ms": 4.0, "live_sessions": 1.0, "activity_history": 0.0}}},
		{"ipc_version": 2, "kind": "response", "request_id": "start", "action": "start", "ok": true, "result": map[string]any{"schema_version": 2.0, "operation": map[string]any{"operation_id": "op", "session_id": "s", "state": "running"}, "child": map[string]any{"state": "running", "timed_out": false}, "output": map[string]any{"canonical_stream": "combined", "raw_bytes": 0.0, "returned_bytes": 0.0, "cursor": 0.0, "next_cursor": 0.0, "truncated": false, "output_complete": false}}},
		{"ipc_version": 2, "kind": "response", "request_id": "x", "action": "inspect.workspace", "ok": false, "error": map[string]any{"code": "feature_unavailable", "message": "feature unavailable", "retryable": false, "details": map[string]any{"feature": "inspect.workspace"}}},
	}
	for _, payload := range valid {
		if err := rs.Validate(payload); err != nil {
			t.Errorf("expected valid %v: %v", payload, err)
		}
	}
	invalid := []map[string]any{
		{"ipc_version": 2, "kind": "request", "request_id": "x", "action": "inspect.server", "extra": true},
		{"ipc_version": 2, "kind": "request", "request_id": "x", "action": "start", "command": "echo hi", "cwd": "/tmp"},
		{"ipc_version": 2, "kind": "response", "request_id": "x", "action": "inspect.server", "ok": true, "extra": true},
		{"ipc_version": 2, "kind": "response", "request_id": "x", "action": "start", "ok": true, "result": map[string]any{"schema_version": 2.0, "operation": map[string]any{"operation_id": "op", "session_id": "s", "state": "running", "extra": true}, "child": map[string]any{"state": "running", "timed_out": false}, "output": map[string]any{"canonical_stream": "combined", "raw_bytes": 0.0, "returned_bytes": 0.0, "cursor": 0.0, "next_cursor": 0.0, "truncated": false, "output_complete": false}}},
	}
	for _, payload := range invalid {
		if err := rs.Validate(payload); err == nil {
			t.Errorf("expected invalid %v", payload)
		}
	}
}

func TestReadMediaSchemaFragmentsValidateSafeShapes(t *testing.T) {
	inputData, err := Load(MCPReadMediaInputV1)
	if err != nil {
		t.Fatal(err)
	}
	outputData, err := Load(MCPReadMediaOutputV1)
	if err != nil {
		t.Fatal(err)
	}
	for label, data := range map[string][]byte{"input": inputData, "output": outputData} {
		var s jsonschema.Schema
		if err := json.Unmarshal(data, &s); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		if _, err := s.Resolve(nil); err != nil {
			t.Fatalf("%s resolve: %v", label, err)
		}
	}
	var inSchema jsonschema.Schema
	_ = json.Unmarshal(inputData, &inSchema)
	in, err := inSchema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	valid := []map[string]any{
		{"action": "read_media", "workspace_id": "ws_01K00000000000000000000000", "path": "artifacts/settings.png"},
		{"action": "read_media", "cwd": "/tmp", "path": "settings.png"},
	}
	for _, p := range valid {
		if err := in.Validate(p); err != nil {
			t.Fatalf("valid input %#v: %v", p, err)
		}
	}
	invalid := []map[string]any{
		{"action": "read_media", "path": "settings.png"},
		{"action": "read_media", "workspace_id": "ws_01K00000000000000000000000", "cwd": "/tmp", "path": "settings.png"},
		{"action": "read_media", "cwd": "/tmp", "path": "/settings.png"},
		{"action": "read_media", "cwd": "/tmp", "path": "a//b.png"},
		{"action": "read_media", "cwd": "/tmp", "path": "a/../b.png"},
		{"action": "read_media", "cwd": "/tmp", "path": "settings.png", "extra": true},
	}
	for _, p := range invalid {
		if err := in.Validate(p); err == nil {
			t.Fatalf("invalid input accepted %#v", p)
		}
	}

	var outSchema jsonschema.Schema
	_ = json.Unmarshal(outputData, &outSchema)
	out, err := outSchema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	good := map[string]any{"schema_version": 1.0, "kind": "media", "display_address": map[string]any{"address_kind": "cwd", "cwd": "/tmp", "path": "settings.png"}, "mime_type": "image/png", "format": "png", "byte_size": 123.0, "width": 10.0, "height": 10.0}
	if err := out.Validate(good); err != nil {
		t.Fatalf("valid output: %v", err)
	}
	bad := make(map[string]any, len(good)+1)
	for k, v := range good {
		bad[k] = v
	}
	bad["data"] = "base64-secret"
	if err := out.Validate(bad); err == nil {
		t.Fatal("output schema accepted raw data")
	}
}
