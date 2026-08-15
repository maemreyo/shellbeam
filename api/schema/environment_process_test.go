package schema

import (
	"strings"
	"testing"
)

func TestA25EnvironmentProcessRequestSchemasAreClosed(t *testing.T) {
	workspaceID := "ws_01K00000000000000000000000"
	validMCP := []map[string]any{
		{"action": "inspect.environment", "workspace_id": workspaceID, "freshness": "refresh", "execution": map[string]any{"mode": "shell", "identity": "/bin/zsh"}},
		{"action": "inspect.environment"},
		{"action": "inspect.process", "process_target": map[string]any{"kind": "pid", "pid": 123.0}, "include_ports": true},
		{"action": "inspect.process", "process_target": map[string]any{"kind": "session", "session_id": "session-123"}},
	}
	for _, payload := range validMCP {
		if err := resolvedSchema(t, MCPInputV2).Validate(payload); err != nil {
			t.Errorf("valid MCP A2.5 payload rejected %v: %v", payload, err)
		}
	}
	invalidMCP := []map[string]any{
		{"action": "inspect.environment", "freshness": "future"},
		{"action": "inspect.environment", "raw_path": "/private/bin"},
		{"action": "inspect.environment", "value": "low-entropy-secret"},
		{"action": "inspect.environment", "execution": map[string]any{"mode": "argv"}},
		{"action": "inspect.process", "process_target": map[string]any{"kind": "pid", "pid": 0.0}},
		{"action": "inspect.process", "process_target": map[string]any{"kind": "session", "session_id": "s", "pid": 12.0}},
		{"action": "inspect.process", "process_target": map[string]any{"kind": "name", "name": "node"}},
		{"action": "inspect.process", "target": map[string]any{"kind": "session", "session_id": "s"}},
	}
	for _, payload := range invalidMCP {
		if err := resolvedSchema(t, MCPInputV2).Validate(payload); err == nil {
			t.Errorf("invalid MCP A2.5 payload accepted %v", payload)
		}
	}
	validIPC := []map[string]any{
		{"ipc_version": 2.0, "kind": "request", "request_id": "env", "action": "inspect.environment", "workspace_id": workspaceID, "freshness": "cached"},
		{"ipc_version": 2.0, "kind": "request", "request_id": "proc", "action": "inspect.process", "process_target": map[string]any{"kind": "pid", "pid": 123.0}},
	}
	for _, payload := range validIPC {
		if err := resolvedSchema(t, IPCV2).Validate(payload); err != nil {
			t.Errorf("valid IPC A2.5 payload rejected %v: %v", payload, err)
		}
	}
}

func TestA25EnvironmentProcessResponseSchemasAreClosedAndBounded(t *testing.T) {
	validIPC := []map[string]any{
		{"ipc_version": 2.0, "kind": "response", "request_id": "env", "action": "inspect.environment", "ok": true, "environment": a25EnvironmentPayload()},
		{"ipc_version": 2.0, "kind": "response", "request_id": "proc", "action": "inspect.process", "ok": true, "process": a25ProcessPayload()},
	}
	for _, payload := range validIPC {
		if err := resolvedSchema(t, IPCV2).Validate(payload); err != nil {
			t.Errorf("valid IPC A2.5 payload rejected %v: %v", payload, err)
		}
	}
	validMCPOutput := []map[string]any{
		{"schema_version": 2.0, "ok": true, "action": "inspect.environment", "environment": a25EnvironmentPayload()},
		{"schema_version": 2.0, "ok": true, "action": "inspect.process", "process": a25ProcessPayload()},
	}
	for _, payload := range validMCPOutput {
		if err := resolvedSchema(t, MCPOutputV2).Validate(payload); err != nil {
			t.Errorf("valid MCP A2.5 output rejected %v: %v", payload, err)
		}
	}
	leaky := a25EnvironmentPayload()
	leaky["raw_path"] = "/Users/alice/.secret/bin"
	if err := resolvedSchema(t, MCPOutputV2).Validate(map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.environment", "environment": leaky}); err == nil {
		t.Fatal("environment response accepted raw_path")
	}
	leaky = a25EnvironmentPayload()
	leaky["value"] = "low-entropy-secret"
	if err := resolvedSchema(t, IPCV2).Validate(map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "env", "action": "inspect.environment", "ok": true, "environment": leaky}); err == nil {
		t.Fatal("environment response accepted raw value")
	}
	tooManyDescendants := a25ProcessPayload()
	descendants := make([]any, 129)
	for i := range descendants {
		descendants[i] = map[string]any{"pid": float64(1000 + i), "parent_pid": 123.0, "shellbeam_relation": "external", "state": "running"}
	}
	tooManyDescendants["descendants"] = descendants
	if err := resolvedSchema(t, MCPOutputV2).Validate(map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.process", "process": tooManyDescendants}); err == nil {
		t.Fatal("process response accepted more than 128 descendants")
	}
	tooManyPorts := a25ProcessPayload()
	ports := make([]any, 65)
	for i := range ports {
		ports[i] = map[string]any{"pid": 123.0, "protocol": "tcp", "local_endpoint_class": "loopback", "port": float64(1000 + i), "quality": "complete"}
	}
	tooManyPorts["ports"] = ports
	if err := resolvedSchema(t, IPCV2).Validate(map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "proc", "action": "inspect.process", "ok": true, "process": tooManyPorts}); err == nil {
		t.Fatal("process response accepted more than 64 ports")
	}
}

func a25EnvironmentPayload() map[string]any {
	return map[string]any{
		"schema_version":          1.0,
		"snapshot_id":             "env_" + strings.Repeat("a", 64),
		"captured_at":             "2026-08-15T12:00:00Z",
		"quality":                 "complete",
		"environment_fingerprint": strings.Repeat("b", 64),
		"fingerprint_version":     1.0,
		"platform":                map[string]any{"os": "darwin", "architecture": "arm64"},
		"execution":               map[string]any{"mode": "shell", "identity": "/bin/zsh"},
		"path":                    map[string]any{"digest": strings.Repeat("c", 64), "entry_count": 3.0, "quality": "complete"},
		"variable_presence":       []any{map[string]any{"name": "CI", "present": true}},
	}
}

func a25ProcessPayload() map[string]any {
	return map[string]any{
		"schema_version": 1.0,
		"observed_at":    "2026-08-15T12:00:00Z",
		"quality":        "complete",
		"target":         map[string]any{"kind": "pid", "pid": 123.0},
		"root":           map[string]any{"pid": 123.0, "shellbeam_relation": "external", "state": "running"},
		"truncated":      false,
	}
}
