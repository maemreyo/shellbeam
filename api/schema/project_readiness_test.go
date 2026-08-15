package schema

import (
	"strings"
	"testing"
)

func TestProjectReadinessV2SchemasAreClosedAndValueFree(t *testing.T) {
	workspaceID := "ws_01K00000000000000000000000"
	readiness := projectReadinessSchemaValue()
	valid := []struct {
		name  Name
		value map[string]any
	}{
		{MCPInputV2, map[string]any{"action": "inspect.readiness", "workspace_id": workspaceID}},
		{IPCV2, map[string]any{"ipc_version": 2.0, "kind": "request", "request_id": "r", "action": "inspect.readiness", "workspace_id": workspaceID}},
		{MCPOutputV2, map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.readiness", "readiness": readiness}},
		{IPCV2, map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "r", "action": "inspect.readiness", "ok": true, "readiness": readiness}},
	}
	for _, tc := range valid {
		if err := resolvedSchema(t, tc.name).Validate(tc.value); err != nil {
			t.Errorf("%s rejected %#v: %v", tc.name, tc.value, err)
		}
	}
	invalid := []struct {
		name  Name
		value map[string]any
	}{
		{MCPInputV2, map[string]any{"action": "inspect.readiness"}},
		{MCPInputV2, map[string]any{"action": "inspect.readiness", "workspace_id": workspaceID, "operation_id": "op-1"}},
		{IPCV2, map[string]any{"ipc_version": 2.0, "kind": "request", "request_id": "r", "action": "inspect.readiness", "workspace_id": workspaceID, "command": "env"}},
		{MCPOutputV2, map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.readiness", "readiness": map[string]any{
			"schema_version": 1.0, "state": "ready", "repository_id": "repo_01K00000000000000000000000", "workspace_id": workspaceID,
			"manifest_digest": strings.Repeat("a", 64), "manifest_schema_version": 2.0, "captured_at": "2026-08-15T00:00:00Z",
			"cache_quality": "fresh", "cache_age_ms": 0.0, "checks": []any{}, "environment_value": "secret",
		}}},
	}
	for _, tc := range invalid {
		if err := resolvedSchema(t, tc.name).Validate(tc.value); err == nil {
			t.Errorf("%s accepted invalid %#v", tc.name, tc.value)
		}
	}
}

func projectReadinessSchemaValue() map[string]any {
	digest := strings.Repeat("a", 64)
	return map[string]any{
		"schema_version":          1.0,
		"state":                   "ready",
		"repository_id":           "repo_01K00000000000000000000000",
		"workspace_id":            "ws_01K00000000000000000000000",
		"manifest_digest":         digest,
		"manifest_schema_version": 2.0,
		"environment_fingerprint": strings.Repeat("b", 64),
		"toolchain_fingerprint":   strings.Repeat("c", 64),
		"captured_at":             "2026-08-15T00:00:00Z",
		"cache_quality":           "fresh",
		"cache_age_ms":            0.0,
		"checks": []any{
			map[string]any{"id": "go", "kind": "toolchain", "required": true, "status": "compatible", "provider_id": "go-host", "provider_version": 1.0},
			map[string]any{"id": "git", "kind": "executable", "required": true, "status": "available"},
			map[string]any{"id": "DATABASE_URL", "kind": "environment_presence", "required": true, "status": "present_nonempty"},
		},
	}
}
