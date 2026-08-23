package schema

import "testing"

func TestDelegatedInteractiveModernSchemasAcceptClosedH1SurfaceAndLegacyRejectsIt(t *testing.T) {
	mcp := resolvedSchema(t, MCPInputV2)
	validMCP := []map[string]any{
		{"action": "start", "operation_id": "delegated-op", "command": "cat", "cwd": "/tmp", "session_mode": "delegated_interactive"},
		{"action": "start", "operation_id": "delegated-stream", "command": "cat", "cwd": "/tmp", "session_mode": "delegated_interactive", "stdin_mode": "stream", "timeout_mode": "unlimited"},
		{"action": "start", "operation_id": "delegated-typed", "workspace_id": "ws_01K00000000000000000000000", "project_command_id": "test", "session_mode": "delegated_interactive", "session_name": "typed-shell", "timeout_mode": "unlimited"},
		{"action": "write", "session_id": "delegated-session", "authority_epoch": 1.0, "input_offset": 0.0, "chars": "x"},
		{"action": "kill", "session_id": "delegated-session", "authority_epoch": 1.0, "kill_id": "kill-1", "signal": "TERM"},
		// Legacy session syntax keeps authority_epoch optional because the schema
		// cannot know the target session mode. Runtime requires it for delegated.
		{"action": "write", "session_id": "legacy-session", "input_offset": 0.0, "chars": "x"},
		{"action": "kill", "session_id": "legacy-session", "kill_id": "kill-legacy", "signal": "TERM"},
	}
	for _, payload := range validMCP {
		if err := mcp.Validate(payload); err != nil {
			t.Errorf("valid delegated MCP rejected %v: %v", payload, err)
		}
	}
	invalidMCP := []map[string]any{
		{"action": "start", "operation_id": "bad-tty", "command": "cat", "cwd": "/tmp", "session_mode": "delegated_interactive", "tty": true},
		{"action": "start", "operation_id": "bad-persistent", "command": "cat", "cwd": "/tmp", "session_mode": "delegated_interactive", "persistent": true},
		{"action": "start", "operation_id": "bad-mode", "command": "cat", "cwd": "/tmp", "session_mode": "future_mode"},
		{"action": "start", "operation_id": "bad-evidence", "command": "cat", "cwd": "/tmp", "session_mode": "delegated_interactive", "evidence": map[string]any{"verification_kind": "test"}},
		{"action": "start", "operation_id": "bad-closed", "command": "cat", "cwd": "/tmp", "session_mode": "delegated_interactive", "stdin_mode": "closed"},
		{"action": "start", "operation_id": "bad-timeout", "command": "cat", "cwd": "/tmp", "session_mode": "delegated_interactive", "timeout_mode": "finite", "timeout_ms": 1000.0},
		{"action": "write", "session_id": "s", "authority_epoch": 0.0, "input_offset": 0.0, "chars": "x"},
		{"action": "kill", "session_id": "s", "authority_epoch": -1.0, "kill_id": "k"},
	}
	for _, payload := range invalidMCP {
		if err := mcp.Validate(payload); err == nil {
			t.Errorf("invalid delegated MCP accepted %v", payload)
		}
	}

	ipc := resolvedSchema(t, IPCV2)
	validIPC := []map[string]any{
		{"ipc_version": 2.0, "kind": "request", "request_id": "start", "action": "start", "operation_id": "delegated-ipc", "command": "cat", "cwd": "/tmp", "session_mode": "delegated_interactive"},
		{"ipc_version": 2.0, "kind": "request", "request_id": "write", "action": "write", "session_id": "delegated-session", "authority_epoch": 1.0, "input_offset": 0.0, "chars": "x"},
		{"ipc_version": 2.0, "kind": "request", "request_id": "kill", "action": "kill", "session_id": "delegated-session", "authority_epoch": 1.0, "kill_id": "kill-1", "signal": "TERM"},
	}
	for _, payload := range validIPC {
		if err := ipc.Validate(payload); err != nil {
			t.Errorf("valid delegated IPC rejected %v: %v", payload, err)
		}
	}

	legacy := resolvedSchema(t, MCPInputV1)
	for _, payload := range []map[string]any{
		{"action": "start", "operation_id": "legacy", "command": "cat", "cwd": "/tmp", "session_mode": "delegated_interactive"},
		{"action": "write", "session_id": "legacy", "input_offset": 0.0, "chars": "x", "authority_epoch": 1.0},
		{"action": "kill", "session_id": "legacy", "kill_id": "k", "authority_epoch": 1.0},
	} {
		if err := legacy.Validate(payload); err == nil {
			t.Errorf("legacy schema accepted delegated field %v", payload)
		}
	}
}

func TestDelegatedV5ResultSchemasAcceptAuthorityAndCaptureTruth(t *testing.T) {
	receipt := delegatedReceiptV5SchemaPayload()
	result := delegatedResultV5SchemaPayload(receipt)
	mcp := map[string]any{"schema_version": 2.0, "ok": true, "action": "start", "result": result}
	if err := resolvedSchema(t, MCPOutputV2).Validate(mcp); err != nil {
		t.Fatalf("MCP delegated v5 rejected: %v", err)
	}
	ipc := map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "v5", "action": "start", "ok": true, "result": result}
	if err := resolvedSchema(t, IPCV2).Validate(ipc); err != nil {
		t.Fatalf("IPC delegated v5 rejected: %v", err)
	}
}

