//go:build linux || darwin

package ipc

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	dp "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
	"github.com/maemreyo/shellbeam/internal/core/failure"
)

func decisionMinimumPayloads(t *testing.T) map[string]map[string]any {
	t.Helper()
	policy := dp.PolicyContent{PolicyID: "policy-transport", EpisodeKinds: []dp.EpisodeKind{dp.EpisodeDiagnosis}, OverridePolicy: dp.OverridePolicy{Allowed: false}}
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

func decodeDecisionPayload(t *testing.T, action string, decision map[string]any) (RequestV2, error) {
	t.Helper()
	payload := map[string]any{"ipc_version": 2, "kind": "request", "request_id": "req-decision", "action": action, "decision": decision}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return decodeRequestV2(bytes.NewReader(b))
}

func TestDecisionProtocolBridgeRequestPreservesOuterWorkspaceSelector(t *testing.T) {
	workspaceID := "ws_01K00000000000000000000000"
	decision := &bridge.DecisionRequest{EpisodeID: "episode-transport"}
	got := requestV2FromBridge(bridge.Request{Action: "decision.inspect", WorkspaceID: workspaceID, Decision: decision})
	if got.WorkspaceID != workspaceID {
		t.Fatalf("workspace selector lost: got=%q want=%q request=%#v", got.WorkspaceID, workspaceID, got)
	}
	if got.Decision == nil || got.Decision.EpisodeID != "episode-transport" {
		t.Fatalf("decision payload lost: %#v", got.Decision)
	}
}

func TestDecisionProtocolIPCAcceptsOuterWorkspaceSelector(t *testing.T) {
	workspaceID := "ws_01K00000000000000000000000"
	wire := map[string]any{
		"ipc_version":  2,
		"kind":         "request",
		"request_id":   "decision-workspace",
		"action":       "decision.inspect",
		"workspace_id": workspaceID,
		"decision":     decisionMinimumPayloads(t)["decision.inspect"],
	}
	encoded, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeRequestV2(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("outer workspace selector rejected: %v", err)
	}
	if got.WorkspaceID != workspaceID {
		t.Fatalf("workspace selector lost after decode: got=%q want=%q", got.WorkspaceID, workspaceID)
	}
}

func TestDecisionProtocolIPCRejectsInvalidOuterWorkspaceSelector(t *testing.T) {
	wire := map[string]any{
		"ipc_version":  2,
		"kind":         "request",
		"request_id":   "decision-workspace-invalid",
		"action":       "decision.inspect",
		"workspace_id": "not-a-workspace-id",
		"decision":     decisionMinimumPayloads(t)["decision.inspect"],
	}
	encoded, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	_, err = decodeRequestV2(bytes.NewReader(encoded))
	if err == nil {
		t.Fatal("invalid outer workspace selector accepted")
	}
	public := failure.Public(err)
	if public.Code != failure.InvalidInput || public.Details["field"] != "workspace_id" {
		t.Fatalf("public failure=%#v", public)
	}
}

func TestDecisionProtocolIPCKeepsOuterFieldSetClosed(t *testing.T) {
	wire := map[string]any{
		"ipc_version": 2,
		"kind":        "request",
		"request_id":  "decision-unrelated-field",
		"action":      "decision.inspect",
		"decision":    decisionMinimumPayloads(t)["decision.inspect"],
		"cwd":         "/tmp",
	}
	encoded, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeRequestV2(bytes.NewReader(encoded)); err == nil {
		t.Fatal("unrelated outer field accepted")
	}
}

func TestDecisionActionsStrictMinimumPayloads(t *testing.T) {
	payloads := decisionMinimumPayloads(t)
	if len(payloads) != 18 {
		t.Fatalf("actions=%d", len(payloads))
	}
	for action, decision := range payloads {
		t.Run(action, func(t *testing.T) {
			req, err := decodeDecisionPayload(t, action, decision)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if req.Decision == nil {
				t.Fatal("decision payload lost")
			}
		})
	}
}

func TestDecisionActionsRejectEveryRequiredFieldOmission(t *testing.T) {
	for action, decision := range decisionMinimumPayloads(t) {
		t.Run(action, func(t *testing.T) {
			for field := range decision {
				clone := map[string]any{}
				for k, v := range decision {
					clone[k] = v
				}
				delete(clone, field)
				if _, err := decodeDecisionPayload(t, action, clone); err == nil {
					t.Fatalf("missing %s accepted", field)
				}
			}
		})
	}
}

func TestDecisionActionRejectsCrossActionFieldsByPresence(t *testing.T) {
	cases := []struct {
		action, field string
		value         any
	}{{"decision.inspect", "actor_ref", ""}, {"decision.inspect", "workspace_id", "ws_01K00000000000000000000000"}, {"decision.inspect", "repository_id", "repo_01K00000000000000000000000"}, {"decision.override.create", "actor_ref", "forged"}, {"decision.authority.materialize", "actor_ref", "forged"}, {"decision.evaluate", "reason", "x"}}
	payloads := decisionMinimumPayloads(t)
	for _, tc := range cases {
		t.Run(tc.action+"_"+tc.field, func(t *testing.T) {
			clone := map[string]any{}
			for k, v := range payloads[tc.action] {
				clone[k] = v
			}
			clone[tc.field] = tc.value
			if _, err := decodeDecisionPayload(t, tc.action, clone); err == nil {
				t.Fatalf("cross-action field %s accepted", tc.field)
			}
		})
	}
}

func TestDecisionAssessmentInputRejectsQualifiedContextFields(t *testing.T) {
	p := decisionMinimumPayloads(t)["decision.assessment.record"]
	a := p["assessment"].(map[string]any)
	a["qualified_context_class"] = "HUMAN"
	if _, err := decodeDecisionPayload(t, "decision.assessment.record", p); err == nil {
		t.Fatal("qualified context accepted")
	}
}
func TestDecisionAuthorityInputRejectsCallerAttestationBody(t *testing.T) {
	p := decisionMinimumPayloads(t)["decision.authority.materialize"]
	r := p["authority_request"].(map[string]any)
	r["attestation_id"] = "forged"
	if _, err := decodeDecisionPayload(t, "decision.authority.materialize", p); err == nil {
		t.Fatal("caller attestation body accepted")
	}
}
func TestDecisionAuthorityMaterializeRejectsCallerActorRef(t *testing.T) {
	p := decisionMinimumPayloads(t)["decision.authority.materialize"]
	p["actor_ref"] = "forged"
	if _, err := decodeDecisionPayload(t, "decision.authority.materialize", p); err == nil {
		t.Fatal("caller actor accepted")
	}
}
func TestDecisionObservationResultsHaveNoCallerInputField(t *testing.T) {
	p := decisionMinimumPayloads(t)["decision.experiment.close"]
	p["prediction_results"] = []any{}
	if _, err := decodeDecisionPayload(t, "decision.experiment.close", p); err == nil {
		t.Fatal("caller observation result accepted")
	}
}
func TestDecisionPolicySnapshotInputContainsContentOnly(t *testing.T) {
	p := decisionMinimumPayloads(t)["decision.policy.snapshot"]
	policy := p["policy"].(map[string]any)
	policy["repository_id"] = "forged"
	if _, err := decodeDecisionPayload(t, "decision.policy.snapshot", p); err == nil {
		t.Fatal("caller repository id accepted in policy snapshot")
	}
}

func TestDecisionPolicyActivateRequiresGenerationDigestAndPreviousSentinelOrDigest(t *testing.T) {
	base := decisionMinimumPayloads(t)["decision.policy.activate"]
	for _, bad := range []struct {
		field string
		value any
	}{{"proposal_generation", "1"}, {"expected_previous_policy_digest", ""}, {"expected_previous_policy_digest", "pol_bad"}} {
		clone := map[string]any{}
		for k, v := range base {
			clone[k] = v
		}
		clone[bad.field] = bad.value
		if _, err := decodeDecisionPayload(t, "decision.policy.activate", clone); err == nil {
			t.Fatalf("bad %s accepted", bad.field)
		}
	}
	clone := map[string]any{}
	for k, v := range base {
		clone[k] = v
	}
	clone["expected_previous_policy_digest"] = "pol_" + strings.Repeat("e", 64)
	if _, err := decodeDecisionPayload(t, "decision.policy.activate", clone); err != nil {
		t.Fatalf("digest predecessor rejected: %v", err)
	}
}

func TestCloseUnresolvedExplicitEmptyDimensionsSurviveDecode(t *testing.T) {
	req, err := decodeDecisionPayload(t, "decision.close_unresolved", decisionMinimumPayloads(t)["decision.close_unresolved"])
	if err != nil {
		t.Fatal(err)
	}
	if req.Decision.UnresolvedDimensions == nil || len(*req.Decision.UnresolvedDimensions) != 0 {
		t.Fatalf("dimensions=%#v", req.Decision.UnresolvedDimensions)
	}
}

type decisionStartCaptureActions struct {
	fakeActions
	lastStart app.StartRequest
}

func (a *decisionStartCaptureActions) Start(_ context.Context, req app.StartRequest) (app.View, error) {
	a.lastStart = req
	return app.View{SessionID: "s"}, nil
}

func TestStartForwardsExperimentIDToDaemonAdmission(t *testing.T) {
	runtime, err := os.MkdirTemp("/tmp", "shellbeam-decision-start-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtime) })
	actions := &decisionStartCaptureActions{}
	srv, err := Listen(runtime, actions)
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") || strings.Contains(err.Error(), "permission denied") {
			t.Skip("sandbox blocks Unix sockets")
		}
		t.Fatal(err)
	}
	defer srv.Close()
	go srv.Serve()
	client := NewClient(srv.SocketPath())
	_, err = client.CallV2(context.Background(), RequestV2{IPVersion: 2, Kind: "request", RequestID: "decision-start-experiment", Action: "start", OperationID: "op-decision-start", ExperimentID: "experiment-forwarded", Command: "true", CWD: "/tmp"})
	if err != nil {
		t.Fatal(err)
	}
	if actions.lastStart.ExperimentID != "experiment-forwarded" {
		t.Fatalf("start experiment_id=%q want=%q", actions.lastStart.ExperimentID, "experiment-forwarded")
	}
}

