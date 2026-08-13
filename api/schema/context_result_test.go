package schema

import "testing"

func TestV2ContextAndAdvisoryResultSchemas(t *testing.T) {
	result := map[string]any{
		"schema_version": 2.0,
		"operation":      map[string]any{"operation_id": "op", "workspace_id": "ws_01K00000000000000000000000", "session_id": "s", "state": "running"},
		"child":          map[string]any{"state": "running", "timed_out": false},
		"output":         map[string]any{"canonical_stream": "combined", "raw_bytes": 0.0, "returned_bytes": 0.0, "cursor": 0.0, "next_cursor": 0.0, "truncated": false, "output_complete": false},
		"context_events": []any{map[string]any{"code": "branch_changed", "message": "branch changed", "workspace_id": "ws_01K00000000000000000000000", "generation": "gen", "transition_fingerprint": "abc"}},
		"advisories":     []any{map[string]any{"code": "workspace_hint_mismatch", "severity": "warning", "message": "workspace context mismatch", "workspace_id": "ws_01K00000000000000000000000", "cause_fingerprint": "def"}},
	}
	mcp := map[string]any{"schema_version": 2.0, "ok": true, "action": "start", "result": result}
	if err := resolvedSchema(t, MCPOutputV2).Validate(mcp); err != nil {
		t.Fatalf("MCP context result rejected: %v", err)
	}
	ipc := map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "x", "action": "start", "ok": true, "result": result}
	if err := resolvedSchema(t, IPCV2).Validate(ipc); err != nil {
		t.Fatalf("IPC context result rejected: %v", err)
	}
}
