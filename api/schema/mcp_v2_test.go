package schema

import (
	"encoding/json"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

func TestMCPV2SchemasValidateRealPayloads(t *testing.T) {
	input := resolvedSchema(t, MCPInputV2)
	validInputs := []map[string]any{
		{"action": "start", "operation_id": "op", "command": "true", "cwd": "/tmp"},
		{"action": "start", "operation_id": "op-argv", "argv": []any{"git", "status"}, "cwd": "/tmp", "intent": map[string]any{"kind": "inspect", "mutates_source": false, "external_effect": nil}},
		{"action": "start", "operation_id": "op-ws", "command": "true", "workspace_id": "ws_01K00000000000000000000000", "cwd": "src", "workspace_hint": map[string]any{"branch": "main"}},
		{"action": "start", "operation_id": "op-ws-default", "command": "true", "workspace_id": "ws_01K00000000000000000000000"},
		{"action": "poll", "session_id": "s", "cursor": 0.0},
		{"action": "write", "session_id": "s", "input_offset": 0.0, "chars": "x"},
		{"action": "kill", "session_id": "s", "kill_id": "k", "signal": "TERM"},
		{"action": "inspect.server"},
	}
	for _, payload := range validInputs {
		if err := input.Validate(payload); err != nil {
			t.Errorf("valid input rejected %v: %v", payload, err)
		}
	}
	invalidInputs := []map[string]any{
		{"action": "inspect.server", "extra": true},
		{"action": "start", "operation_id": "op", "command": "true", "cwd": "/tmp", "session_id": "s"},
		{"action": "start", "operation_id": "op", "command": "true", "workspace_id": "ws_01K00000000000000000000000", "cwd": "/tmp"},
		{"action": "start", "operation_id": "op", "command": "true", "cwd": "relative"},
		{"action": "start", "operation_id": "op", "command": "true", "argv": []any{"true"}, "cwd": "/tmp"},
		{"action": "start", "operation_id": "op", "argv": []any{}, "cwd": "/tmp"},
		{"action": "start", "operation_id": "op", "argv": []any{""}, "cwd": "/tmp"},
		{"action": "start", "operation_id": "op", "argv": []any{"git"}, "cwd": "/tmp", "intent": map[string]any{"kind": "inspect", "extra": true}},
	}
	for _, payload := range invalidInputs {
		if err := input.Validate(payload); err == nil {
			t.Errorf("invalid input accepted %v", payload)
		}
	}

	output := resolvedSchema(t, MCPOutputV2)
	validOutputs := []map[string]any{
		{"schema_version": 2.0, "ok": true, "action": "start", "result": map[string]any{"schema_version": 2.0, "operation": map[string]any{"operation_id": "op", "workspace_id": "ws_01K00000000000000000000000", "session_id": "s", "state": "running"}, "child": map[string]any{"state": "running", "timed_out": false}, "output": map[string]any{"canonical_stream": "combined", "raw_bytes": 0.0, "returned_bytes": 0.0, "cursor": 0.0, "next_cursor": 0.0, "truncated": false, "output_complete": false}}},
		{"schema_version": 2.0, "ok": true, "action": "inspect.server", "server": map[string]any{"shellbeam_protocol_version": 2.0, "receipt_schema_versions": []any{1.0, 2.0}, "project_manifest_schema_versions": []any{}, "features": map[string]any{"argv_mode": "unavailable"}, "limits": map[string]any{"command_bytes": 1.0, "response_bytes": 2.0, "session_output_bytes": 3.0, "runtime_ms": 4.0, "live_sessions": 5.0, "activity_history": 0.0}}},
		{"schema_version": 2.0, "ok": true, "action": "write", "view": map[string]any{"session_id": "s", "state": "running", "outcome": "", "cursor": 0.0, "next_cursor": 0.0, "truncated": false, "accepted_input_bytes": 1.0, "next_input_offset": 1.0, "eof_queued": false, "kill_id": "", "signal": ""}},
		{"schema_version": 2.0, "ok": false, "action": "inspect.workspace", "error": map[string]any{"code": "feature_unavailable", "message": "feature unavailable", "retryable": false}},
	}
	for _, payload := range validOutputs {
		if err := output.Validate(payload); err != nil {
			t.Errorf("valid output rejected %v: %v", payload, err)
		}
	}
	invalidOutputs := []map[string]any{
		{"schema_version": 2.0, "ok": false, "action": "not-a-shellbeam-action", "error": map[string]any{"code": "feature_unavailable", "message": "feature unavailable", "retryable": false}},
	}
	for _, payload := range invalidOutputs {
		if err := output.Validate(payload); err == nil {
			t.Errorf("invalid output accepted %v", payload)
		}
	}
}

func resolvedSchema(t *testing.T, name Name) *jsonschema.Resolved {
	t.Helper()
	data, err := Load(name)
	if err != nil {
		t.Fatal(err)
	}
	var doc jsonschema.Schema
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	resolved, err := doc.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func TestLegacyMCPInspectServerSchemaIsClosed(t *testing.T) {
	input := resolvedSchema(t, MCPInputV1)
	if err := input.Validate(map[string]any{"action": "inspect.server"}); err != nil {
		t.Fatalf("inspect.server rejected: %v", err)
	}
	if err := input.Validate(map[string]any{"action": "inspect.server", "extra": true}); err == nil {
		t.Fatal("inspect.server accepted extra property")
	}
	output := resolvedSchema(t, MCPOutputV1)
	payload := map[string]any{
		"schema_version": 1.0, "ok": true, "action": "inspect.server",
		"server": map[string]any{
			"shellbeam_protocol_version":       2.0,
			"receipt_schema_versions":          []any{1.0, 2.0},
			"project_manifest_schema_versions": []any{},
			"features":                         map[string]any{"argv_mode": "unavailable"},
			"limits":                           map[string]any{"command_bytes": 1.0, "response_bytes": 2.0, "session_output_bytes": 3.0, "runtime_ms": 4.0, "live_sessions": 5.0, "activity_history": 0.0},
		},
	}
	if err := output.Validate(payload); err != nil {
		t.Fatalf("legacy inspect output rejected: %v", err)
	}
}

func TestMCPV1SchemasValidateEveryBranch(t *testing.T) {
	input := resolvedSchema(t, MCPInputV1)
	inputs := []map[string]any{
		{"action": "start", "operation_id": "op", "command": "true", "cwd": "/tmp"},
		{"action": "poll", "session_id": "s", "cursor": 0.0},
		{"action": "write", "session_id": "s", "input_offset": 0.0, "chars": "x"},
		{"action": "kill", "session_id": "s", "kill_id": "k", "signal": "TERM"},
		{"action": "inspect.server"},
	}
	for _, payload := range inputs {
		if err := input.Validate(payload); err != nil {
			t.Errorf("v1 input rejected %v: %v", payload, err)
		}
	}

	output := resolvedSchema(t, MCPOutputV1)
	for _, action := range []string{"start", "poll", "write", "kill"} {
		payload := map[string]any{
			"schema_version": 1.0, "ok": true, "action": action,
			"session_id": "s", "state": "running", "outcome": "", "output": "",
			"cursor": 0.0, "next_cursor": 0.0, "truncated": false,
			"accepted_input_bytes": 0.0, "next_input_offset": 0.0,
			"eof_queued": false, "kill_id": "", "signal": "", "receipt": nil,
		}
		if err := output.Validate(payload); err != nil {
			t.Errorf("v1 output rejected %s: %v", action, err)
		}
	}
}
