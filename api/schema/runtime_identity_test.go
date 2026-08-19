package schema

import "testing"

func TestRuntimeIdentityIsOptionalAndStrictInServerCatalogs(t *testing.T) {
	runtime := map[string]any{
		"schema_version":     1.0,
		"version":            "v1.2.3",
		"revision":           "86b0cb56cf7a57dd6ab1d0208bf08ffcb3acbbbf",
		"vcs_modified":       false,
		"binary_sha256":      "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"daemon_incarnation": "01M0BTQCZYM47Y3YCPAGDDGKME",
		"daemon_started_at":  "2026-08-19T08:38:42+07:00",
	}
	server := map[string]any{
		"shellbeam_protocol_version":       2.0,
		"receipt_schema_versions":          []any{1.0, 2.0},
		"project_manifest_schema_versions": []any{1.0, 2.0},
		"features":                         map[string]any{"argv_mode": "available"},
		"limits": map[string]any{
			"command_bytes": 1.0, "response_bytes": 2.0, "session_output_bytes": 3.0,
			"runtime_ms": 4.0, "live_sessions": 1.0, "activity_history": 1.0,
		},
	}
	mcp := resolvedSchema(t, MCPOutputV2)
	ipc := resolvedSchema(t, IPCV2)

	// Compatibility: an older server catalog without runtime identity stays valid.
	if err := mcp.Validate(map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.server", "server": server}); err != nil {
		t.Fatalf("legacy-compatible MCP catalog: %v", err)
	}
	if err := ipc.Validate(map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "x", "action": "inspect.server", "ok": true, "server": server}); err != nil {
		t.Fatalf("legacy-compatible IPC catalog: %v", err)
	}

	server["runtime"] = runtime
	if err := mcp.Validate(map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.server", "server": server}); err != nil {
		t.Fatalf("MCP runtime identity rejected: %v", err)
	}
	if err := ipc.Validate(map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "x", "action": "inspect.server", "ok": true, "server": server}); err != nil {
		t.Fatalf("IPC runtime identity rejected: %v", err)
	}

	runtime["executable_path"] = "/Users/alice/private/shellbeam"
	if err := mcp.Validate(map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.server", "server": server}); err == nil {
		t.Fatal("MCP runtime identity accepted private executable path")
	}
	if err := ipc.Validate(map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "x", "action": "inspect.server", "ok": true, "server": server}); err == nil {
		t.Fatal("IPC runtime identity accepted private executable path")
	}
}
