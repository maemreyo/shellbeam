package schema

import "testing"

func TestAgentExecutionA1InspectWorkspaceActivityWireSchemas(t *testing.T) {
	workspaceID := "ws_01K00000000000000000000000"
	repositoryID := "repo_01K00000000000000000000000"
	workspace := map[string]any{
		"schema_version": 1.0,
		"workspace_id":   workspaceID,
		"repository_id":  repositoryID,
		"label":          "primary",
		"root":           "/tmp/repo",
		"git_dir":        "/tmp/repo/.git",
		"created_at":     "2026-08-14T03:30:00Z",
		"last_seen_at":   "2026-08-14T03:30:00Z",
	}
	activity := map[string]any{
		"schema_version":       1.0,
		"activity_id":          "activity-a1",
		"workspace_ids":        []any{workspaceID},
		"compacted_operations": 0.0,
		"created_at":           "2026-08-14T03:30:00Z",
		"updated_at":           "2026-08-14T03:30:00Z",
	}
	for _, schemaName := range []Name{MCPInputV2, IPCV2} {
		payload := map[string]any{"action": "start", "operation_id": "op-a1", "activity_id": "activity-a1", "workspace_id": workspaceID, "command": "true", "cwd": "."}
		if schemaName == IPCV2 {
			payload["ipc_version"] = 2.0
			payload["kind"] = "request"
			payload["request_id"] = "start"
		}
		if err := resolvedSchema(t, schemaName).Validate(payload); err == nil {
			continue
		} else {
			t.Errorf("%s rejected start activity binding: %v", schemaName, err)
		}
	}

	valid := []struct {
		schema Name
		value  map[string]any
	}{
		{MCPInputV2, map[string]any{"action": "inspect.workspace", "workspace_id": workspaceID}},
		{MCPInputV2, map[string]any{"action": "inspect.activity", "activity_id": "activity-a1"}},
		{MCPOutputV2, map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.workspace", "workspace": workspace}},
		{MCPOutputV2, map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.activity", "activity": activity}},
		{IPCV2, map[string]any{"ipc_version": 2.0, "kind": "request", "request_id": "w", "action": "inspect.workspace", "workspace_id": workspaceID}},
		{IPCV2, map[string]any{"ipc_version": 2.0, "kind": "request", "request_id": "a", "action": "inspect.activity", "activity_id": "activity-a1"}},
		{IPCV2, map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "w", "action": "inspect.workspace", "ok": true, "workspace": workspace}},
		{IPCV2, map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "a", "action": "inspect.activity", "ok": true, "activity": activity}},
	}
	for _, tc := range valid {
		if err := resolvedSchema(t, tc.schema).Validate(tc.value); err != nil {
			t.Errorf("%s rejected %v: %v", tc.schema, tc.value, err)
		}
	}

	invalid := []struct {
		schema Name
		value  map[string]any
	}{
		{MCPInputV2, map[string]any{"action": "inspect.workspace"}},
		{MCPInputV2, map[string]any{"action": "inspect.activity"}},
		{MCPInputV2, map[string]any{"action": "inspect.activity", "activity_id": "activity-a1", "workspace_id": workspaceID}},
		{IPCV2, map[string]any{"ipc_version": 2.0, "kind": "request", "request_id": "w", "action": "inspect.workspace"}},
		{IPCV2, map[string]any{"ipc_version": 2.0, "kind": "request", "request_id": "a", "action": "inspect.activity"}},
	}
	for _, tc := range invalid {
		if err := resolvedSchema(t, tc.schema).Validate(tc.value); err == nil {
			t.Errorf("%s accepted invalid %v", tc.schema, tc.value)
		}
	}
}
