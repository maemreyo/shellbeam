package schema

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/capability"
)

const a26WorkspaceID = "ws_01K00000000000000000000000"

func TestA26MutationScopeRequestSchemasAreClosedAndBounded(t *testing.T) {
	validMCP := []map[string]any{
		{"action": "mutation_scope.set", "mutation_id": "mutation-1", "scope_id": "scope-a", "activity_id": "activity-a", "workspace_id": a26WorkspaceID, "mode": "mutate", "paths": []any{"src/auth/**", "tests/auth/**"}},
		{"action": "mutation_scope.set", "mutation_id": "mutation-2", "scope_id": "scope-a", "activity_id": "activity-a", "workspace_id": a26WorkspaceID, "mode": "read", "paths": []any{"README.md"}, "ttl_ms": 1000.0},
		{"action": "mutation_scope.release", "mutation_id": "release-1", "scope_id": "scope-a"},
		{"action": "inspect.mutation_scopes", "workspace_id": a26WorkspaceID},
		{"action": "inspect.mutation_scopes", "workspace_id": a26WorkspaceID, "activity_id": "activity-a"},
	}
	for _, payload := range validMCP {
		if err := resolvedSchema(t, MCPInputV2).Validate(payload); err != nil {
			t.Errorf("valid MCP A2.6 rejected %v: %v", payload, err)
		}
	}
	invalidMCP := []map[string]any{
		{"action": "mutation_scope.set", "mutation_id": "mutation-1", "scope_id": "scope-a", "activity_id": "activity-a", "workspace_id": a26WorkspaceID, "mode": "mutate", "paths": []any{}},
		{"action": "mutation_scope.set", "mutation_id": "mutation-1", "scope_id": "scope-a", "activity_id": "activity-a", "workspace_id": a26WorkspaceID, "mode": "mutate", "paths": []any{"/private/path"}},
		{"action": "mutation_scope.set", "mutation_id": "mutation-1", "scope_id": "scope-a", "activity_id": "activity-a", "workspace_id": a26WorkspaceID, "mode": "mutate", "paths": []any{"src/*.go"}},
		{"action": "mutation_scope.set", "mutation_id": "mutation-1", "scope_id": "scope-a", "activity_id": "activity-a", "workspace_id": a26WorkspaceID, "mode": "mutate", "paths": []any{"src"}, "ttl_ms": 999.0},
		{"action": "mutation_scope.release", "mutation_id": "release-1", "scope_id": "scope-a", "workspace_id": a26WorkspaceID},
		{"action": "inspect.mutation_scopes", "activity_id": "activity-a"},
		{"action": "inspect.mutation_scopes", "workspace_id": a26WorkspaceID, "paths": []any{"src"}},
	}
	tooMany := make([]any, 17)
	for i := range tooMany {
		tooMany[i] = "p" + string(rune('a'+i))
	}
	invalidMCP = append(invalidMCP, map[string]any{"action": "mutation_scope.set", "mutation_id": "mutation-1", "scope_id": "scope-a", "activity_id": "activity-a", "workspace_id": a26WorkspaceID, "mode": "mutate", "paths": tooMany})
	for _, payload := range invalidMCP {
		if err := resolvedSchema(t, MCPInputV2).Validate(payload); err == nil {
			t.Errorf("invalid MCP A2.6 accepted %v", payload)
		}
	}

	validIPC := []map[string]any{
		{"ipc_version": 2.0, "kind": "request", "request_id": "set", "action": "mutation_scope.set", "mutation_id": "mutation-1", "scope_id": "scope-a", "activity_id": "activity-a", "workspace_id": a26WorkspaceID, "mode": "mutate", "paths": []any{"src/**"}, "ttl_ms": 1800000.0},
		{"ipc_version": 2.0, "kind": "request", "request_id": "release", "action": "mutation_scope.release", "mutation_id": "release-1", "scope_id": "scope-a"},
		{"ipc_version": 2.0, "kind": "request", "request_id": "inspect", "action": "inspect.mutation_scopes", "workspace_id": a26WorkspaceID},
	}
	for _, payload := range validIPC {
		if err := resolvedSchema(t, IPCV2).Validate(payload); err != nil {
			t.Errorf("valid IPC A2.6 rejected %v: %v", payload, err)
		}
	}
}

