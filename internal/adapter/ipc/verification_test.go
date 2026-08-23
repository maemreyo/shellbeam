package ipc

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	verificationapp "github.com/maemreyo/shellbeam/internal/app/verification"
	verificationcore "github.com/maemreyo/shellbeam/internal/core/verification"
)

type verificationActionsFixture struct {
	fakeActions
	inspectReq  verificationapp.InspectRequest
	previewReq  verificationapp.PreviewPolicyRequest
	activateReq verificationapp.ActivateRequest
	waiverReq   verificationapp.SetWaiverRequest
	revokeReq   verificationapp.RevokeWaiverRequest
}

func (f *verificationActionsFixture) InspectVerification(_ context.Context, req verificationapp.InspectRequest) (verificationapp.Inspection, error) {
	f.inspectReq = req
	return verificationapp.Inspection{SchemaVersion: 1, Phase: req.Phase, WorkspaceID: req.WorkspaceID, PolicyState: verificationapp.PolicyStateAbsent}, nil
}
func (f *verificationActionsFixture) PreviewVerificationPolicy(_ context.Context, req verificationapp.PreviewPolicyRequest) (verificationapp.PolicyPreview, error) {
	f.previewReq = req
	return verificationapp.PolicyPreview{State: verificationapp.PolicyLoadAbsent}, nil
}
func (f *verificationActionsFixture) ActivateVerificationPolicy(_ context.Context, req verificationapp.ActivateRequest) (verificationcore.ActivationWriteResult, error) {
	f.activateReq = req
	return verificationcore.ActivationWriteResult{Replayed: true}, nil
}
func (f *verificationActionsFixture) SetVerificationWaiver(_ context.Context, req verificationapp.SetWaiverRequest) (verificationcore.WaiverWriteResult, error) {
	f.waiverReq = req
	return verificationcore.WaiverWriteResult{Active: true}, nil
}
func (f *verificationActionsFixture) RevokeVerificationWaiver(_ context.Context, req verificationapp.RevokeWaiverRequest) (verificationcore.RevocationWriteResult, error) {
	f.revokeReq = req
	return verificationcore.RevocationWriteResult{Replayed: true}, nil
}

func TestVerificationV2FieldOwnershipRejectsCrossActionFields(t *testing.T) {
	valid := []string{
		`{"ipc_version":2,"kind":"request","request_id":"r","action":"inspect.verification","workspace_id":"ws_01K00000000000000000000000","phase":"checkpoint"}`,
		`{"ipc_version":2,"kind":"request","request_id":"r","action":"verification.policy.preview","workspace_id":"ws_01K00000000000000000000000","profile":"team"}`,
		`{"ipc_version":2,"kind":"request","request_id":"r","action":"verification.policy.activate","workspace_id":"ws_01K00000000000000000000000","activation_id":"act_one","proposed_policy_digest":"pol_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","expected_previous_policy_digest":"absent","proposal_generation":"gen_1111111111111111111111111111111111111111111111111111111111111111","authority":"explicit_caller","actor":"tester"}`,
		`{"ipc_version":2,"kind":"request","request_id":"r","action":"verification.waiver.set","workspace_id":"ws_01K00000000000000000000000","waiver_id":"wv_one","policy_digest":"pol_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","rule_id":"r1","phase":"checkpoint","generation":"gen_1111111111111111111111111111111111111111111111111111111111111111","authority":"explicit_caller","actor":"tester","reason":"CI only"}`,
		`{"ipc_version":2,"kind":"request","request_id":"r","action":"verification.waiver.revoke","workspace_id":"ws_01K00000000000000000000000","waiver_id":"wv_one","authority":"explicit_caller","actor":"tester"}`,
	}
	for _, raw := range valid {
		if _, err := decodeRequestV2(bytes.NewBufferString(raw)); err != nil {
			t.Fatalf("valid request rejected %s: %v", raw, err)
		}
	}
	bad := `{"ipc_version":2,"kind":"request","request_id":"r","action":"inspect.verification","workspace_id":"ws_01K00000000000000000000000","phase":"checkpoint","reason":"leak"}`
	if _, err := decodeRequestV2(bytes.NewBufferString(bad)); err == nil {
		t.Fatal("cross-action verification field accepted")
	}
}

