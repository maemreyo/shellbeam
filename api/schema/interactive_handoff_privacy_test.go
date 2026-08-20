package schema

import "testing"

func TestInteractiveHandoffSecretRequirementSchemasStayTypedAndSecretSafe(t *testing.T) {
	valid := map[string]any{
		"action": "handoff.request", "handoff_id": "handoff_secret_1", "session_id": "session_secret_1",
		"reason": "credential_required", "privacy": "secret",
		"completion": map[string]any{"kind": "environment_exported_nonempty", "name": "CONTROL_PLANE_API_KEY"},
	}
	if err := resolvedSchema(t, MCPInputV2).Validate(valid); err != nil {
		t.Fatalf("valid secret handoff rejected: %v", err)
	}
	for _, completion := range []map[string]any{
		{"kind": "environment_exported_nonempty", "name": "1TOKEN"},
		{"kind": "environment_exported_nonempty", "name": "TOKEN-VALUE"},
		{"kind": "environment_exported_nonempty", "name": "TOKEN", "expected_value": "secret"},
		{"kind": "environment_exported_nonempty", "name": "TOKEN", "script": "env"},
		{"kind": "environment_exported_nonempty", "name": "TOKEN", "regex": ".*"},
	} {
		bad := map[string]any{
			"action": "handoff.request", "handoff_id": "handoff_secret_1", "session_id": "session_secret_1",
			"reason": "credential_required", "privacy": "secret", "completion": completion,
		}
		if err := resolvedSchema(t, MCPInputV2).Validate(bad); err == nil {
			t.Errorf("unsafe completion accepted: %#v", completion)
		}
	}
}

func TestInteractiveHandoffH4OutputSchemasExposePrivacyStateButNoSecretMaterial(t *testing.T) {
	state := publicHandoffSchemaPayload()
	state["privacy_state"] = "private"
	state["privacy_release"] = "pending"
	state["capture_state"] = "private"
	for _, schema := range []Name{MCPOutputV2, IPCV2} {
		payload := map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.handoff", "handoff": state}
		if schema == IPCV2 {
			payload = map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "h4-private", "action": "inspect.handoff", "ok": true, "handoff": state}
		}
		if err := resolvedSchema(t, schema).Validate(payload); err != nil {
			t.Fatalf("%s rejected H4 public state: %v", schema, err)
		}
	}
	for _, forbidden := range []string{"secret_value", "secret_hash", "environment_value", "human_input", "private_output", "terminal_history"} {
		bad := publicHandoffSchemaPayload()
		bad["privacy_state"], bad["privacy_release"], bad["capture_state"] = "private", "pending", "private"
		bad[forbidden] = "must-never-exist"
		if err := resolvedSchema(t, MCPOutputV2).Validate(map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.handoff", "handoff": bad}); err == nil {
			t.Errorf("public H4 schema accepted secret-bearing field %q", forbidden)
		}
	}
}

func TestInteractiveHandoffH4CapabilitySchemaAdvertisesClosedProviderShellRequirementCaptureFacts(t *testing.T) {
	server := delegatedCapabilityServerPayload()
	server["features"].(map[string]any)["delegated_interactive"] = "available"
	server["features"].(map[string]any)["interactive_handoff"] = "available"
	server["delegated_interactive"] = map[string]any{"provider_id": "tmux_control_mode", "provider_version": 1.0, "platform": "darwin", "max_mutation_records": 4096.0, "daemon_restart_continuity": true, "host_reboot_continuity": false}
	server["interactive_handoff"] = map[string]any{
		"manual_standard": true, "secret": true, "automatic_readiness": true,
		"shell_integrations": []any{
			map[string]any{"shell": "fish", "level": "requirement_aware", "safe_boundary": true, "environment_exported_nonempty": true},
			map[string]any{"shell": "zsh", "level": "requirement_aware", "safe_boundary": true, "environment_exported_nonempty": true},
			map[string]any{"shell": "bash", "level": "requirement_aware", "safe_boundary": true, "environment_exported_nonempty": true},
		},
		"requirement_kinds": []any{"environment_exported_nonempty"},
		"privacy":           map[string]any{"secret_private_interval": true, "privacy_release_separate": true, "observer_topology_qualified": true, "human_input_persisted": false},
		"capture_qualities": []any{"complete", "partial", "incomplete"},
	}
	if err := resolvedSchema(t, MCPOutputV2).Validate(map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.server", "server": server}); err != nil {
		t.Fatalf("H4 capability rejected: %v", err)
	}
	bad := server["interactive_handoff"].(map[string]any)
	bad["requirement_kinds"] = []any{"shell_script"}
	if err := resolvedSchema(t, MCPOutputV2).Validate(map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.server", "server": server}); err == nil {
		t.Fatal("capability schema accepted arbitrary requirement kind")
	}
}
