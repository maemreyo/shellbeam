package mcp

import (
	"context"
	"encoding/json"
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
