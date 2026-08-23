package schema

import (
	"encoding/json"
	"strings"
	"testing"

	dp "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
)

type decisionTransportCase struct {
	required map[string]any
	optional map[string]any
}

func decisionTransportCases() map[string]decisionTransportCase {
	policy := map[string]any{
		"content": map[string]any{
			"policy_id":       "policy-transport",
			"episode_kinds":   []any{"DIAGNOSIS"},
			"override_policy": map[string]any{"allowed": false},
		},
	}
	return map[string]decisionTransportCase{
		"decision.policy.snapshot":       {required: map[string]any{"policy": policy}},
		"decision.policy.activate":       {required: map[string]any{"activation_id": "activate-transport", "policy_digest": "pol_" + strings.Repeat("a", 64), "proposal_generation": "gen_" + strings.Repeat("b", 64), "expected_previous_policy_digest": "absent", "actor_ref": "actor"}},
		"decision.create":                {required: map[string]any{"episode_id": "episode-transport", "episode_kind": "DIAGNOSIS", "actor_ref": "actor"}, optional: map[string]any{"predecessor_episode_id": "episode-before", "expected_policy_digest": "pol_" + strings.Repeat("a", 64), "expected_activation_ref": "activation-before"}},
		"decision.inspect":               {required: map[string]any{"episode_id": "episode-transport"}, optional: map[string]any{"candidate_id": "candidate-transport"}},
		"decision.evaluate":              {required: map[string]any{"episode_id": "episode-transport", "candidate_id": "candidate-transport"}},
		"decision.close_unresolved":      {required: map[string]any{"episode_id": "episode-transport", "actor_ref": "actor", "expected_projection_digest": "proj_" + strings.Repeat("c", 64), "reason": "unresolved", "unresolved_dimensions": []any{}}},
		"decision.candidate.create":      {required: map[string]any{"episode_id": "episode-transport", "candidate": map[string]any{"candidate_id": "candidate-transport", "semantic_claim": "A"}, "actor_ref": "actor"}},
		"decision.candidate.revise":      {required: map[string]any{"episode_id": "episode-transport", "parent_candidate_id": "candidate-parent", "candidate": map[string]any{"candidate_id": "candidate-child", "semantic_claim": "B"}, "actor_ref": "actor"}},
		"decision.experiment.define":     {required: map[string]any{"episode_id": "episode-transport", "experiment_id": "experiment-transport", "actor_ref": "actor"}},
		"decision.prediction.bind":       {required: map[string]any{"episode_id": "episode-transport", "experiment_id": "experiment-transport", "prediction": map[string]any{"prediction_id": "prediction-transport", "candidate_id": "candidate-transport", "role": "REQUIRED_PREDICTION", "predicate": map[string]any{"kind": "OPERATION_OUTCOME", "operation_outcome": map[string]any{"expected_outcome": "SUCCESS"}}}}},
		"decision.experiment.seal":       {required: map[string]any{"experiment_id": "experiment-transport", "actor_ref": "actor"}},
		"decision.experiment.close":      {required: map[string]any{"experiment_id": "experiment-transport", "actor_ref": "actor"}},
		"decision.experiment.abort":      {required: map[string]any{"experiment_id": "experiment-transport", "abort_phase": "BEFORE_EXECUTION", "actor_ref": "actor", "reason": "stop"}},
		"decision.assessment.record":     {required: map[string]any{"episode_id": "episode-transport", "assessment": map[string]any{"assessment_id": "assessment-transport", "declared_context_class": "SAME_CONTEXT", "preferred_candidates": []any{"candidate-transport"}}, "actor_ref": "actor"}},
		"decision.selection.propose":     {required: map[string]any{"episode_id": "episode-transport", "candidate_id": "candidate-transport", "actor_ref": "actor"}, optional: map[string]any{"reason": "prefer evidence"}},
		"decision.override.create":       {required: map[string]any{"episode_id": "episode-transport", "candidate_id": "candidate-transport", "expected_policy_digest": "pol_" + strings.Repeat("a", 64), "expected_projection_digest": "proj_" + strings.Repeat("c", 64), "blocking_requirement_digest": "block_" + strings.Repeat("d", 64), "authority_attestation_ref": "attestation-transport", "reason": "override"}},
		"decision.selection.commit":      {required: map[string]any{"episode_id": "episode-transport", "candidate_id": "candidate-transport", "actor_ref": "actor", "expected_policy_digest": "pol_" + strings.Repeat("a", 64), "expected_projection_digest": "proj_" + strings.Repeat("c", 64), "idempotency_key": "idem-transport"}, optional: map[string]any{"override_ref": "override-transport"}},
		"decision.authority.materialize": {required: map[string]any{"authority_request": map[string]any{"required_authority_class": map[string]any{"domain": "shellbeam", "class_id": "explicit_caller", "version": 1.0}, "required_scope": map[string]any{"repository_id": "repo-transport", "episode_id": "episode-transport", "action_kind": "COMMIT_SELECTION_OVERRIDE"}}}},
	}
}

func cloneAnyMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func decisionSchemaPayload(name Name, action string, decision map[string]any) map[string]any {
	payload := map[string]any{"action": action, "decision": decision}
	if name == IPCV2 {
		payload["ipc_version"] = 2.0
		payload["kind"] = "request"
		payload["request_id"] = "decision-request"
	}
	return payload
}

func TestDecisionProtocolTransportSchemasAcceptExactActionMatrix(t *testing.T) {
	cases := decisionTransportCases()
	if len(cases) != 18 {
		t.Fatalf("actions=%d", len(cases))
	}
	for _, schemaName := range []Name{MCPInputV2, IPCV2} {
		schemaName := schemaName
		t.Run(string(schemaName), func(t *testing.T) {
			schema := resolvedSchema(t, schemaName)
			for action, tc := range cases {
				t.Run(action+"_minimum", func(t *testing.T) {
					if err := schema.Validate(decisionSchemaPayload(schemaName, action, cloneAnyMap(tc.required))); err != nil {
						t.Fatalf("minimum rejected: %v", err)
					}
				})
				if len(tc.optional) != 0 {
					t.Run(action+"_optional", func(t *testing.T) {
						decision := cloneAnyMap(tc.required)
						for key, value := range tc.optional {
							decision[key] = value
						}
						if err := schema.Validate(decisionSchemaPayload(schemaName, action, decision)); err != nil {
							t.Fatalf("optional fields rejected: %v", err)
						}
					})
				}
			}
		})
	}
}

func TestDecisionProtocolTransportSchemasAcceptOptionalOuterWorkspaceSelector(t *testing.T) {
	workspaceID := "ws_01K00000000000000000000000"
	for _, schemaName := range []Name{MCPInputV2, IPCV2} {
		schemaName := schemaName
		t.Run(string(schemaName), func(t *testing.T) {
			schema := resolvedSchema(t, schemaName)
			payload := decisionSchemaPayload(schemaName, "decision.inspect", cloneAnyMap(decisionTransportCases()["decision.inspect"].required))
			payload["workspace_id"] = workspaceID
			if err := schema.Validate(payload); err != nil {
				t.Fatalf("valid outer workspace selector rejected: %v", err)
			}
			payload["workspace_id"] = "not-a-workspace-id"
			if err := schema.Validate(payload); err == nil {
				t.Fatal("invalid outer workspace selector accepted")
			}
		})
	}
}

func TestDecisionProtocolTransportSchemasRejectRequiredOmissionsAndServerOwnedFields(t *testing.T) {
	for _, schemaName := range []Name{MCPInputV2, IPCV2} {
		schemaName := schemaName
		t.Run(string(schemaName), func(t *testing.T) {
			schema := resolvedSchema(t, schemaName)
			for action, tc := range decisionTransportCases() {
				for required := range tc.required {
					t.Run(action+"_missing_"+required, func(t *testing.T) {
						decision := cloneAnyMap(tc.required)
						delete(decision, required)
						if err := schema.Validate(decisionSchemaPayload(schemaName, action, decision)); err == nil {
							t.Fatalf("missing required %s accepted", required)
						}
					})
				}
			}
			bad := []struct {
				action string
				field  string
				value  any
			}{
				{"decision.authority.materialize", "actor_ref", "forged"},
				{"decision.override.create", "actor_ref", "forged"},
				{"decision.experiment.close", "prediction_results", []any{}},
				{"decision.inspect", "workspace_id", "ws_01K00000000000000000000000"},
				{"decision.inspect", "repository_id", "repo_01K00000000000000000000000"},
				{"decision.assessment.record", "qualified_context_class", "HUMAN"},
			}
			for _, tc := range bad {
				decision := cloneAnyMap(decisionTransportCases()[tc.action].required)
				decision[tc.field] = tc.value
				if err := schema.Validate(decisionSchemaPayload(schemaName, tc.action, decision)); err == nil {
					t.Fatalf("%s accepted server-owned/cross-action %s", tc.action, tc.field)
				}
			}
		})
	}
}

func TestDecisionProtocolTransportSchemasKeepExperimentIDStartOnly(t *testing.T) {
	for _, schemaName := range []Name{MCPInputV2, IPCV2} {
		schema := resolvedSchema(t, schemaName)
		start := map[string]any{"action": "start", "operation_id": "op-exp", "command": "true", "cwd": "/tmp", "experiment_id": "experiment-transport"}
		poll := map[string]any{"action": "poll", "session_id": "session-transport", "experiment_id": "experiment-transport"}
		if schemaName == IPCV2 {
			for _, payload := range []map[string]any{start, poll} {
				payload["ipc_version"] = 2.0
				payload["kind"] = "request"
				payload["request_id"] = "experiment-request"
			}
		}
		if err := schema.Validate(start); err != nil {
			t.Fatalf("%s start experiment rejected: %v", schemaName, err)
		}
		if err := schema.Validate(poll); err == nil {
			t.Fatalf("%s poll accepted experiment_id", schemaName)
		}
	}
}