func TestStartAcceptsOptionalExperimentIDAndPollRejectsIt(t *testing.T) {
	start := map[string]any{"ipc_version": 2, "kind": "request", "request_id": "r", "action": "start", "operation_id": "op-start-experiment", "command": "true", "cwd": "/tmp", "experiment_id": "experiment-transport"}
	b, _ := json.Marshal(start)
	if _, err := decodeRequestV2(bytes.NewReader(b)); err != nil {
		t.Fatalf("start experiment id rejected: %v", err)
	}
	poll := map[string]any{"ipc_version": 2, "kind": "request", "request_id": "r", "action": "poll", "session_id": "sess", "cursor": 0, "experiment_id": "experiment-transport"}
	b, _ = json.Marshal(poll)
	if _, err := decodeRequestV2(bytes.NewReader(b)); err == nil {
		t.Fatal("poll accepted experiment_id")
	}
}

type decisionTrustedPeerActions struct {
	fakeActions
	uid         uint32
	ok          bool
	workspaceID string
}

func (a *decisionTrustedPeerActions) DecisionProtocol(ctx context.Context, _, workspaceID string, _ DecisionRequestV1) (DecisionResponseV1, error) {
	a.uid, a.ok = TrustedPeerUID(ctx)
	a.workspaceID = workspaceID
	return DecisionResponseV1{}, nil
}

