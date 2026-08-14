package schema

import "testing"

func TestStructuredAdapterStartAndPersistenceSchemasAreClosed(t *testing.T) {
	workspaceID := "ws_01K00000000000000000000000"
	valid := []struct {
		name  Name
		value map[string]any
	}{
		{MCPInputV2, map[string]any{"action": "start", "operation_id": "op", "argv": []any{"go", "test", "-json", "./..."}, "cwd": "/tmp", "structured_adapter": "go-test-json"}},
		{IPCV2, map[string]any{"ipc_version": 2.0, "kind": "request", "request_id": "r", "action": "start", "operation_id": "op", "argv": []any{"go", "vet", "-json", "./..."}, "cwd": "/tmp", "structured_adapter": "go-vet-json"}},
		{Name("operation-v2.json"), map[string]any{"schema_version": 2.0, "operation_id": "op", "session_id": "session", "request_fingerprint": "req", "execution_fingerprint": "exec", "observation_binding_fingerprint": "obs", "structured_adapter": "go-test-json", "execution_mode": "argv", "executable": "go", "argv": []any{"go", "test", "-json", "./..."}, "cwd": "/tmp", "tty": false, "timeout_ms": 0.0, "daemon_incarnation": "d", "control_reservation_bytes": 0.0, "created_at": "2026-08-14T00:00:00Z"}},
		{MCPInputV2, map[string]any{"action": "start", "operation_id": "op-ws", "argv": []any{"go", "test", "-json", "./..."}, "workspace_id": workspaceID, "structured_adapter": "go-test-json"}},
		{MCPInputV2, map[string]any{"action": "start", "operation_id": "op-unsupported", "command": "true", "cwd": "/tmp", "structured_adapter": "junit"}},
	}
	for _, tc := range valid {
		if err := resolvedSchema(t, tc.name).Validate(tc.value); err != nil {
			t.Errorf("%s rejected %#v: %v", tc.name, tc.value, err)
		}
	}
	invalid := []struct {
		name  Name
		value map[string]any
	}{
		{MCPInputV2, map[string]any{"action": "start", "operation_id": "op", "argv": []any{"go", "test", "-json"}, "cwd": "/tmp", "structured_adapter": "../bad"}},
		{MCPInputV2, map[string]any{"action": "poll", "session_id": "s", "structured_adapter": "go-test-json"}},
		{IPCV2, map[string]any{"ipc_version": 2.0, "kind": "request", "request_id": "r", "action": "start", "operation_id": "op", "argv": []any{"go", "test", "-json"}, "cwd": "/tmp", "structured_adapter": "../bad"}},
		{Name("operation-v2.json"), map[string]any{"schema_version": 2.0, "operation_id": "op", "session_id": "session", "request_fingerprint": "req", "execution_fingerprint": "exec", "structured_adapter": "../bad", "command": "true", "cwd": "/tmp", "shell": "/bin/sh", "daemon_incarnation": "d"}},
	}
	for _, tc := range invalid {
		if err := resolvedSchema(t, tc.name).Validate(tc.value); err == nil {
			t.Errorf("%s accepted %#v", tc.name, tc.value)
		}
	}
}
