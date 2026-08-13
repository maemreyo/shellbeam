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
	}
	for _, payload := range invalid {
		if err := rs.Validate(payload); err == nil {
			t.Errorf("expected invalid %v", payload)
		}
	}
}
