package schema

import "testing"

func validHermeticSchemaValue() map[string]any {
	return map[string]any{
		"version":     1.0,
		"mode":        "required",
		"repo_inputs": []any{"go.mod", "internal/**"},
		"network":     "off",
		"environment": "fixed_allowlist",
		"stdin":       "closed",
		"writes":      "ephemeral_discard",
	}
}

func TestHermeticStartSchemasAcceptOnlyClosedV1Contract(t *testing.T) {
	mcp := map[string]any{"action": "start", "operation_id": "hermetic-schema", "command": "true", "cwd": "/tmp", "hermetic": validHermeticSchemaValue()}
	if err := resolvedSchema(t, MCPInputV2).Validate(mcp); err != nil {
		t.Fatalf("MCP v2 rejected hermetic contract: %v", err)
	}
	ipc := map[string]any{"ipc_version": 2.0, "kind": "request", "request_id": "hermetic-schema", "action": "start", "operation_id": "hermetic-schema", "command": "true", "cwd": "/tmp", "hermetic": validHermeticSchemaValue()}
	if err := resolvedSchema(t, IPCV2).Validate(ipc); err != nil {
		t.Fatalf("IPC v2 rejected hermetic contract: %v", err)
	}
	legacy := map[string]any{"action": "start", "operation_id": "hermetic-schema", "command": "true", "cwd": "/tmp", "hermetic": validHermeticSchemaValue()}
	if err := resolvedSchema(t, MCPInputV1).Validate(legacy); err == nil {
		t.Fatal("MCP v1 accepted hermetic contract")
	}
	invalid := validHermeticSchemaValue()
	invalid["network"] = "allow"
	mcp["hermetic"] = invalid
	if err := resolvedSchema(t, MCPInputV2).Validate(mcp); err == nil {
		t.Fatal("MCP v2 accepted invalid hermetic network mode")
	}
	unknown := validHermeticSchemaValue()
	unknown["provider_path"] = "/private/provider"
	ipc["hermetic"] = unknown
	if err := resolvedSchema(t, IPCV2).Validate(ipc); err == nil {
		t.Fatal("IPC v2 accepted undeclared hermetic implementation detail")
	}
}

func TestHermeticTypedStartSchemasAcceptContract(t *testing.T) {
	mcp := map[string]any{
		"action": "start", "operation_id": "hermetic-typed", "workspace_id": "ws_01K00000000000000000000000",
		"project_command_id": "test_package", "hermetic": validHermeticSchemaValue(),
	}
	if err := resolvedSchema(t, MCPInputV2).Validate(mcp); err != nil {
		t.Fatalf("MCP typed start rejected hermetic contract: %v", err)
	}
	ipc := map[string]any{
		"ipc_version": 2.0, "kind": "request", "request_id": "hermetic-typed", "action": "start", "operation_id": "hermetic-typed",
		"workspace_id": "ws_01K00000000000000000000000", "project_command_id": "test_package", "hermetic": validHermeticSchemaValue(),
	}
	if err := resolvedSchema(t, IPCV2).Validate(ipc); err != nil {
		t.Fatalf("IPC typed start rejected hermetic contract: %v", err)
	}
}

func TestHermeticServerSchemasExposeClosedCapability(t *testing.T) {
	support := map[string]any{
		"version": 1.0, "maturity": "experimental", "provider": "bubblewrap", "provider_version": "0.11.2",
		"scope": "verification_only_ephemeral", "filesystem": "immutable_capture", "network": "off", "environment": "fixed_allowlist",
		"stdin": "closed", "writes": "ephemeral_discard", "time_randomness": "ambient_nondeterministic", "child_tree": "enclosed",
		"placement": "pre_exec", "pty": "unsupported", "persistent_sessions": "unsupported", "authority": "proven_input_scope",
	}
	baseServer := func() map[string]any {
		return map[string]any{
			"shellbeam_protocol_version": 2.0, "receipt_schema_versions": []any{1.0, 2.0}, "project_manifest_schema_versions": []any{},
			"hermetic_boundary": support, "features": map[string]any{"hermetic_boundary_v1": "available"},
			"limits": map[string]any{"command_bytes": 1.0, "response_bytes": 2.0, "session_output_bytes": 3.0, "runtime_ms": 4.0, "live_sessions": 1.0, "activity_history": 0.0},
		}
	}
	mcp := map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.server", "server": baseServer()}
	if err := resolvedSchema(t, MCPOutputV2).Validate(mcp); err != nil {
		t.Fatalf("MCP output rejected hermetic support: %v", err)
	}
	ipc := map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "hermetic-server", "action": "inspect.server", "ok": true, "server": baseServer()}
	if err := resolvedSchema(t, IPCV2).Validate(ipc); err != nil {
		t.Fatalf("IPC output rejected hermetic support: %v", err)
	}
	leaky := baseServer()
	leaky["hermetic_boundary"].(map[string]any)["provider_path"] = "/private/provider"
	bad := map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.server", "server": leaky}
	if err := resolvedSchema(t, MCPOutputV2).Validate(bad); err == nil {
		t.Fatal("MCP output accepted hermetic provider path leak")
	}
}

func TestHermeticStartSchemasRejectInteractivePersistentOrStreamingV1(t *testing.T) {
	cases := []map[string]any{
		{"tty": true},
		{"persistent": true, "session_name": "h"},
		{"stdin_mode": "stream"},
	}
	for _, extra := range cases {
		mcp := map[string]any{"action": "start", "operation_id": "hermetic-closed", "command": "true", "cwd": "/tmp", "hermetic": validHermeticSchemaValue()}
		ipc := map[string]any{"ipc_version": 2.0, "kind": "request", "request_id": "hermetic-closed", "action": "start", "operation_id": "hermetic-closed", "command": "true", "cwd": "/tmp", "hermetic": validHermeticSchemaValue()}
		for key, value := range extra {
			mcp[key] = value
			ipc[key] = value
		}
		if err := resolvedSchema(t, MCPInputV2).Validate(mcp); err == nil {
			t.Fatalf("MCP schema accepted contradictory hermetic request: %#v", extra)
		}
		if err := resolvedSchema(t, IPCV2).Validate(ipc); err == nil {
			t.Fatalf("IPC schema accepted contradictory hermetic request: %#v", extra)
		}
	}
}
