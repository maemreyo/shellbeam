package schema

import "testing"

func TestInteractiveHandoffModernInputSchemasRecognizeFutureVocabularyButStayClosed(t *testing.T) {
	mcp := resolvedSchema(t, MCPInputV2)
	valid := []map[string]any{
		{"action": "handoff.request", "handoff_id": "handoff_1", "session_id": "session_1", "reason": "manual_intervention", "privacy": "standard", "completion": map[string]any{"kind": "manual_ready"}},
		{"action": "handoff.request", "handoff_id": "handoff_2", "session_id": "session_2", "reason": "credential_required", "privacy": "secret", "completion": map[string]any{"kind": "manual_ready"}},
		{"action": "handoff.request", "handoff_id": "handoff_3", "session_id": "session_3", "reason": "credential_required", "privacy": "standard", "completion": map[string]any{"kind": "environment_exported_nonempty", "name": "TOKEN"}},
		{"action": "handoff.wait", "handoff_id": "handoff_1", "yield-time_ms": 30000.0},
		{"action": "handoff.abort", "handoff_id": "handoff_1"},
		{"action": "inspect.handoff", "handoff_id": "handoff_1"},
	}
	for _, payload := range valid {
		if err := mcp.Validate(payload); err != nil {
			t.Errorf("valid MCP handoff rejected %v: %v", payload, err)
		}
	}
	invalid := []map[string]any{
		{"action": "handoff.request", "handoff_id": ".bad", "session_id": "session_1", "reason": "manual_intervention", "privacy": "standard", "completion": map[string]any{"kind": "manual_ready"}},
		{"action": "handoff.request", "handoff_id": "handoff_1", "session_id": "session_1", "reason": "manual_intervention", "privacy": "standard", "completion": map[string]any{"kind": "manual_ready", "extra": true}},
		{"action": "handoff.wait", "handoff_id": "handoff_1", "session_id": "cross_action"},
		{"action": "handoff.wait", "handoff_id": "handoff_1", "yield-time_ms": 30001.0},
	}
	for _, payload := range invalid {
		if err := mcp.Validate(payload); err == nil {
			t.Errorf("invalid MCP handoff accepted %v", payload)
		}
	}

	ipc := resolvedSchema(t, IPCV2)
	for i, payload := range valid {
		clone := map[string]any{"ipc_version": 2.0, "kind": "request", "request_id": "handoff-schema"}
		for k, v := range payload {
			clone[k] = v
		}
		if err := ipc.Validate(clone); err != nil {
			t.Errorf("valid IPC handoff %d rejected %v: %v", i, clone, err)
		}
	}

	legacy := resolvedSchema(t, MCPInputV1)
	if err := legacy.Validate(valid[0]); err == nil {
		t.Fatal("legacy MCP schema accepted interactive handoff")
	}
}

func TestInteractiveHandoffOutputSchemasExposeOnlyBoundedPublicProjection(t *testing.T) {
	state := publicHandoffSchemaPayload()
	mcp := resolvedSchema(t, MCPOutputV2)
	for _, payload := range []map[string]any{
		{"schema_version": 2.0, "ok": true, "action": "handoff.request", "handoff": state},
		{"schema_version": 2.0, "ok": true, "action": "handoff.wait", "handoff": state, "timed_out": true},
		{"schema_version": 2.0, "ok": true, "action": "handoff.abort", "handoff": state},
		{"schema_version": 2.0, "ok": true, "action": "inspect.handoff", "handoff": state},
	} {
		if err := mcp.Validate(payload); err != nil {
			t.Errorf("valid MCP handoff output rejected %v: %v", payload, err)
		}
	}
	ipc := resolvedSchema(t, IPCV2)
	for _, tc := range []struct {
		action string
		wait   bool
	}{{"handoff.request", false}, {"handoff.wait", true}, {"handoff.abort", false}, {"inspect.handoff", false}} {
		payload := map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "handoff-out", "action": tc.action, "ok": true, "handoff": state}
		if tc.wait {
			payload["handoff_timed_out"] = false
		}
		if err := ipc.Validate(payload); err != nil {
			t.Errorf("valid IPC handoff output rejected %v: %v", payload, err)
		}
	}

	for _, private := range []string{"provider_generation", "human_client", "client_ref", "provider_ref", "pane_id", "window_id", "tmux_session"} {
		bad := publicHandoffSchemaPayload()
		bad[private] = "must-not-exist"
		if err := mcp.Validate(map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.handoff", "handoff": bad}); err == nil {
			t.Errorf("public handoff schema accepted private field %q", private)
		}
	}
}

func TestInteractiveHandoffCapabilitySchemaIsManualStandardOnly(t *testing.T) {
	server := delegatedCapabilityServerPayload()
	server["features"].(map[string]any)["delegated_interactive"] = "available"
	server["features"].(map[string]any)["interactive_handoff"] = "available"
	server["delegated_interactive"] = map[string]any{"provider_id": "tmux_control_mode", "provider_version": 1.0, "platform": "darwin", "max_mutation_records": 4096.0, "daemon_restart_continuity": true, "host_reboot_continuity": false}
	server["interactive_handoff"] = map[string]any{"manual_standard": true, "secret": false, "automatic_readiness": false}
	if err := resolvedSchema(t, MCPOutputV2).Validate(map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.server", "server": server}); err != nil {
		t.Fatalf("manual H2 capability rejected: %v", err)
	}
	bad := delegatedCapabilityServerPayload()
	bad["features"].(map[string]any)["interactive_handoff"] = "available"
	bad["interactive_handoff"] = map[string]any{"manual_standard": true, "secret": true, "automatic_readiness": false}
	if err := resolvedSchema(t, MCPOutputV2).Validate(map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.server", "server": bad}); err == nil {
		t.Fatal("H2 capability schema accepted secret=true")
	}
}

func publicHandoffSchemaPayload() map[string]any {
	return map[string]any{
		"schema_version": 1.0,
		"handoff_id":     "handoff_1", "session_id": "session_1", "authority_epoch": 2.0,
		"status": "HUMAN_CONNECTING", "agent_ingress": "fenced", "human_ingress": "fenced",
		"transfer_boundary": map[string]any{"kind": "provider_ordered", "established": true},
		"attached":          false,
		"created_at":        "2026-08-19T10:00:00Z", "updated_at": "2026-08-19T10:00:01Z",
		"attach_argv": []any{"shellbeam", "session", "attach", "--handoff-id", "handoff_1"},
	}
}
