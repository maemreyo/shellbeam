package mcp

import (
	"testing"

	evidence "github.com/maemreyo/shellbeam/internal/core/evidence"
)

func TestVerificationAttemptMCPV2StartMapsAndValidates(t *testing.T) {
	raw := []byte(`{"action":"start","operation_id":"attempt-mcp","workspace_id":"ws_01K00000000000000000000000","command":"true","cwd":".","evidence":{"verification_kind":"test","source_scope":"full"},"verification_attempt":{"rerun_of_evidence_id":"ev_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","rerun_reason":"diagnose_flake"}}`)
	in, err := decodeInput(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateForVersion(2, in, raw); err != nil {
		t.Fatal(err)
	}
	req := requestFromInput(2, in, raw)
	if req.Start.VerificationAttempt == nil || req.Start.VerificationAttempt.RerunReason != evidence.RerunDiagnoseFlake {
		t.Fatalf("mapped attempt=%#v", req.Start.VerificationAttempt)
	}

	bad := []byte(`{"action":"start","operation_id":"attempt-mcp","workspace_id":"ws_01K00000000000000000000000","command":"true","cwd":".","evidence":{"verification_kind":"test","source_scope":"full"},"verification_attempt":{"rerun_reason":"diagnose_flake"}}`)
	invalid, err := decodeInput(bad)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateForVersion(2, invalid, bad); err == nil {
		t.Fatal("invalid MCP verification attempt accepted")
	}
	if err := validateForVersion(1, in, raw); err == nil {
		t.Fatal("legacy MCP accepted verification_attempt")
	}
}
