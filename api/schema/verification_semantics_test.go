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
		"schema_version": 1.0, "phase": "checkpoint", "repository_id": "repo_01K00000000000000000000000", "workspace_id": "ws_01K00000000000000000000000",
		"source_generation": "", "policy_state": "absent", "affected_surface": verificationUnknownAffectedSummary(), "obligations": []any{},
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