func TestA26MutationScopeResponseSchemasAreClosedAndBounded(t *testing.T) {
	mutation := a26MutationPayload()
	inspect := a26InspectPayload()
	validIPC := []map[string]any{
		{"ipc_version": 2.0, "kind": "response", "request_id": "set", "action": "mutation_scope.set", "ok": true, "mutation": mutation},
		{"ipc_version": 2.0, "kind": "response", "request_id": "release", "action": "mutation_scope.release", "ok": true, "mutation": map[string]any{"receipt": a26ReceiptPayload("released"), "replayed": false, "current_revision": false, "advisory_count": 0.0, "advisory_limit": 32.0}},
		{"ipc_version": 2.0, "kind": "response", "request_id": "inspect", "action": "inspect.mutation_scopes", "ok": true, "mutation_scopes": inspect},
		{"ipc_version": 2.0, "kind": "response", "request_id": "activity", "action": "inspect.activity", "ok": true, "activity": a26ActivityPayload(), "active_mutation_scopes": inspect["active_scopes"], "mutation_scope_advisories": inspect["advisories"], "mutation_scopes_truncated": false, "mutation_scope_advisories_truncated": false},
	}
	for _, payload := range validIPC {
		if err := resolvedSchema(t, IPCV2).Validate(payload); err != nil {
			t.Errorf("valid IPC output rejected %v: %v", payload, err)
		}
	}
	validMCP := []map[string]any{
		{"schema_version": 2.0, "ok": true, "action": "mutation_scope.set", "mutation": mutation},
		{"schema_version": 2.0, "ok": true, "action": "inspect.mutation_scopes", "mutation_scopes": inspect},
	}
	for _, payload := range validMCP {
		if err := resolvedSchema(t, MCPOutputV2).Validate(payload); err != nil {
			t.Errorf("valid MCP output rejected %v: %v", payload, err)
		}
	}
	leaky := a26MutationPayload()
	leaky["command"] = "rm -rf /"
	if err := resolvedSchema(t, MCPOutputV2).Validate(map[string]any{"schema_version": 2.0, "ok": true, "action": "mutation_scope.set", "mutation": leaky}); err == nil {
		t.Fatal("mutation response accepted command field")
	}
}

func a26MutationPayload() map[string]any {
	return map[string]any{"receipt": a26ReceiptPayload("set"), "scope": a26ScopePayload(), "replayed": false, "current_revision": true, "advisories": []any{}, "advisory_count": 0.0, "advisory_limit": 32.0}
}
func a26ReceiptPayload(result string) map[string]any {
	out := map[string]any{"schema_version": 1.0, "mutation_id": "mutation-1", "request_fingerprint": strings.Repeat("a", 64), "result": result, "scope_id": "scope-a", "committed_at": "2026-08-15T12:00:00Z"}
	if result == "set" {
		out["set_effect"] = "created"
		out["expires_at"] = "2026-08-15T12:15:00Z"
	}
	return out
}
func a26ScopePayload() map[string]any {
	return map[string]any{"schema_version": 1.0, "scope_id": "scope-a", "activity_id": "activity-a", "workspace_id": a26WorkspaceID, "mode": "mutate", "paths": []any{"src/**"}, "declared_at": "2026-08-15T12:00:00Z", "expires_at": "2026-08-15T12:15:00Z", "revision_id": "mutation-1"}
}
func a26InspectPayload() map[string]any {
	return map[string]any{"active_scopes": []any{a26ScopePayload()}, "advisories": []any{}, "active_count": 1.0, "advisory_count": 0.0, "active_scope_limit": 64.0, "advisory_limit": 32.0}
}
func a26ActivityPayload() map[string]any {
	return map[string]any{"schema_version": 1.0, "activity_id": "activity-a", "label": "activity-a", "operations": []any{}, "compacted_operations": 0.0, "created_at": "2026-08-15T12:00:00Z", "updated_at": "2026-08-15T12:00:00Z"}
}

func TestA26ServerCapabilitySchemasAcceptExactAdvertisedLimits(t *testing.T) {
	catalog := capability.Baseline(capability.Limits{}).WithMutationScopes(16, 64, 16, 256, 32, 900000, 1800000)
	raw, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	var server map[string]any
	if err := json.Unmarshal(raw, &server); err != nil {
		t.Fatal(err)
	}
	ipc := map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "server", "action": "inspect.server", "ok": true, "server": server}
	if err := resolvedSchema(t, IPCV2).Validate(ipc); err != nil {
		t.Fatalf("IPC server capability rejected: %v", err)
	}
	mcp := map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.server", "server": server}
	if err := resolvedSchema(t, MCPOutputV2).Validate(mcp); err != nil {
		t.Fatalf("MCP server capability rejected: %v", err)
	}
}
