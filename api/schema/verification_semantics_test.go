package schema

import "testing"

func verificationUnknownAffectedSummary() map[string]any {
	return map[string]any{
		"relation_count": 0.0,
		"domains": []any{map[string]any{
			"domain_id": "dom_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"kind":      "source_selection", "derivation_authority": "mechanical", "coverage": "unknown",
			"source_generation": "", "provenance_refs": []any{"selection:unknown"}, "captured_at": "2026-08-18T00:00:00Z",
		}},
		"by_authority": map[string]any{}, "by_coverage": map[string]any{},
	}
}

func TestVerificationInspectionSchemasAcceptUnknownGenerationWithoutInventingIdentity(t *testing.T) {
	verification := map[string]any{
		"schema_version": 2.0, "phase": "checkpoint", "repository_id": "repo_01K00000000000000000000000", "workspace_id": "ws_01K00000000000000000000000",
		"source_generation": "", "policy_state": "absent", "affected_surface": verificationUnknownAffectedSummary(),
		"gate": verificationV2Gate("indeterminate"), "obligations": []any{}, "obligation_views": []any{},
	}
	mcp := map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.verification", "verification": verification}
	if err := resolvedSchema(t, MCPOutputV2).Validate(mcp); err != nil {
		t.Fatalf("MCP rejected truthful unknown generation: %v", err)
	}
	ipc := map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "r", "action": "inspect.verification", "ok": true, "verification": verification}
	if err := resolvedSchema(t, IPCV2).Validate(ipc); err != nil {
		t.Fatalf("IPC rejected truthful unknown generation: %v", err)
	}
}

func verificationV2Gate(status string) map[string]any {
	return map[string]any{
		"status":       status,
		"breakdown":    map[string]any{"evidence_satisfied": 0.0, "waived": 0.0, "blocking": 0.0, "indeterminate": 1.0},
		"reason_codes": []any{"policy_absent"},
	}
}

func TestVerificationInspectionV2SchemaIsAdditiveAndRejectsCompletionTruth(t *testing.T) {
	verification := map[string]any{
		"schema_version": 2.0, "phase": "checkpoint", "repository_id": "repo_01K00000000000000000000000", "workspace_id": "ws_01K00000000000000000000000",
		"source_generation": "", "policy_state": "absent", "affected_surface": verificationUnknownAffectedSummary(),
		"gate": verificationV2Gate("indeterminate"), "obligations": []any{}, "obligation_views": []any{},
	}
	mcp := map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.verification", "verification": verification}
	if err := resolvedSchema(t, MCPOutputV2).Validate(mcp); err != nil {
		t.Fatalf("MCP rejected v2 verification inspection: %v", err)
	}
	ipc := map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "r", "action": "inspect.verification", "ok": true, "verification": verification}
	if err := resolvedSchema(t, IPCV2).Validate(ipc); err != nil {
		t.Fatalf("IPC rejected v2 verification inspection: %v", err)
	}
	for _, forbidden := range []string{"task_complete", "work_complete", "safe_to_finish"} {
		copy := map[string]any{}
		for key, value := range verification {
			copy[key] = value
		}
		copy[forbidden] = true
		bad := map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.verification", "verification": copy}
		if err := resolvedSchema(t, MCPOutputV2).Validate(bad); err == nil {
			t.Fatalf("MCP accepted forbidden completion truth %q", forbidden)
		}
	}
}

func TestVerificationInspectionV2SchemaAcceptsEvidenceAwareViewAndUnavailableCost(t *testing.T) {
	obl := "obl_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	ev := "ev_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	verification := map[string]any{
		"schema_version": 2.0, "phase": "checkpoint", "repository_id": "repo_01K00000000000000000000000", "workspace_id": "ws_01K00000000000000000000000",
		"source_generation": "gen_1111111111111111111111111111111111111111111111111111111111111111", "policy_state": "effective", "affected_surface": verificationUnknownAffectedSummary(),
		"gate":        map[string]any{"status": "clear", "breakdown": map[string]any{"evidence_satisfied": 1.0, "waived": 0.0, "blocking": 0.0, "indeterminate": 0.0}},
		"obligations": []any{},
		"obligation_views": []any{map[string]any{
			"obligation_id": obl, "source_rule_id": "integration", "disposition": "required_now", "evidence_status": "satisfied", "sufficiency_basis": "current integration evidence",
			"requirement_results": []any{map[string]any{
				"evaluation_id": "eval_cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", "policy_digest": "pol_dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
				"rule_id": "integration", "obligation_id": obl, "requirement_id": "integration", "status": "satisfied", "evidence_refs": []any{ev},
			}},
			"evidence_refs": []any{ev},
		}},
		"cost_summary": []any{map[string]any{
			"obligation_id": obl, "requirement_id": "integration", "provider_class": "integration_test", "execution": map[string]any{},
			"cost": map[string]any{
				"wall_ms": map[string]any{"quality": "unavailable"}, "output_bytes": map[string]any{"quality": "unavailable"},
				"cpu_user_ms": map[string]any{"quality": "unavailable"}, "cpu_system_ms": map[string]any{"quality": "unavailable"},
				"max_rss_bytes": map[string]any{"quality": "unavailable"}, "process_count_peak": map[string]any{"quality": "unavailable"},
				"provider_cost": map[string]any{"quality": "unavailable"}, "model_cost": map[string]any{"quality": "unavailable"},
			},
		}},
	}
	for name, payload := range map[string]map[string]any{
		"mcp": {"schema_version": 2.0, "ok": true, "action": "inspect.verification", "verification": verification},
		"ipc": {"ipc_version": 2.0, "kind": "response", "request_id": "r", "action": "inspect.verification", "ok": true, "verification": verification},
	} {
		var err error
		if name == "mcp" {
			err = resolvedSchema(t, MCPOutputV2).Validate(payload)
		} else {
			err = resolvedSchema(t, IPCV2).Validate(payload)
		}
		if err != nil {
			t.Fatalf("%s rejected evidence-aware v2 inspection: %v", name, err)
		}
	}
}