func TestDelegatedV5OutputSchemaRejectsImpossibleCaptureTruth(t *testing.T) {
	cases := []func(map[string]any){
		func(r map[string]any) { r["output"].(map[string]any)["capture_reasons"] = []any{"provider_lost"} },
		func(r map[string]any) {
			out := r["output"].(map[string]any)
			out["capture_quality"] = "partial"
			out["capture_reasons"] = []any{"transport_gap"}
			out["output_complete"] = false
		},
		func(r map[string]any) {
			out := r["output"].(map[string]any)
			out["capture_quality"] = "incomplete"
			out["capture_reasons"] = []any{}
			out["output_complete"] = false
		},
	}
	for i, mutate := range cases {
		receipt := delegatedReceiptV5SchemaPayload()
		result := delegatedResultV5SchemaPayload(receipt)
		mutate(result)
		if err := resolvedSchema(t, MCPOutputV2).Validate(map[string]any{"schema_version": 2.0, "ok": true, "action": "start", "result": result}); err == nil {
			t.Errorf("invalid capture case %d accepted", i)
		}
	}
}

func delegatedReceiptV5SchemaPayload() map[string]any {
	return map[string]any{
		"schema_version": 5.0, "operation_id": "delegated-v5-op", "session_id": "delegated-v5-session",
		"request_fingerprint": "request", "execution_fingerprint": "execution", "daemon_incarnation": "daemon-v5",
		"execution_mode": "shell", "executable": "/bin/sh", "state": "completed", "outcome": "success", "shell": "/bin/sh", "cwd": "/tmp",
		"tty": false, "timeout_ms": 0.0, "output_bytes": 4.0, "output_complete": true,
		"input_accepted_bytes": 0.0, "input_delivered_bytes": 0.0, "stdin_closed": false,
		"session_mode": "delegated_interactive", "authority_epoch": 1.0, "evidence_authority": "session_lifecycle_only", "input_authority_provenance": "agent_only",
		"capture_quality": "complete", "capture_reasons": []any{},
		"spawn_evidence": map[string]any{"attempted": true, "succeeded": true}, "exit_evidence": map[string]any{"reaped": false, "code": 0.0}, "signal_evidence": map[string]any{"attempted": false, "succeeded": false},
	}
}

func delegatedResultV5SchemaPayload(receipt map[string]any) map[string]any {
	return map[string]any{
		"schema_version": 2.0, "session_mode": "delegated_interactive", "authority_epoch": 1.0, "evidence_authority": "session_lifecycle_only", "input_authority_provenance": "agent_only",
		"operation": map[string]any{"operation_id": "delegated-v5-op", "session_id": "delegated-v5-session", "state": "terminal"},
		"child":     map[string]any{"state": "exited", "outcome": "success", "exit_code": 0.0, "timed_out": false},
		"output":    map[string]any{"canonical_stream": "combined", "raw_bytes": 4.0, "returned_bytes": 4.0, "cursor": 0.0, "next_cursor": 4.0, "truncated": false, "output_complete": true, "capture_quality": "complete", "capture_reasons": []any{}},
		"receipt":   receipt,
	}
}

func TestDelegatedInteractiveCapabilitySchemaProjectsTruthfulTask7Support(t *testing.T) {
	server := delegatedCapabilityServerPayload()
	server["receipt_schema_versions"] = append(server["receipt_schema_versions"].([]any), 5.0)
	server["features"].(map[string]any)["delegated_interactive"] = "available"
	server["delegated_interactive"] = map[string]any{
		"provider_id": "tmux_control_mode", "provider_version": 1.0, "platform": "darwin",
		"max_mutation_records": 4096.0, "daemon_restart_continuity": false, "host_reboot_continuity": false,
	}
	for _, tc := range []struct {
		schema  Name
		payload map[string]any
	}{
		{MCPOutputV2, map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.server", "server": server}},
		{IPCV2, map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "delegated-cap", "action": "inspect.server", "ok": true, "server": server}},
	} {
		if err := resolvedSchema(t, tc.schema).Validate(tc.payload); err != nil {
			t.Fatalf("delegated capability rejected: %v", err)
		}
	}
	bad := delegatedCapabilityServerPayload()
	bad["features"].(map[string]any)["delegated_interactive"] = "available"
	bad["delegated_interactive"] = map[string]any{"provider_id": "tmux_control_mode", "provider_version": 1.0, "platform": "darwin", "max_mutation_records": 0.0, "daemon_restart_continuity": false, "host_reboot_continuity": false}
	if err := resolvedSchema(t, MCPOutputV2).Validate(map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.server", "server": bad}); err == nil {
		t.Fatal("invalid delegated capability bound accepted")
	}
}

func delegatedCapabilityServerPayload() map[string]any {
	return map[string]any{
		"shellbeam_protocol_version":       2.0,
		"receipt_schema_versions":          []any{1.0, 2.0, 3.0, 4.0},
		"project_manifest_schema_versions": []any{},
		"features":                         map[string]any{"argv_mode": "unavailable"},
		"limits": map[string]any{
			"command_bytes": 1.0, "response_bytes": 2.0, "session_output_bytes": 3.0,
			"runtime_ms": 4.0, "live_sessions": 1.0, "activity_history": 0.0,
		},
	}
}
