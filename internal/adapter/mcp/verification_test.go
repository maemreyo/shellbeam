package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	verificationapp "github.com/maemreyo/shellbeam/internal/app/verification"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	verificationcore "github.com/maemreyo/shellbeam/internal/core/verification"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestVerificationMCPStrictFieldsAndTypedBridgeConversion(t *testing.T) {
	raw := []byte(`{"action":"inspect.verification","workspace_id":"ws_01K00000000000000000000000","phase":"checkpoint","activity_id":"activity-1"}`)
	in, err := decodeInputV2(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateForVersion(2, in, raw); err != nil {
		t.Fatal(err)
	}
	req := requestFromInput(2, in, raw)
	if req.VerificationInspect.WorkspaceID != "ws_01K00000000000000000000000" || req.VerificationInspect.Phase != verificationcore.PhaseCheckpoint || req.VerificationInspect.ActivityID != "activity-1" {
		t.Fatalf("req=%#v", req.VerificationInspect)
	}
	bad := []byte(`{"action":"inspect.verification","workspace_id":"ws_01K00000000000000000000000","phase":"checkpoint","reason":"leak"}`)
	if in, err := decodeInputV2(bad); err == nil {
		if err := validateForVersion(2, in, bad); err == nil {
			t.Fatal("cross-action field accepted")
		}
	}
}

func TestVerificationMCPSuccessPayloadsAreTyped(t *testing.T) {
	cases := []struct {
		action, key string
		out         bridge.Response
	}{
		{"inspect.verification", "verification", bridge.Response{Verification: &verificationapp.Inspection{SchemaVersion: 1, Phase: verificationcore.PhaseCheckpoint, PolicyState: verificationapp.PolicyStateAbsent}}},
		{"verification.policy.preview", "verification_policy_preview", bridge.Response{VerificationPolicyPreview: &verificationapp.PolicyPreview{State: verificationapp.PolicyLoadAbsent}}},
		{"verification.policy.activate", "verification_activation", bridge.Response{VerificationActivation: &verificationcore.ActivationWriteResult{Replayed: true}}},
		{"verification.waiver.set", "verification_waiver", bridge.Response{VerificationWaiver: &verificationcore.WaiverWriteResult{Active: true}}},
		{"verification.waiver.revoke", "verification_revocation", bridge.Response{VerificationRevocation: &verificationcore.RevocationWriteResult{Replayed: true}}},
	}
	for _, tc := range cases {
		t.Run(tc.action, func(t *testing.T) {
			result := successV2(input{Action: tc.action}, tc.out)
			if result.IsError {
				t.Fatalf("error result %#v", result)
			}
			body := result.StructuredContent.(map[string]any)
			if body[tc.key] == nil {
				b, _ := json.Marshal(body)
				t.Fatalf("missing %s in %s", tc.key, b)
			}
		})
	}
}

func TestLegacyCatalogOmitsVerificationSemantics(t *testing.T) {
	support := capability.VerificationSemanticsSupport{SchemaVersions: []int{1}, PolicySchemaVersions: []int{1}, MaxDomains: 16, MaxRelations: 512, MaxObligations: 256, MaxPolicyGaps: 128, MaxPolicyRules: 128, MaxClassifications: 128, MaxEvidenceRequirementsPerRule: 32}
	modern := capability.Baseline(capability.Limits{}).WithVerificationSemantics(support)
	legacy := legacyCatalogView(modern)
	if legacy.VerificationSemantics != nil {
		t.Fatalf("legacy leaked support %#v", legacy.VerificationSemantics)
	}
	if _, ok := legacy.Features[capability.FeatureVerificationSemantics]; ok {
		t.Fatalf("legacy leaked verification feature %#v", legacy.Features)
	}
}

func TestVerificationKeepsExactlyOneLocalShellTool(t *testing.T) {
	catalog := capability.Baseline(capability.Limits{}).WithVerificationSemantics(capability.VerificationSemanticsSupport{SchemaVersions: []int{1}, PolicySchemaVersions: []int{1}, MaxDomains: 16, MaxRelations: 512, MaxObligations: 256, MaxPolicyGaps: 128, MaxPolicyRules: 128, MaxClassifications: 128, MaxEvidenceRequirementsPerRule: 32})
	server := New(bridge.New(&fakeClient{}), catalog)
	st, ct := mcpgo.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go server.Run(ctx, st)
	client := mcpgo.NewClient(&mcpgo.Implementation{Name: "verification-one-tool", Version: "1"}, nil)
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 1 || tools.Tools[0].Name != "local_shell" {
		t.Fatalf("tools=%#v", tools.Tools)
	}
}

func TestVerificationInspectionV2StructuredContentPreservesTruthWithoutCompletionClaim(t *testing.T) {
	oblFailed := "obl_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	oblNotTriggered := "obl_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	evFail := "ev_cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	evPass := "ev_dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	policy := "pol_eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	evaluationID, err := verificationcore.EvaluationID(verificationcore.EvaluationIdentityInput{PolicyDigest: policy, RuleID: "race", ObligationID: oblFailed, RequirementID: "race", EvidenceRefs: []string{evFail, evPass}})
	if err != nil {
		t.Fatal(err)
	}
	unavailable := verificationcore.CostMetric{Quality: verificationcore.CostQualityUnavailable}
	inspection := verificationapp.Inspection{
		SchemaVersion: 2, Phase: verificationcore.PhaseCheckpoint,
		RepositoryID: "repo_01K00000000000000000000000", WorkspaceID: "ws_01K00000000000000000000000",
		SourceGeneration: "gen_1111111111111111111111111111111111111111111111111111111111111111",
		PolicyState:      verificationapp.PolicyStateEffective,
		Gate:             verificationcore.GateEvaluation{Status: verificationcore.GateClear, Breakdown: verificationcore.GateBreakdown{Waived: 1}},
		Obligations:      []verificationcore.VerificationObligation{},
		ObligationViews: []verificationapp.ObligationView{
			{ObligationID: oblNotTriggered, SourceRuleID: "browser", Disposition: verificationcore.DispositionNotTriggered, EvidenceStatus: verificationcore.EvidenceNotEvaluated, SufficiencyBasis: "browser policy considered but not triggered", RequirementResults: []verificationcore.RequirementEvaluation{}},
			{ObligationID: oblFailed, SourceRuleID: "race", Disposition: verificationcore.DispositionWaived, EvidenceStatus: verificationcore.EvidenceInconsistent, SufficiencyBasis: "race evidence", WaiverID: "wv_ci_only", EvidenceRefs: []string{evFail, evPass}, ReasonCodes: []string{"contradictory_evidence", "evidence_stale"}, RequirementResults: []verificationcore.RequirementEvaluation{{EvaluationID: evaluationID, PolicyDigest: policy, RuleID: "race", ObligationID: oblFailed, RequirementID: "race", Status: verificationcore.EvidenceInconsistent, EvidenceRefs: []string{evFail, evPass}, ReasonCode: "contradictory_evidence"}}},
		},
		PolicyGaps:  []verificationcore.PolicyGap{{GapID: "gap_ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", SurfaceRef: "internal/auth", DeclaredClass: "security_sensitive", ClassificationSource: "security", MissingPolicyClass: "security_sensitive", Authority: verificationcore.AuthorityMechanical, ProvenanceRefs: []string{"policy:" + policy}}},
		CostSummary: []verificationcore.BoundRequirementCost{{ObligationID: oblFailed, RequirementID: "race", ProviderClass: verificationcore.ProviderIntegrationTest, Cost: verificationcore.VerificationCost{WallMS: unavailable, OutputBytes: unavailable, CPUUserMS: unavailable, CPUSystemMS: unavailable, MaxRSSBytes: unavailable, ProcessPeak: unavailable, ProviderCost: unavailable, ModelCost: unavailable}}},
	}
	result := successV2(input{Action: "inspect.verification"}, bridge.Response{Verification: &inspection})
	if result.IsError {
		t.Fatalf("unexpected error result %#v", result)
	}
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, required := range []string{"not_triggered", "waived", "inconsistent", evFail, evPass, "evidence_stale", "unavailable", "policy_gaps", `"status":"clear"`} {
		if !strings.Contains(text, required) {
			t.Fatalf("structured verification output missing %q: %s", required, text)
		}
	}
	for _, forbidden := range []string{"task_complete", "work_complete", "safe_to_finish"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("structured verification output leaked completion truth %q: %s", forbidden, text)
		}
	}
}