func TestVerificationV2DispatchPreservesTypedRequests(t *testing.T) {
	a := &verificationActionsFixture{}
	s := &Server{actions: a}
	expires := time.Unix(200, 0).UTC()
	cases := []RequestV2{
		{Action: "inspect.verification", WorkspaceID: "ws_01K00000000000000000000000", ActivityID: "activity-1", VerificationRequestV2Fields: VerificationRequestV2Fields{Phase: verificationcore.PhaseCheckpoint}},
		{Action: "verification.policy.preview", WorkspaceID: "ws_01K00000000000000000000000", VerificationRequestV2Fields: VerificationRequestV2Fields{Profile: "team"}},
		{Action: "verification.policy.activate", WorkspaceID: "ws_01K00000000000000000000000", VerificationRequestV2Fields: VerificationRequestV2Fields{ActivationID: "act_one", ProposedPolicyDigest: "pol_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ExpectedPreviousPolicyDigest: "absent", ProposalGeneration: "gen_1111111111111111111111111111111111111111111111111111111111111111", Authority: "explicit_caller", Actor: "tester"}},
		{Action: "verification.waiver.set", WorkspaceID: "ws_01K00000000000000000000000", CheckpointID: "chk_01K00000000000000000000000", Reason: "CI only", VerificationRequestV2Fields: VerificationRequestV2Fields{WaiverID: "wv_one", PolicyDigest: "pol_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", RuleID: "r1", Phase: verificationcore.PhaseCheckpoint, Generation: "gen_1111111111111111111111111111111111111111111111111111111111111111", Authority: "explicit_caller", Actor: "tester", ExpiresAt: &expires, ExpiresPhase: verificationcore.PhasePreMerge}},
		{Action: "verification.waiver.revoke", WorkspaceID: "ws_01K00000000000000000000000", Reason: "CI only", VerificationRequestV2Fields: VerificationRequestV2Fields{WaiverID: "wv_one", Authority: "explicit_caller", Actor: "tester"}},
	}
	for _, req := range cases {
		resp := ResponseV2{}
		if err := s.verificationV2(context.Background(), req, &resp); err != nil {
			t.Fatalf("%s: %v", req.Action, err)
		}
	}
	if a.inspectReq.ActivityID != "activity-1" || a.inspectReq.Phase != verificationcore.PhaseCheckpoint {
		t.Fatalf("inspect=%#v", a.inspectReq)
	}
	if a.previewReq.Profile != "team" {
		t.Fatalf("preview=%#v", a.previewReq)
	}
	if a.activateReq.ActivationID != "act_one" || a.activateReq.ExpectedPreviousDigest != "absent" {
		t.Fatalf("activate=%#v", a.activateReq)
	}
	if !a.waiverReq.ExpiresAt.Equal(expires) || a.waiverReq.ExpiresPhase != verificationcore.PhasePreMerge || a.waiverReq.CheckpointID == "" {
		t.Fatalf("waiver=%#v", a.waiverReq)
	}
	if a.revokeReq.WaiverID != "wv_one" {
		t.Fatalf("revoke=%#v", a.revokeReq)
	}
}

func TestVerificationBridgeV2ConversionAndResponsePreservation(t *testing.T) {
	in := bridge.Request{ProtocolVersion: 2, Action: "verification.policy.activate", VerificationActivate: verificationapp.ActivateRequest{ActivationID: "act_one", WorkspaceID: "ws_01K00000000000000000000000", ProposedPolicyDigest: "pol_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ExpectedPreviousDigest: "absent", ProposalGeneration: "gen_1111111111111111111111111111111111111111111111111111111111111111", Authority: "explicit_caller", Actor: "tester"}}
	req := requestV2FromBridge(in)
	if req.ActivationID != "act_one" || req.ExpectedPreviousPolicyDigest != "absent" || req.WorkspaceID == "" {
		t.Fatalf("req=%#v", req)
	}
	activation := verificationcore.ActivationWriteResult{Replayed: true}
	resp := ResponseV2{VerificationResponseV2Fields: VerificationResponseV2Fields{VerificationActivation: &activation}}
	got := bridgeResponseFromV2(resp)
	if got.VerificationActivation == nil || !got.VerificationActivation.Replayed {
		t.Fatalf("got=%#v", got)
	}
}

func TestVerificationClearResponseDropsAllPayloads(t *testing.T) {
	resp := ResponseV2{VerificationResponseV2Fields: VerificationResponseV2Fields{Verification: &verificationapp.Inspection{}, VerificationPolicyPreview: &verificationapp.PolicyPreview{}, VerificationActivation: &verificationcore.ActivationWriteResult{}, VerificationWaiver: &verificationcore.WaiverWriteResult{}, VerificationRevocation: &verificationcore.RevocationWriteResult{}}}
	clearResponseV2Payload(&resp)
	if resp.Verification != nil || resp.VerificationPolicyPreview != nil || resp.VerificationActivation != nil || resp.VerificationWaiver != nil || resp.VerificationRevocation != nil {
		t.Fatalf("verification payload survived clear: %#v", resp)
	}
}

func TestVerificationIPCNonWaiverRequestOmitsZeroExpiresAt(t *testing.T) {
	request := requestV2FromBridge(bridge.Request{ProtocolVersion: 2, Action: "inspect.verification", VerificationInspect: verificationapp.InspectRequest{WorkspaceID: "ws_01K00000000000000000000000", Phase: verificationcore.PhaseCheckpoint}})
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte(`"expires_at"`)) {
		t.Fatalf("zero expires_at leaked into unrelated request: %s", encoded)
	}
}

func TestVerificationIPCWaiverExpiryRoundTripsOnlyWhenDeclared(t *testing.T) {
	expires := time.Unix(200, 0).UTC()
	request := requestV2FromBridge(bridge.Request{ProtocolVersion: 2, Action: "verification.waiver.set", VerificationWaiverSet: verificationapp.SetWaiverRequest{WaiverID: "wv_one", WorkspaceID: "ws_01K00000000000000000000000", PolicyDigest: "pol_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", RuleID: "r1", Phase: verificationcore.PhaseCheckpoint, Authority: "explicit_caller", Actor: "tester", ExpiresAt: expires}})
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`"expires_at"`)) {
		t.Fatalf("declared expires_at omitted: %s", encoded)
	}
}

func TestVerificationIPCV2ValidationMatchesClosedMCPContract(t *testing.T) {
	actor := strings.Repeat("a", 129)
	cases := []string{
		`{"ipc_version":2,"kind":"request","request_id":"r","action":"inspect.verification","workspace_id":"ws_01K00000000000000000000000","activity_id":"../bad","phase":"checkpoint"}`,
		`{"ipc_version":2,"kind":"request","request_id":"r","action":"verification.policy.activate","workspace_id":"ws_01K00000000000000000000000","activation_id":"act_one","proposed_policy_digest":"pol_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","expected_previous_policy_digest":"garbage","proposal_generation":"gen_1111111111111111111111111111111111111111111111111111111111111111","authority":"explicit_caller","actor":"tester"}`,
		`{"ipc_version":2,"kind":"request","request_id":"r","action":"verification.waiver.set","workspace_id":"ws_01K00000000000000000000000","waiver_id":"wv_one","policy_digest":"pol_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","rule_id":"r1","phase":"checkpoint","checkpoint_id":"bad-checkpoint","authority":"explicit_caller","actor":"tester","reason":"CI only"}`,
		`{"ipc_version":2,"kind":"request","request_id":"r","action":"verification.waiver.revoke","workspace_id":"ws_01K00000000000000000000000","waiver_id":"wv_one","authority":"explicit_caller","actor":"` + actor + `"}`,
	}
	for _, raw := range cases {
		if _, err := decodeRequestV2(bytes.NewBufferString(raw)); err == nil {
			t.Fatalf("invalid verification request accepted: %s", raw)
		}
	}
}
