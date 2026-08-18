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

func TestStructuredCodeIntelligenceInspectWireSchemasAreClosed(t *testing.T) {
	workspaceID := "ws_01K00000000000000000000000"
	query := map[string]any{"kind": "diagnostics", "scope": "changed_files"}
	result := map[string]any{"status": "unavailable", "query": query}
	valid := []struct {
		schema Name
		value  map[string]any
	}{
		{MCPInputV2, map[string]any{"action": "inspect.code", "workspace_id": workspaceID, "activity_id": "ZMR-111-validator", "code_query": query}},
		{MCPOutputV2, map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.code", "code": result}},
		{IPCV2, map[string]any{"ipc_version": 2.0, "kind": "request", "request_id": "code", "action": "inspect.code", "workspace_id": workspaceID, "code_query": query}},
		{IPCV2, map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "code", "action": "inspect.code", "ok": true, "code": result}},
	}
	for _, tc := range valid {
		if err := resolvedSchema(t, tc.schema).Validate(tc.value); err != nil {
			t.Errorf("%s rejected inspect.code %#v: %v", tc.schema, tc.value, err)
		}
	}

	invalidQueries := []map[string]any{
		{"kind": "diagnostics", "scope": "changed_files", "uri": "file:///tmp/main.go"},
		{"kind": "diagnostics", "scope": "changed_files", "document_version": 1.0},
		{"kind": "diagnostics", "scope": "changed_files", "jsonrpc_id": 7.0},
		{"kind": "definition", "path": "main.go", "line": 1.0},
		{"kind": "diagnostics", "scope": "file", "path": "main.go", "line": 1.0, "column": 1.0},
		{"kind": "diagnostics", "scope": "repository"},
		{"kind": "hover", "path": "main.go", "line": 1.0, "column": 1.0},
		{"kind": "diagnostics", "scope": "changed_files", "provider": "mystery"},
	}
	for i, codeQuery := range invalidQueries {
		for _, schemaName := range []Name{MCPInputV2, IPCV2} {
			value := map[string]any{"action": "inspect.code", "workspace_id": workspaceID, "code_query": codeQuery}
			if schemaName == IPCV2 {
				value["ipc_version"] = 2.0
				value["kind"] = "request"
				value["request_id"] = "code"
			}
			if err := resolvedSchema(t, schemaName).Validate(value); err == nil {
				t.Errorf("%s accepted invalid code query %d: %#v", schemaName, i, codeQuery)
			}
		}
	}
	for _, schemaName := range []Name{MCPInputV2, IPCV2} {
		for _, missing := range []string{"workspace_id", "code_query"} {
			value := map[string]any{"action": "inspect.code", "workspace_id": workspaceID, "code_query": query}
			delete(value, missing)
			if schemaName == IPCV2 {
				value["ipc_version"] = 2.0
				value["kind"] = "request"
				value["request_id"] = "code"
			}
			if err := resolvedSchema(t, schemaName).Validate(value); err == nil {
				t.Errorf("%s accepted inspect.code missing %s", schemaName, missing)
			}
		}
	}
}

func TestCodeIntelligenceResolvedLocationAcceptsBoundedDisplayNavigation(t *testing.T) {
	query := map[string]any{"kind": "definition", "path": "main.go", "line": 2.0, "column": 9.0}
	location := map[string]any{
		"kind": "resolved",
		"resolved": map[string]any{
			"source_ref_id": "src_01K00000000000000000000000", "start_byte": 12.0, "end_byte": 13.0,
			"display": map[string]any{"path": "other.go", "line": 2.0, "column": 5.0, "end_line": 2.0, "end_column": 6.0, "preview": "var X = 1"},
		},
	}
	result := map[string]any{
		"status": "unavailable", "query": query,
		"records": []any{map[string]any{
			"kind": "location_target", "authority": "mechanical", "source_correlation": "current", "completeness": "provider_reported",
			"location_target": map[string]any{"name": "X", "relationship": "definition", "location": location},
		}},
	}
	for _, tc := range []struct {
		schema Name
		value  map[string]any
	}{
		{MCPOutputV2, map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.code", "code": result}},
		{IPCV2, map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "code-display", "action": "inspect.code", "ok": true, "code": result}},
	} {
		if err := resolvedSchema(t, tc.schema).Validate(tc.value); err != nil {
			t.Fatalf("%s rejected code display navigation: %v", tc.schema, err)
		}
	}
}

func TestProjectDiscoveryAndManifestUnboundReadinessWireSchemas(t *testing.T) {
	workspaceID := "ws_01K00000000000000000000000"
	repositoryID := "repo_01K00000000000000000000000"
	project := map[string]any{
		"status": "absent", "discovery_fingerprint": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"detected_families": []any{"go", "node"}, "discovery_evidence": []any{"go:go.mod", "node:package.json"},
		"confidence": "medium", "provenance": "workspace_discovery", "code": "project_manifest_absent",
	}
	readiness := map[string]any{
		"schema_version": 1.0, "state": "unavailable", "repository_id": repositoryID, "workspace_id": workspaceID,
		"captured_at": "2026-08-18T03:30:00Z", "cache_quality": "fresh", "cache_age_ms": 0.0, "checks": []any{},
		"diagnostic_code": "project_manifest_absent",
	}
	for _, tc := range []struct {
		schema Name
		value  map[string]any
	}{
		{MCPOutputV2, map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.project", "project": project}},
		{MCPOutputV2, map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.readiness", "readiness": readiness}},
		{IPCV2, map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "project-discovery", "action": "inspect.project", "ok": true, "project": project}},
		{IPCV2, map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "readiness-unbound", "action": "inspect.readiness", "ok": true, "readiness": readiness}},
	} {
		if err := resolvedSchema(t, tc.schema).Validate(tc.value); err != nil {
			t.Fatalf("%s rejected discovery/readiness: %v", tc.schema, err)
		}
	}
}
