package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	dp "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"
)

type decisionMCPClient struct {
	last   bridge.Request
	starts int
}

func (c *decisionMCPClient) Forward(_ context.Context, req bridge.Request) (bridge.Response, error) {
	c.last = req
	if req.Action == "start" {
		c.starts++
	}
	projection := dp.DecisionProjection{EpisodeID: dp.EpisodeID("episode-transport")}
	return bridge.Response{Decision: &bridge.DecisionResponse{Projection: &projection}}, nil
}

func decisionMCPMinimumPayloads() map[string]map[string]any {
	policy := map[string]any{
		"policy_id":     "policy-transport",
		"episode_kinds": []string{"DIAGNOSIS"},
		"override_policy": map[string]any{
			"allowed": false,
		},
	}
	return map[string]map[string]any{
		"decision.policy.snapshot":       {"policy": map[string]any{"content": policy}},
		"decision.policy.activate":       {"activation_id": "activate-transport", "policy_digest": "pol_" + strings.Repeat("a", 64), "proposal_generation": "gen_" + strings.Repeat("b", 64), "expected_previous_policy_digest": "absent", "actor_ref": "actor"},
		"decision.create":                {"episode_id": "episode-transport", "episode_kind": "DIAGNOSIS", "actor_ref": "actor"},
		"decision.inspect":               {"episode_id": "episode-transport"},
		"decision.evaluate":              {"episode_id": "episode-transport", "candidate_id": "candidate-transport"},
		"decision.close_unresolved":      {"episode_id": "episode-transport", "actor_ref": "actor", "expected_projection_digest": "proj_" + strings.Repeat("c", 64), "reason": "unresolved", "unresolved_dimensions": []string{}},
		"decision.candidate.create":      {"episode_id": "episode-transport", "candidate": map[string]any{"candidate_id": "candidate-transport", "semantic_claim": "A"}, "actor_ref": "actor"},
		"decision.candidate.revise":      {"episode_id": "episode-transport", "parent_candidate_id": "candidate-parent", "candidate": map[string]any{"candidate_id": "candidate-child", "semantic_claim": "B"}, "actor_ref": "actor"},
		"decision.experiment.define":     {"episode_id": "episode-transport", "experiment_id": "experiment-transport", "actor_ref": "actor"},
		"decision.prediction.bind":       {"episode_id": "episode-transport", "experiment_id": "experiment-transport", "prediction": map[string]any{"prediction_id": "prediction-transport", "candidate_id": "candidate-transport", "role": "REQUIRED_PREDICTION", "predicate": map[string]any{"kind": "OPERATION_OUTCOME", "operation_outcome": map[string]any{"expected_outcome": "SUCCESS"}}}},
		"decision.experiment.seal":       {"experiment_id": "experiment-transport", "actor_ref": "actor"},
		"decision.experiment.close":      {"experiment_id": "experiment-transport", "actor_ref": "actor"},
		"decision.experiment.abort":      {"experiment_id": "experiment-transport", "abort_phase": "BEFORE_EXECUTION", "actor_ref": "actor", "reason": "stop"},
		"decision.assessment.record":     {"episode_id": "episode-transport", "assessment": map[string]any{"assessment_id": "assessment-transport", "declared_context_class": "SAME_CONTEXT", "preferred_candidates": []string{"candidate-transport"}}, "actor_ref": "actor"},
		"decision.selection.propose":     {"episode_id": "episode-transport", "candidate_id": "candidate-transport", "actor_ref": "actor"},
		"decision.override.create":       {"episode_id": "episode-transport", "candidate_id": "candidate-transport", "expected_policy_digest": "pol_" + strings.Repeat("a", 64), "expected_projection_digest": "proj_" + strings.Repeat("c", 64), "blocking_requirement_digest": "block_" + strings.Repeat("d", 64), "authority_attestation_ref": "attestation-transport", "reason": "override"},
		"decision.selection.commit":      {"episode_id": "episode-transport", "candidate_id": "candidate-transport", "actor_ref": "actor", "expected_policy_digest": "pol_" + strings.Repeat("a", 64), "expected_projection_digest": "proj_" + strings.Repeat("c", 64), "idempotency_key": "idem-transport"},
		"decision.authority.materialize": {"authority_request": map[string]any{"required_authority_class": map[string]any{"domain": "shellbeam", "class_id": "explicit_caller", "version": 1}, "required_scope": map[string]any{"repository_id": "repo-transport", "episode_id": "episode-transport", "action_kind": "COMMIT_SELECTION_OVERRIDE"}}},
	}
}

func callDecisionMCP(t *testing.T, session *mcpgo.ClientSession, action string, decision map[string]any) *mcpgo.CallToolResult {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"action": action, "decision": decision})
	if err != nil {
		t.Fatal(err)
	}
	res, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(payload)})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestDecisionProtocolMCPAcceptsAndForwardsOuterWorkspaceSelector(t *testing.T) {
	client := &decisionMCPClient{}
	session, closeSession := currentSession(t, New(bridge.New(client), capability.Baseline(capability.Limits{})))
	defer closeSession()
	workspaceID := "ws_01K00000000000000000000000"
	payload, err := json.Marshal(map[string]any{
		"action":       "decision.policy.snapshot",
		"workspace_id": workspaceID,
		"decision":     decisionMCPMinimumPayloads()["decision.policy.snapshot"],
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(payload)})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("outer workspace selector rejected: %#v", res)
	}
	if client.last.WorkspaceID != workspaceID {
		t.Fatalf("workspace selector lost: got=%q want=%q request=%#v", client.last.WorkspaceID, workspaceID, client.last)
	}
	if client.last.Decision == nil {
		t.Fatal("decision payload was not forwarded")
	}
}