func TestDecisionProtocolOutputSchemasCarryBoundedTypedResult(t *testing.T) {
	mcp := map[string]any{
		"schema_version": 2.0,
		"ok":             true,
		"action":         "decision.authority.materialize",
		"decision":       map[string]any{"authority_status": "UNKNOWN"},
	}
	if err := resolvedSchema(t, MCPOutputV2).Validate(mcp); err != nil {
		t.Fatalf("mcp decision response rejected: %v", err)
	}
	ipc := map[string]any{
		"ipc_version": 2.0,
		"kind":        "response",
		"request_id":  "decision-response",
		"action":      "decision.authority.materialize",
		"ok":          true,
		"decision":    map[string]any{"authority_status": "UNKNOWN"},
	}
	if err := resolvedSchema(t, IPCV2).Validate(ipc); err != nil {
		t.Fatalf("ipc decision response rejected: %v", err)
	}
	leaky := cloneAnyMap(mcp)
	leaky["decision"] = map[string]any{"authority_status": "UNKNOWN", "secret": true}
	if err := resolvedSchema(t, MCPOutputV2).Validate(leaky); err == nil {
		t.Fatal("mcp decision response accepted unknown result field")
	}
}

func TestDecisionProtocolOutputSchemasAcceptRealGoProjectionJSON(t *testing.T) {
	projection := dp.DecisionProjection{
		EpisodeID:                     "episode-transport",
		EpisodeState:                  dp.EpisodeOpen,
		EpisodeKind:                   dp.EpisodeDiagnosis,
		PolicyBinding:                 dp.EpisodePolicyBinding{PolicyID: "policy-transport", PolicyDigest: "pol_" + strings.Repeat("a", 64), ActivationRef: "activation-transport"},
		SourceGenerationCompatibility: dp.SourceGenerationCurrent,
		ProjectionDigest:              "proj_" + strings.Repeat("b", 64),
		AuditDigest:                   "audit_" + strings.Repeat("c", 64),
		Protocol: dp.DecisionProtocolEvaluation{
			EpisodeID:                 "episode-transport",
			CandidateID:               "candidate-transport",
			RequirementEvaluations:    []dp.DecisionRequirementEvaluation{},
			CandidateContractBlockers: []dp.CandidateContractBlocker{},
			Gate:                      dp.GateClear,
			BlockingRequirementDigest: "block_" + strings.Repeat("d", 64),
		},
		SourceCompatible: true,
		Budget: dp.BudgetAdmission{
			MayStartExperiment: true,
			MayLinkOperation:   true,
			MachineWallQuality: dp.MachineWallNotObserved,
		},
	}
	encoded, err := json.Marshal(projection)
	if err != nil {
		t.Fatal(err)
	}
	var projectionJSON map[string]any
	if err := json.Unmarshal(encoded, &projectionJSON); err != nil {
		t.Fatal(err)
	}
	mcp := map[string]any{"schema_version": 2.0, "ok": true, "action": "decision.inspect", "decision": map[string]any{"projection": projectionJSON}}
	if err := resolvedSchema(t, MCPOutputV2).Validate(mcp); err != nil {
		t.Fatalf("mcp rejected real DecisionProjection JSON %s: %v", encoded, err)
	}
	ipc := map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "projection-response", "action": "decision.inspect", "ok": true, "decision": map[string]any{"projection": projectionJSON}}
	if err := resolvedSchema(t, IPCV2).Validate(ipc); err != nil {
		t.Fatalf("ipc rejected real DecisionProjection JSON %s: %v", encoded, err)
	}
}

func TestDecisionProtocolMCPToolSchemaAdvertisesEnvelopeWithoutDoingSemanticValidation(t *testing.T) {
	data, err := Load(MCPToolInputV2)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	properties, ok := doc["properties"].(map[string]any)
	if !ok || properties["decision"] == nil || properties["experiment_id"] == nil {
		t.Fatalf("tool properties=%#v", properties)
	}
	action, _ := properties["action"].(map[string]any)
	description, _ := action["description"].(string)
	for _, want := range []string{"decision.policy.snapshot", "decision.selection.commit", "decision.authority.materialize"} {
		if !strings.Contains(description, want) {
			t.Fatalf("action description does not advertise %s: %q", want, description)
		}
	}
	permissive := map[string]any{"action": "decision.inspect", "decision": map[string]any{"episode_id": 7.0, "forged": true}}
	if err := resolvedSchema(t, MCPToolInputV2).Validate(permissive); err != nil {
		t.Fatalf("transport admission became semantic: %v", err)
	}
	if err := resolvedSchema(t, MCPInputV2).Validate(permissive); err == nil {
		t.Fatal("canonical decision schema accepted malformed decision payload")
	}
}
