package schema

import "testing"

func TestContextExecModernRequestSchemaIsExactAndLegacyClosed(t *testing.T) {
	valid := map[string]any{
		"action":           "context.exec",
		"context_exec_id":  "ctxexec_public_01",
		"session_id":       "session_public_01",
		"authority_epoch":  4.0,
		"argv":             []any{"go", "test", "./..."},
		"timeout_ms":       30000.0,
		"max_output_bytes": 1048576.0,
	}
	if err := resolvedSchema(t, MCPInputV2).Validate(valid); err != nil {
		t.Fatalf("valid MCP context.exec rejected: %v", err)
	}

	ipcPayload := map[string]any{"ipc_version": 2.0, "kind": "request", "request_id": "ctxexec-schema"}
	for k, v := range valid {
		ipcPayload[k] = v
	}
	if err := resolvedSchema(t, IPCV2).Validate(ipcPayload); err != nil {
		t.Fatalf("valid IPC context.exec rejected: %v", err)
	}

	for _, forbidden := range []string{"command", "cwd", "tty", "persistent", "stdin_mode", "session_mode", "env", "environment"} {
		bad := cloneSchemaMap(valid)
		bad[forbidden] = "must-not-be-accepted"
		if err := resolvedSchema(t, MCPInputV2).Validate(bad); err == nil {
			t.Errorf("context.exec accepted forbidden field %q", forbidden)
		}
	}

	invalid := []map[string]any{
		{"action": "context.exec", "context_exec_id": "ctxexec_public_01", "session_id": "session_public_01", "authority_epoch": 4.0, "argv": []any{}, "timeout_ms": 30000.0, "max_output_bytes": 1048576.0},
		{"action": "context.exec", "context_exec_id": "ctxexec_public_01", "session_id": "session_public_01", "authority_epoch": 0.0, "argv": []any{"go"}, "timeout_ms": 30000.0, "max_output_bytes": 1048576.0},
		{"action": "context.exec", "context_exec_id": "ctxexec_public_01", "session_id": "session_public_01", "authority_epoch": 4.0, "argv": []any{"go"}, "timeout_ms": -1.0, "max_output_bytes": 1048576.0},
		{"action": "context.exec", "context_exec_id": "ctxexec_public_01", "session_id": "session_public_01", "authority_epoch": 4.0, "argv": []any{"go"}, "timeout_ms": 30000.0, "max_output_bytes": 0.0},
	}
	for _, payload := range invalid {
		if err := resolvedSchema(t, MCPInputV2).Validate(payload); err == nil {
			t.Errorf("invalid context.exec accepted: %v", payload)
		}
	}

	if err := resolvedSchema(t, MCPInputV1).Validate(valid); err == nil {
		t.Fatal("legacy MCP schema accepted context.exec")
	}
}

func cloneSchemaMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in)+1)
	for k, v := range in {
		out[k] = v
	}
	return out
}

func TestContextExecOutputSchemaExposesOnlyBoundedPublicState(t *testing.T) {
	state := map[string]any{
		"schema_version":  1.0,
		"context_exec_id": "ctxexec_public_01",
		"session_id":      "session_public_01",
		"authority_epoch": 4.0,
		"lifecycle":       "helper_requested",
	}
	mcp := map[string]any{"schema_version": 2.0, "ok": true, "action": "context.exec", "context_exec": state}
	if err := resolvedSchema(t, MCPOutputV2).Validate(mcp); err != nil {
		t.Fatalf("valid MCP context.exec output rejected: %v", err)
	}
	ipc := map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "ctxexec-schema", "action": "context.exec", "ok": true, "context_exec": state}
	if err := resolvedSchema(t, IPCV2).Validate(ipc); err != nil {
		t.Fatalf("valid IPC context.exec output rejected: %v", err)
	}

	for _, private := range []string{"request_fingerprint", "provider_generation", "shell_identity", "cwd_observed", "helper", "opaque_launch_id", "generation", "executable_path", "environment", "env"} {
		badState := cloneSchemaMap(state)
		badState[private] = "must-not-project"
		bad := map[string]any{"schema_version": 2.0, "ok": true, "action": "context.exec", "context_exec": badState}
		if err := resolvedSchema(t, MCPOutputV2).Validate(bad); err == nil {
			t.Errorf("public context.exec output accepted private field %q", private)
		}
	}
}

