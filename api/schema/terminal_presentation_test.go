package schema

import "testing"

func TestTerminalPresentationCapabilityExtendsH2WithoutAdvertisingH4(t *testing.T) {
	for _, name := range []Name{MCPOutputV2, IPCV2} {
		server := delegatedCapabilityServerPayload()
		server["features"].(map[string]any)["delegated_interactive"] = "available"
		server["features"].(map[string]any)["interactive_handoff"] = "available"
		server["delegated_interactive"] = map[string]any{"provider_id": "tmux_control_mode", "provider_version": 1.0, "platform": "darwin", "max_mutation_records": 4096.0, "daemon_restart_continuity": true, "host_reboot_continuity": false}
		server["interactive_handoff"] = map[string]any{
			"manual_standard": true, "secret": false, "automatic_readiness": false,
			"terminal_presentation": map[string]any{
				"resolution_sources":  []any{"active", "recent", "bridge_affinity", "single_running"},
				"qualified_launchers": []any{"ghostty"},
			},
		}
		var envelope map[string]any
		if name == MCPOutputV2 {
			envelope = map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.server", "server": server}
		} else {
			envelope = map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "terminal-cap", "action": "inspect.server", "ok": true, "server": server}
		}
		if err := resolvedSchema(t, name).Validate(envelope); err != nil {
			t.Fatalf("%s rejected H2+H3 capability: %v", name, err)
		}
	}
}

func TestTerminalPresentationIPCHandoffRequestAcceptsOnlyBoundedBridgeAffinity(t *testing.T) {
	base := map[string]any{
		"ipc_version": 2.0, "kind": "request", "request_id": "terminal-affinity", "action": "handoff.request",
		"handoff_id": "handoff_terminal", "session_id": "session_terminal", "reason": "manual_intervention", "privacy": "standard",
		"completion": map[string]any{"kind": "manual_ready"},
	}
	base["terminal_affinity"] = map[string]any{
		"identity": map[string]any{
			"provider_id": "ghostty", "provider_version": 1.0, "platform": "darwin",
			"bundle_id": "com.mitchellh.ghostty", "executable_name": "ghostty",
		},
		"observed_at": "2026-08-20T05:00:00Z", "fresh_until": "2026-08-20T05:01:00Z", "evidence_source": "bridge_affinity",
	}
	ipc := resolvedSchema(t, IPCV2)
	if err := ipc.Validate(base); err != nil {
		t.Fatalf("valid bridge affinity rejected: %v", err)
	}

	bad := cloneTerminalMap(base)
	affinity := cloneTerminalMap(base["terminal_affinity"].(map[string]any))
	affinity["evidence_source"] = "request_origin"
	bad["terminal_affinity"] = affinity
	if err := ipc.Validate(bad); err == nil {
		t.Fatal("IPC schema accepted unqualified request-origin terminal claim")
	}

	mcp := resolvedSchema(t, MCPInputV2)
	modelRequest := map[string]any{
		"action": "handoff.request", "handoff_id": "handoff_terminal", "session_id": "session_terminal",
		"reason": "manual_intervention", "privacy": "standard", "completion": map[string]any{"kind": "manual_ready"},
		"terminal_affinity": base["terminal_affinity"],
	}
	if err := mcp.Validate(modelRequest); err == nil {
		t.Fatal("public MCP input accepted bridge-owned terminal affinity")
	}
}

func cloneTerminalMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
