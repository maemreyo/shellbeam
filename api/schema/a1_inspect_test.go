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

func TestLazyWorkspaceProvenanceReceiptSchemaVersionsAndClosure(t *testing.T) {
	schema := resolvedSchema(t, ReceiptV2)
	base := func() map[string]any {
		return map[string]any{
			"schema_version":        2.0,
			"operation_id":          "op",
			"session_id":            "s",
			"request_fingerprint":   "req",
			"execution_fingerprint": "exec",
			"daemon_incarnation":    "daemon",
			"state":                 "completed",
			"outcome":               "success",
			"tty":                   false,
			"timeout_ms":            0.0,
			"output_bytes":          0.0,
			"output_complete":       true,
			"input_accepted_bytes":  0.0,
			"input_delivered_bytes": 0.0,
			"stdin_closed":          false,
			"spawn_evidence":        map[string]any{"attempted": true, "succeeded": true},
			"exit_evidence":         map[string]any{"reaped": true, "code": 0.0},
			"signal_evidence":       map[string]any{"attempted": false, "succeeded": false},
		}
	}
	generationA := "gen_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	generationB := "gen_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	legacy := base()
	legacy["workspace_provenance"] = map[string]any{
		"schema_version":   1.0,
		"repository_id":    "repo_01K00000000000000000000000",
		"workspace_id":     "ws_01K00000000000000000000000",
		"pre_generation":   generationA,
		"post_generation":  generationB,
		"pre_quality":      "fresh",
		"post_quality":     "cached",
		"pre_observed_at":  "2026-08-14T03:30:00Z",
		"post_observed_at": "2026-08-14T03:30:01Z",
		"observed_change":  true,
	}
	if err := schema.Validate(legacy); err != nil {
		t.Fatalf("legacy provenance rejected: %v", err)
	}
	lazy := base()
	lazy["workspace_provenance"] = map[string]any{
		"schema_version":  2.0,
		"binding":         map[string]any{"repository_id": "repo_01K00000000000000000000000", "workspace_id": "ws_01K00000000000000000000000"},
		"pre":             map[string]any{"kind": "cached", "generation": generationA, "quality": "cached", "observed_at": "2026-08-14T03:30:00Z"},
		"post":            map[string]any{"kind": "unreconciled", "observation_invalidated": true},
		"observed_change": false,
	}
	if err := schema.Validate(lazy); err != nil {
		t.Fatalf("lazy provenance rejected: %v", err)
	}

	invalid := []map[string]any{
		{"schema_version": 2.0, "binding": map[string]any{}, "pre": map[string]any{"kind": "unreconciled"}, "post": map[string]any{"kind": "unreconciled", "generation": generationA, "observed_at": "2026-08-14T03:30:00Z"}, "observed_change": false},
		{"schema_version": 2.0, "binding": map[string]any{}, "pre": map[string]any{"kind": "freshly_sampled"}, "post": map[string]any{"kind": "unreconciled"}, "observed_change": false},
		{"schema_version": 2.0, "binding": map[string]any{}, "pre": map[string]any{"kind": "mystery"}, "post": map[string]any{"kind": "unreconciled"}, "observed_change": false},
		{"schema_version": 2.0, "binding": map[string]any{}, "pre": map[string]any{"kind": "unreconciled"}, "post": map[string]any{"kind": "unreconciled"}, "observed_change": false, "extra": true},
	}
	for i, provenance := range invalid {
		value := base()
		value["workspace_provenance"] = provenance
		if err := schema.Validate(value); err == nil {
			t.Fatalf("invalid provenance case %d accepted: %#v", i, provenance)
		}
	}
}