func TestContextExecCanonicalOutputSchemaCarriesLiteralChildEvidence(t *testing.T) {
	state := map[string]any{
		"schema_version":       1.0,
		"context_exec_id":      "ctxexec_public_01",
		"session_id":           "session_public_01",
		"authority_epoch":      4.0,
		"lifecycle":            "canonicalized",
		"child_operation_id":   "cxop_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"child_session_id":     "cxs_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"requested_executable": "go",
		"resolved_executable":  "/usr/bin/go",
		"spawn_evidence":       map[string]any{"attempted": true, "succeeded": true},
		"exit_evidence":        map[string]any{"reaped": true, "code": 0.0},
		"signal_evidence":      map[string]any{"attempted": false, "succeeded": false},
		"timed_out":            false,
		"output": map[string]any{
			"stdout_bytes": 3.0, "stderr_bytes": 0.0, "raw_bytes": 3.0, "returned_bytes": 3.0, "output_complete": true,
			"truncated": false, "attribution": "helper_owned_child_pipes",
		},
		"evidence_quality":   "complete",
		"evidence_authority": "context_exec_child_owned_v1",
	}
	payload := map[string]any{"schema_version": 2.0, "ok": true, "action": "context.exec", "context_exec": state}
	if err := resolvedSchema(t, MCPOutputV2).Validate(payload); err != nil {
		t.Fatalf("canonical context.exec output rejected: %v", err)
	}
}

func TestContextExecCapabilitySchemaExposesClosedNativeContract(t *testing.T) {
	server := delegatedCapabilityServerPayload()
	server["receipt_schema_versions"] = append(server["receipt_schema_versions"].([]any), 5.0, 6.0)
	features := server["features"].(map[string]any)
	features["delegated_interactive"] = "available"
	features["interactive_handoff"] = "available"
	features["context_exec"] = "available"
	server["delegated_interactive"] = map[string]any{
		"provider_id": "tmux_control_mode", "provider_version": 1.0, "platform": "darwin",
		"max_mutation_records": 4096.0, "daemon_restart_continuity": true, "host_reboot_continuity": false,
	}
	server["interactive_handoff"] = map[string]any{
		"manual_standard": true, "secret": true, "automatic_readiness": false,
		"privacy":           map[string]any{"secret_private_interval": true, "privacy_release_separate": true, "observer_topology_qualified": true, "human_input_persisted": false},
		"capture_qualities": []any{"complete", "partial", "incomplete"},
	}
	server["context_exec"] = map[string]any{
		"provider_id": "tmux_control_mode", "provider_version": 1.0, "platform": "darwin",
		"shell_adapters": []any{"fish", "zsh", "bash", "nushell"}, "helper_protocol_version": 3.0,
		"evidence_authority":   "context_exec_child_owned_v1",
		"evidence_qualities":   []any{"unproven", "incomplete", "complete", "ambiguous"},
		"output_attribution":   "helper_owned_child_pipes",
		"resource_enforcement": "unavailable", "hermetic": "unavailable",
	}
	for _, tc := range []struct {
		name    Name
		payload map[string]any
	}{
		{MCPOutputV2, map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.server", "server": server}},
		{IPCV2, map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "ctx-cap", "action": "inspect.server", "ok": true, "server": server}},
	} {
		if err := resolvedSchema(t, tc.name).Validate(tc.payload); err != nil {
			t.Fatalf("%s rejected context exec capability: %v", tc.name, err)
		}
	}

	bad := cloneSchemaMap(server)
	ctx := cloneSchemaMap(server["context_exec"].(map[string]any))
	ctx["resource_enforcement"] = "available"
	bad["context_exec"] = ctx
	if err := resolvedSchema(t, MCPOutputV2).Validate(map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.server", "server": bad}); err == nil {
		t.Fatal("context exec capability schema accepted inherited resource enforcement")
	}
}