func TestDecisionProtocolMCPRejectsInvalidOuterWorkspaceSelector(t *testing.T) {
	client := &decisionMCPClient{}
	session, closeSession := currentSession(t, New(bridge.New(client), capability.Baseline(capability.Limits{})))
	defer closeSession()
	payload, err := json.Marshal(map[string]any{
		"action":       "decision.inspect",
		"workspace_id": "not-a-workspace-id",
		"decision":     decisionMCPMinimumPayloads()["decision.inspect"],
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(payload)})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatalf("invalid outer workspace selector accepted: %#v", res)
	}
	if client.last.Action != "" {
		t.Fatalf("invalid selector reached bridge: %#v", client.last)
	}
}

func TestDecisionProtocolMCPKeepsOuterFieldSetClosed(t *testing.T) {
	client := &decisionMCPClient{}
	session, closeSession := currentSession(t, New(bridge.New(client), capability.Baseline(capability.Limits{})))
	defer closeSession()
	payload, err := json.Marshal(map[string]any{
		"action":   "decision.inspect",
		"decision": decisionMCPMinimumPayloads()["decision.inspect"],
		"cwd":      "/tmp",
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(payload)})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatalf("unrelated outer field accepted: %#v", res)
	}
	if client.last.Action != "" {
		t.Fatalf("invalid outer field reached bridge: %#v", client.last)
	}
}

func TestDecisionProtocolMCPForwardsAllActionsWithoutStart(t *testing.T) {
	client := &decisionMCPClient{}
	session, closeSession := currentSession(t, New(bridge.New(client), capability.Baseline(capability.Limits{})))
	defer closeSession()
	payloads := decisionMCPMinimumPayloads()
	if len(payloads) != 18 {
		t.Fatalf("actions=%d", len(payloads))
	}
	for action, decision := range payloads {
		t.Run(action, func(t *testing.T) {
			res := callDecisionMCP(t, session, action, decision)
			if res.IsError || client.starts != 0 || client.last.Action != action || client.last.Decision == nil {
				t.Fatalf("res=%#v starts=%d last=%#v", res, client.starts, client.last)
			}
			body, ok := res.StructuredContent.(map[string]any)
			if !ok || body["decision"] == nil {
				t.Fatalf("body=%#v", res.StructuredContent)
			}
		})
	}
}

func TestDecisionProtocolMCPRejectsCrossActionAndServerOwnedFields(t *testing.T) {
	client := &decisionMCPClient{}
	session, closeSession := currentSession(t, New(bridge.New(client), capability.Baseline(capability.Limits{})))
	defer closeSession()
	cases := []struct {
		name, action, field string
		value               any
	}{
		{"authority actor", "decision.authority.materialize", "actor_ref", "forged"},
		{"override actor", "decision.override.create", "actor_ref", "forged"},
		{"observation result", "decision.experiment.close", "prediction_results", []any{}},
		{"nested workspace", "decision.inspect", "workspace_id", "ws_01K00000000000000000000000"},
		{"nested repository", "decision.inspect", "repository_id", "repo_01K00000000000000000000000"},
		{"inspect reason", "decision.inspect", "reason", "forged"},
	}
	payloads := decisionMCPMinimumPayloads()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decision := map[string]any{}
			for k, v := range payloads[tc.action] {
				decision[k] = v
			}
			decision[tc.field] = tc.value
			res := callDecisionMCP(t, session, tc.action, decision)
			if !res.IsError {
				t.Fatalf("field %s accepted: %#v", tc.field, res)
			}
		})
	}
	if client.starts != 0 {
		t.Fatalf("invalid decision inputs spawned %d starts", client.starts)
	}
}

func TestDecisionProtocolMCPStartAcceptsExperimentIDAndPollRejectsIt(t *testing.T) {
	client := &decisionMCPClient{}
	// The fake is intentionally not a valid execution responder; this test only
	// requires the accepted start envelope to reach the bridge with its binding.
	raw := []byte(`{"action":"start","operation_id":"op-mcp-exp","command":"true","cwd":"/tmp","experiment_id":"experiment-transport"}`)
	in, err := decodeInputV2(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateForVersion(2, in, raw); err != nil {
		t.Fatalf("start experiment_id rejected: %v", err)
	}
	req := requestFromInput(2, in, raw)
	if req.Start.ExperimentID != "experiment-transport" {
		t.Fatalf("experiment_id lost: %#v", req.Start)
	}
	poll := []byte(`{"action":"poll","session_id":"sess","cursor":0,"experiment_id":"experiment-transport"}`)
	pin, err := decodeInputV2(poll)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateForVersion(2, pin, poll); err == nil {
		t.Fatal("poll accepted experiment_id")
	}
	_ = client
}
