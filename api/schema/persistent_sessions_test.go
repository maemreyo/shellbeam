package schema

import (
	"encoding/json"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/capability"
)

func TestPersistentSessionModernSchemasAcceptClosedB1SurfaceAndLegacyRejectsIt(t *testing.T) {
	mcpInput := resolvedSchema(t, MCPInputV2)
	for _, payload := range []map[string]any{
		{"action": "start", "operation_id": "persistent-schema-op", "command": "sleep 10", "cwd": "/tmp", "persistent": true, "session_name": "dev-server"},
		{"action": "inspect.sessions", "session_name": "dev-server", "state": "running", "persistent_only": false, "max_records": 10.0},
	} {
		if err := mcpInput.Validate(payload); err != nil {
			t.Errorf("valid MCP B1 rejected %v: %v", payload, err)
		}
	}
	for _, payload := range []map[string]any{
		{"action": "start", "operation_id": "persistent-schema-op", "command": "true", "cwd": "/tmp", "session_name": "dev-server"},
		{"action": "start", "operation_id": "persistent-schema-op", "command": "true", "cwd": "/tmp", "persistent": true, "tty": true},
		{"action": "inspect.sessions", "max_records": 101.0},
		{"action": "inspect.sessions", "state": "live"},
		{"action": "inspect.sessions", "command": "true"},
	} {
		if err := mcpInput.Validate(payload); err == nil {
			t.Errorf("invalid MCP B1 accepted %v", payload)
		}
	}

	ipcInput := resolvedSchema(t, IPCV2)
	for _, payload := range []map[string]any{
		{"ipc_version": 2.0, "kind": "request", "request_id": "start", "action": "start", "operation_id": "persistent-schema-op", "command": "sleep 10", "cwd": "/tmp", "persistent": true, "session_name": "dev-server"},
		{"ipc_version": 2.0, "kind": "request", "request_id": "inspect", "action": "inspect.sessions", "persistent_only": false, "max_records": 25.0},
	} {
		if err := ipcInput.Validate(payload); err != nil {
			t.Errorf("valid IPC B1 rejected %v: %v", payload, err)
		}
	}

	sessions := sessionInspectSchemaPayload()
	if err := resolvedSchema(t, MCPOutputV2).Validate(map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.sessions", "sessions": sessions}); err != nil {
		t.Fatalf("MCP B1 output rejected: %v", err)
	}
	if err := ipcInput.Validate(map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "inspect", "action": "inspect.sessions", "ok": true, "sessions": sessions}); err != nil {
		t.Fatalf("IPC B1 output rejected: %v", err)
	}

	legacy := resolvedSchema(t, MCPInputV1)
	for _, payload := range []map[string]any{
		{"action": "start", "operation_id": "legacy-op", "command": "true", "cwd": "/tmp", "persistent": true},
		{"action": "inspect.sessions"},
	} {
		if err := legacy.Validate(payload); err == nil {
			t.Errorf("legacy schema accepted B1 %v", payload)
		}
	}
}

func sessionInspectSchemaPayload() map[string]any {
	return map[string]any{"sessions": []any{map[string]any{
		"session_id": "persistent-schema-session", "session_name": "dev-server", "operation_id": "persistent-schema-op",
		"state": "running", "persistent": true, "ownership_status": "current",
		"created_at": "2026-08-16T02:00:00Z", "updated_at": "2026-08-16T02:00:01Z", "output_bytes": 0.0,
	}}, "continuation": "pscur_v1_payload.signature"}
}

func TestPersistentSessionModernServerSchemaAcceptsExactCapabilityProjection(t *testing.T) {
	catalog := capability.Baseline(capability.Limits{LiveSessions: 4, SessionOutputBytes: 4096}).WithNamedSessions(4, 4096, 512)
	raw, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	var server map[string]any
	if err := json.Unmarshal(raw, &server); err != nil {
		t.Fatal(err)
	}
	if err := resolvedSchema(t, MCPOutputV2).Validate(map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.server", "server": server}); err != nil {
		t.Fatalf("modern B1 capability rejected: %v", err)
	}
}

func TestPersistentSessionEventKindsArePublicSafeSchemaValues(t *testing.T) {
	for _, kind := range []string{"persistent_session_started", "persistent_session_reattached", "persistent_session_terminal", "persistent_session_lost"} {
		event := map[string]any{"schema_version": 1.0, "event_id": "evt_01K00000000000000000000000", "state_root_epoch": "epoch-a", "change_seq": 1.0, "kind": kind, "recorded_at": "2026-08-16T02:00:00Z", "correlation": map[string]any{"operation_id": "persistent-event-op", "session_id": "persistent-event-session"}, "subject_ref": "persistent:persistent-event-session:safe", "summary": kind}
		mcp := map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.events", "events": map[string]any{"continuity": "complete", "events": []any{event}, "next_event_cursor": "evtcur_v1_payload.signature", "truncated": false}}
		if err := resolvedSchema(t, MCPOutputV2).Validate(mcp); err != nil {
			t.Errorf("MCP event %s rejected: %v", kind, err)
		}
	}
}

func TestPersistentTerminalResultSchemasAcceptReceiptV4(t *testing.T) {
	receipt := map[string]any{
		"schema_version": 4.0, "operation_id": "persistent-v4-op", "session_id": "persistent-v4-session",
		"request_fingerprint": "request", "execution_fingerprint": "execution", "daemon_incarnation": "daemon-v4",
		"execution_mode": "shell", "executable": "/bin/sh", "state": "failed", "outcome": "failure", "shell": "/bin/sh", "cwd": "/tmp",
		"tty": false, "timeout_ms": 0.0, "persistent": true, "session_name": "dev-server", "output_bytes": 0.0, "output_complete": true,
		"input_accepted_bytes": 0.0, "input_delivered_bytes": 0.0, "stdin_closed": false,
		"spawn_evidence": map[string]any{"attempted": true, "succeeded": true}, "exit_evidence": map[string]any{"reaped": true, "code": 1.0}, "signal_evidence": map[string]any{"attempted": false, "succeeded": false},
	}
	result := map[string]any{"schema_version": 2.0, "operation": map[string]any{"operation_id": "persistent-v4-op", "session_id": "persistent-v4-session", "state": "terminal"}, "child": map[string]any{"state": "exited", "outcome": "failure", "exit_code": 1.0, "timed_out": false}, "output": map[string]any{"canonical_stream": "combined", "raw_bytes": 0.0, "returned_bytes": 0.0, "cursor": 0.0, "next_cursor": 0.0, "truncated": false, "output_complete": true}, "receipt": receipt}
	if err := resolvedSchema(t, MCPOutputV2).Validate(map[string]any{"schema_version": 2.0, "ok": true, "action": "start", "result": result}); err != nil {
		t.Fatalf("MCP receipt v4 rejected: %v", err)
	}
	if err := resolvedSchema(t, IPCV2).Validate(map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "v4", "action": "start", "ok": true, "result": result}); err != nil {
		t.Fatalf("IPC receipt v4 rejected: %v", err)
	}
}
