package schema

import "testing"

func TestEvidenceMCPInputSchemaExposesClosedStartAndInspection(t *testing.T) {
	input := resolvedSchema(t, MCPInputV2)
	valid := []map[string]any{
		{
			"action":       "start",
			"operation_id": "evidence-start",
			"workspace_id": "ws_01K00000000000000000000000",
			"command":      "true",
			"evidence":     evidenceContractPayload(),
		},
		{
			"action":               "inspect.evidence",
			"workspace_id":         "ws_01K00000000000000000000000",
			"verification_kind":    "test",
			"result":               "pass",
			"revalidate_artifacts": true,
			"max_records":          2.0,
		},
	}
	for _, payload := range valid {
		if err := input.Validate(payload); err != nil {
			t.Errorf("valid MCP evidence input rejected %v: %v", payload, err)
		}
	}
	invalid := []map[string]any{
		{
			"action": "start", "operation_id": "evidence-start", "workspace_id": "ws_01K00000000000000000000000", "command": "true",
			"evidence": map[string]any{"verification_kind": "test", "secret": "must-not-pass"},
		},
		{
			"action": "start", "operation_id": "typed-evidence", "workspace_id": "ws_01K00000000000000000000000", "project_command_id": "build",
			"evidence": evidenceContractPayload(),
		},
		{"action": "poll", "session_id": "s", "revalidate_artifacts": true},
		{"action": "inspect.evidence", "operation_id": "../bad", "max_records": 1.0},
		{"action": "inspect.evidence", "verification_kind": "bogus", "max_records": 1.0},
	}
	for _, payload := range invalid {
		if err := input.Validate(payload); err == nil {
			t.Errorf("invalid MCP evidence input accepted %v", payload)
		}
	}
}

func TestEvidenceIPCSchemaExposesClosedStartInspectionAndResponse(t *testing.T) {
	ipc := resolvedSchema(t, IPCV2)
	valid := []map[string]any{
		{
			"ipc_version": 2.0, "kind": "request", "request_id": "start-evidence", "action": "start",
			"operation_id": "evidence-start", "workspace_id": "ws_01K00000000000000000000000", "command": "true",
			"evidence": evidenceContractPayload(),
		},
		{
			"ipc_version": 2.0, "kind": "request", "request_id": "inspect-evidence", "action": "inspect.evidence",
			"workspace_id": "ws_01K00000000000000000000000", "verification_kind": "test", "result": "pass",
			"revalidate_artifacts": true, "max_records": 2.0,
		},
		{
			"ipc_version": 2.0, "kind": "response", "request_id": "inspect-evidence", "action": "inspect.evidence", "ok": true,
			"evidence": evidenceNeverRunPayload(),
		},
	}
	for _, payload := range valid {
		if err := ipc.Validate(payload); err != nil {
			t.Errorf("valid IPC evidence payload rejected %v: %v", payload, err)
		}
	}
	invalid := []map[string]any{
		{
			"ipc_version": 2.0, "kind": "request", "request_id": "typed-evidence", "action": "start",
			"operation_id": "typed-evidence", "workspace_id": "ws_01K00000000000000000000000", "project_command_id": "build",
			"evidence": evidenceContractPayload(),
		},
		{
			"ipc_version": 2.0, "kind": "request", "request_id": "poll", "action": "poll", "session_id": "s",
			"evidence": evidenceContractPayload(),
		},
		{
			"ipc_version": 2.0, "kind": "response", "request_id": "inspect-evidence", "action": "inspect.evidence", "ok": true,
			"evidence": map[string]any{"schema_version": 1.0, "status": "bogus"},
		},
	}
	for _, payload := range invalid {
		if err := ipc.Validate(payload); err == nil {
			t.Errorf("invalid IPC evidence payload accepted %v", payload)
		}
	}
}

func TestEvidenceMCPOutputSchemaExposesClosedInspectionResult(t *testing.T) {
	output := resolvedSchema(t, MCPOutputV2)
	valid := []map[string]any{
		{"schema_version": 2.0, "ok": true, "action": "inspect.evidence", "evidence": evidenceNeverRunPayload()},
		{"schema_version": 2.0, "ok": true, "action": "inspect.evidence", "evidence": evidenceAvailablePayload()},
	}
	for _, payload := range valid {
		if err := output.Validate(payload); err != nil {
			t.Errorf("valid MCP evidence output rejected %v: %v", payload, err)
		}
	}
	invalid := []map[string]any{
		{"schema_version": 2.0, "ok": true, "action": "inspect.evidence", "evidence": map[string]any{"schema_version": 1.0, "status": "bogus"}},
		{"schema_version": 2.0, "ok": true, "action": "inspect.evidence", "evidence": map[string]any{"schema_version": 1.0, "status": "never_run", "artifact_body": "secret"}},
	}
	for _, payload := range invalid {
		if err := output.Validate(payload); err == nil {
			t.Errorf("invalid MCP evidence output accepted %v", payload)
		}
	}
}

func evidenceContractPayload() map[string]any {
	return map[string]any{
		"verification_kind": "test",
		"source_scope":      "full",
		"expected_outputs": []any{
			map[string]any{"path": "dist/report.json", "kind": "file", "digest": "sha256", "required": true},
		},
	}
}

func evidenceNeverRunPayload() map[string]any {
	return map[string]any{"schema_version": 1.0, "status": "never_run"}
}

func evidenceAvailablePayload() map[string]any {
	hex64 := "1111111111111111111111111111111111111111111111111111111111111111"
	return map[string]any{
		"schema_version": 1.0,
		"status":         "available",
		"records": []any{
			map[string]any{
				"record": map[string]any{
					"schema_version":    1.0,
					"evidence_id":       "ev_" + hex64,
					"operation_id":      "evidence-start",
					"session_id":        "session-1",
					"verification_kind": "test",
					"source_scope":      "full",
					"contract_digest":   hex64,
					"command":           map[string]any{},
					"receipt_digest":    hex64,
					"terminal":          map[string]any{"authoritative": true, "outcome": "success"},
					"result":            "pass",
					"source":            map[string]any{"observation_quality": "fast"},
					"completed_at":      "2026-08-15T08:00:00Z",
				},
				"validity": map[string]any{
					"source_match": "unknown", "freshness": "unknown", "artifact_match": "not_required", "policy_match": "unknown",
				},
				"current_source": map[string]any{"quality": "unknown"},
			},
		},
		"index_generation": 1.0,
	}
}
