package schema

import (
	"strings"
	"testing"
)

func TestResourceEnforcementStartSchemasAreClosedAndBounded(t *testing.T) {
	validLimits := []map[string]any{
		{"memory_bytes": float64(64 << 20)},
		{"processes": 8.0},
		{"cpu_time_ms": 1000.0},
		{"memory_bytes": float64(64 << 20), "processes": 8.0},
	}
	for _, limits := range validLimits {
		mcp := map[string]any{"action": "start", "operation_id": "resource-schema", "command": "true", "cwd": "/tmp", "limits": limits}
		if err := resolvedSchema(t, MCPInputV2).Validate(mcp); err != nil {
			t.Fatalf("MCP v2 rejected limits %#v: %v", limits, err)
		}
		ipc := map[string]any{"ipc_version": 2.0, "kind": "request", "request_id": "resource-schema", "action": "start", "operation_id": "resource-schema", "command": "true", "cwd": "/tmp", "limits": limits}
		if err := resolvedSchema(t, IPCV2).Validate(ipc); err != nil {
			t.Fatalf("IPC v2 rejected limits %#v: %v", limits, err)
		}
	}
	invalidLimits := []map[string]any{
		{},
		{"memory_bytes": 0.0},
		{"processes": 0.0},
		{"cpu_time_ms": 0.0},
		{"memory_bytes": -1.0},
		{"unknown": 1.0},
	}
	for _, limits := range invalidLimits {
		mcp := map[string]any{"action": "start", "operation_id": "resource-schema", "command": "true", "cwd": "/tmp", "limits": limits}
		if err := resolvedSchema(t, MCPInputV2).Validate(mcp); err == nil {
			t.Fatalf("MCP v2 accepted invalid limits %#v", limits)
		}
		ipc := map[string]any{"ipc_version": 2.0, "kind": "request", "request_id": "resource-schema", "action": "start", "operation_id": "resource-schema", "command": "true", "cwd": "/tmp", "limits": limits}
		if err := resolvedSchema(t, IPCV2).Validate(ipc); err == nil {
			t.Fatalf("IPC v2 accepted invalid limits %#v", limits)
		}
	}
	legacy := map[string]any{"action": "start", "operation_id": "resource-schema", "command": "true", "cwd": "/tmp", "limits": map[string]any{"memory_bytes": float64(64 << 20)}}
	if err := resolvedSchema(t, MCPInputV1).Validate(legacy); err == nil {
		t.Fatal("MCP v1 accepted resource limits")
	}
}

func TestResourceEnforcementTypedStartSchemasAcceptLimits(t *testing.T) {
	mcp := map[string]any{
		"action": "start", "operation_id": "resource-typed", "workspace_id": "ws_01K00000000000000000000000",
		"project_command_id": "test_package", "limits": map[string]any{"processes": 16.0},
	}
	if err := resolvedSchema(t, MCPInputV2).Validate(mcp); err != nil {
		t.Fatalf("MCP typed start rejected limits: %v", err)
	}
	ipc := map[string]any{
		"ipc_version": 2.0, "kind": "request", "request_id": "resource-typed", "action": "start", "operation_id": "resource-typed",
		"workspace_id": "ws_01K00000000000000000000000", "project_command_id": "test_package", "limits": map[string]any{"processes": 16.0},
	}
	if err := resolvedSchema(t, IPCV2).Validate(ipc); err != nil {
		t.Fatalf("IPC typed start rejected limits: %v", err)
	}
}

func TestResourceEnforcementServerSchemasExposeSeparateHardCapability(t *testing.T) {
	support := map[string]any{
		"version":             1.0,
		"maturity":            "experimental",
		"provider":            "linux_cgroup_v2",
		"scope":               "owned_process_tree",
		"placement":           "pre_exec_atomic",
		"memory_bytes":        "hard",
		"processes":           "hard",
		"cpu_time_ms":         "unsupported",
		"persistent_sessions": "unsupported",
	}
	baseServer := func() map[string]any {
		return map[string]any{
			"shellbeam_protocol_version":       2.0,
			"receipt_schema_versions":          []any{1.0, 2.0},
			"project_manifest_schema_versions": []any{},
			"resource_enforcement":             support,
			"features":                         map[string]any{"resource_enforcement": "available"},
			"limits":                           map[string]any{"command_bytes": 1.0, "response_bytes": 2.0, "session_output_bytes": 3.0, "runtime_ms": 4.0, "live_sessions": 1.0, "activity_history": 0.0},
		}
	}
	mcp := map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.server", "server": baseServer()}
	if err := resolvedSchema(t, MCPOutputV2).Validate(mcp); err != nil {
		t.Fatalf("MCP output rejected enforcement support: %v", err)
	}
	ipc := map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "resource-server", "action": "inspect.server", "ok": true, "server": baseServer()}
	if err := resolvedSchema(t, IPCV2).Validate(ipc); err != nil {
		t.Fatalf("IPC output rejected enforcement support: %v", err)
	}

	leaky := baseServer()
	leaky["resource_enforcement"].(map[string]any)["sample_interval_ms"] = 10.0
	bad := map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.server", "server": leaky}
	if err := resolvedSchema(t, MCPOutputV2).Validate(bad); err == nil {
		t.Fatal("MCP output accepted undeclared enforcement implementation detail")
	}
}

func TestResourceLimitsPersistOnlyOnModernOperationSchemas(t *testing.T) {
	limits := map[string]any{"memory_bytes": float64(64 << 20), "processes": 8.0}
	v2 := map[string]any{
		"schema_version": 2.0, "operation_id": "resource-op", "session_id": "resource-session",
		"request_fingerprint": "req", "execution_fingerprint": "exec", "command": "true", "cwd": "/tmp", "tty": false,
		"timeout_ms": 0.0, "shell": "/bin/sh", "daemon_incarnation": "d", "resource_limits": limits,
	}
	if err := resolvedSchema(t, OperationV2).Validate(v2); err != nil {
		t.Fatalf("operation v2 rejected resource limits: %v", err)
	}
	v3 := map[string]any{
		"schema_version": 3.0, "operation_id": "resource-typed", "session_id": "resource-session",
		"request_fingerprint": "req", "execution_fingerprint": "exec", "execution_mode": "argv", "executable": "/bin/true",
		"argv": []any{"/bin/true"}, "workspace_id": "ws_01K00000000000000000000000", "logical_cwd": ".", "cwd": "/tmp",
		"tty": false, "timeout_ms": 0.0, "daemon_incarnation": "d", "control_reservation_bytes": 0.0, "created_at": "2026-08-18T00:00:00Z", "resource_limits": limits,
		"project_command": map[string]any{
			"schema_version": 1.0, "manifest_digest": strings.Repeat("a", 64), "manifest_schema_version": 2.0, "command_id": "test",
			"parameter_fingerprint": strings.Repeat("b", 64), "parameters": []any{}, "resolved_argv": []any{"/bin/true"},
			"logical_cwd": ".", "resolved_cwd": "/tmp",
		},
	}
	if err := resolvedSchema(t, OperationV3).Validate(v3); err != nil {
		t.Fatalf("operation v3 rejected resource limits: %v", err)
	}
}