func TestDecisionProtocolRequestContextCarriesAuthenticatedPeerUID(t *testing.T) {
	runtime, err := os.MkdirTemp("/tmp", "shellbeam-decision-peer-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtime) })
	actions := &decisionTrustedPeerActions{}
	srv, err := Listen(runtime, actions)
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") || strings.Contains(err.Error(), "permission denied") {
			t.Skip("sandbox blocks Unix sockets")
		}
		t.Fatal(err)
	}
	defer srv.Close()
	go srv.Serve()
	client := NewClient(srv.SocketPath())
	_, err = client.CallV2(context.Background(), RequestV2{IPVersion: 2, Kind: "request", RequestID: "decision-peer", Action: "decision.inspect", WorkspaceID: "ws_01K00000000000000000000000", Decision: &DecisionRequestV1{EpisodeID: "episode-transport"}})
	if err != nil {
		t.Fatal(err)
	}
	if !actions.ok || actions.uid != uint32(os.Getuid()) {
		t.Fatalf("trusted peer uid=%d ok=%v want=%d", actions.uid, actions.ok, os.Getuid())
	}
	if actions.workspaceID != "ws_01K00000000000000000000000" {
		t.Fatalf("workspace selector=%q", actions.workspaceID)
	}
}

func TestDecisionProtocolSecurityRejectsUnrelatedExecutionEvidenceFields(t *testing.T) {
	for _, field := range []string{"evidence", "verification_attempt"} {
		t.Run(field, func(t *testing.T) {
			wire := map[string]any{
				"ipc_version": 2,
				"kind":        "request",
				"request_id":  "decision-security-" + field,
				"action":      "decision.inspect",
				"decision":    decisionMinimumPayloads(t)["decision.inspect"],
				field:         map[string]any{},
			}
			encoded, err := json.Marshal(wire)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decodeRequestV2(bytes.NewReader(encoded)); err == nil {
				t.Fatalf("decision action accepted unrelated top-level %s", field)
			}
		})
	}
}
