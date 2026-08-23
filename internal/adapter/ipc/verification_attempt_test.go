package ipc

import (
	"testing"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	daemon "github.com/maemreyo/shellbeam/internal/app/daemon"
	evidence "github.com/maemreyo/shellbeam/internal/core/evidence"
)

func ipcAttempt() *evidence.VerificationAttemptIntent {
	return &evidence.VerificationAttemptIntent{RerunOfEvidenceID: "ev_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", RerunReason: evidence.RerunDiagnoseFlake}
}

func TestVerificationAttemptIPCV2BridgeMappingClonesAttempt(t *testing.T) {
	attempt := ipcAttempt()
	contract := &evidence.Contract{VerificationKind: evidence.VerificationTest, SourceScope: evidence.SourceScopeFull}
	wire := requestV2FromBridge(bridge.Request{ProtocolVersion: 2, Action: "start", Start: daemon.StartRequest{
		OperationID: "attempt-ipc", WorkspaceID: "ws_01K00000000000000000000000", CWD: ".", Command: "true", Evidence: contract, VerificationAttempt: attempt,
	}})
	if wire.VerificationAttempt == nil || wire.VerificationAttempt.RerunReason != evidence.RerunDiagnoseFlake {
		t.Fatalf("wire attempt=%#v", wire.VerificationAttempt)
	}
	wire.VerificationAttempt.RerunReason = evidence.RerunFlakeQualification
	if attempt.RerunReason != evidence.RerunDiagnoseFlake {
		t.Fatal("IPC mapping aliased caller verification attempt")
	}
}

func TestVerificationAttemptIPCV2DecodeValidatesStartOnly(t *testing.T) {
	valid := `{"ipc_version":2,"kind":"request","request_id":"a","action":"start","operation_id":"attempt-ipc","workspace_id":"ws_01K00000000000000000000000","command":"true","cwd":".","evidence":{"verification_kind":"test","source_scope":"full"},"verification_attempt":{"rerun_of_evidence_id":"ev_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","rerun_reason":"diagnose_flake"}}`
	got, err := decodeRequestV2(bytesReaderV2([]byte(valid)))
	if err != nil || got.VerificationAttempt == nil || got.VerificationAttempt.RerunReason != evidence.RerunDiagnoseFlake {
		t.Fatalf("decoded=%#v err=%v", got.VerificationAttempt, err)
	}
	invalid := `{"ipc_version":2,"kind":"request","request_id":"a","action":"start","operation_id":"attempt-ipc","workspace_id":"ws_01K00000000000000000000000","command":"true","cwd":".","evidence":{"verification_kind":"test","source_scope":"full"},"verification_attempt":{"rerun_reason":"diagnose_flake"}}`
	if _, err := decodeRequestV2(bytesReaderV2([]byte(invalid))); err == nil {
		t.Fatal("invalid verification attempt accepted")
	}
	cross := `{"ipc_version":2,"kind":"request","request_id":"p","action":"poll","session_id":"s","verification_attempt":{"rerun_of_evidence_id":"ev_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`
	if _, err := decodeRequestV2(bytesReaderV2([]byte(cross))); err == nil {
		t.Fatal("verification_attempt accepted outside start")
	}
}
