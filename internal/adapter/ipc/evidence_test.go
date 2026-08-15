//go:build linux || darwin

package ipc

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	evidenceapp "github.com/maemreyo/shellbeam/internal/app/evidence"
	core "github.com/maemreyo/shellbeam/internal/core/evidence"
	"github.com/maemreyo/shellbeam/internal/core/project"
)

type evidenceIPCActions struct {
	fakeActions
	startCalls  int
	lastStart   daemonapp.StartRequest
	lastInspect evidenceapp.InspectRequest
}

func (a *evidenceIPCActions) Start(ctx context.Context, req daemonapp.StartRequest) (daemonapp.View, error) {
	a.startCalls++
	a.lastStart = req
	return a.fakeActions.Start(ctx, req)
}
func (a *evidenceIPCActions) InspectEvidence(_ context.Context, req evidenceapp.InspectRequest) (evidenceapp.InspectResult, error) {
	a.lastInspect = req
	return evidenceapp.InspectResult{SchemaVersion: 1, Status: evidenceapp.InspectAvailable, IndexGeneration: 7}, nil
}

func TestEvidenceIPCV2ForwardsRawContractAndInspectWithoutSpawn(t *testing.T) {
	actions := &evidenceIPCActions{}
	runtime, err := os.MkdirTemp("/tmp", "shellbeam-evidence-ipc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtime) })
	server, err := Listen(runtime, actions)
	if err != nil {
		if errors.Is(err, os.ErrPermission) || strings.Contains(err.Error(), "operation not permitted") {
			t.Skip("sandbox blocks Unix sockets")
		}
		t.Fatal(err)
	}
	defer server.Close()
	go server.Serve()
	client := NewClient(server.SocketPath())
	contract := &core.Contract{VerificationKind: core.VerificationBuild, SourceScope: core.SourceScopeFull, ExpectedOutputs: []project.Output{{Path: "dist/app", Kind: "file", Digest: "sha256", Required: true}}}
	if _, err := client.CallV2(context.Background(), RequestV2{IPVersion: 2, Kind: "request", RequestID: "start-evidence", Action: "start", OperationID: "evidence-start", WorkspaceID: "ws_01K00000000000000000000000", Command: "true", Evidence: contract}); err != nil {
		t.Fatal(err)
	}
	if actions.startCalls != 1 || actions.lastStart.Evidence == nil || actions.lastStart.Evidence.VerificationKind != core.VerificationBuild || len(actions.lastStart.Evidence.ExpectedOutputs) != 1 {
		t.Fatalf("start=%#v calls=%d", actions.lastStart, actions.startCalls)
	}
	got, err := client.CallV2(context.Background(), RequestV2{IPVersion: 2, Kind: "request", RequestID: "inspect-evidence", Action: "inspect.evidence", WorkspaceID: "ws_01K00000000000000000000000", VerificationKind: core.VerificationBuild, EvidenceResult: core.ResultPass, RevalidateArtifacts: true, MaxRecords: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !got.OK || got.Evidence == nil || got.Evidence.IndexGeneration != 7 || actions.startCalls != 1 {
		t.Fatalf("got=%#v starts=%d", got, actions.startCalls)
	}
	if actions.lastInspect.Filter.WorkspaceID == "" || actions.lastInspect.Filter.VerificationKind != core.VerificationBuild || actions.lastInspect.Filter.Result != core.ResultPass || !actions.lastInspect.Filter.RevalidateArtifacts || actions.lastInspect.MaxRecords != 2 {
		t.Fatalf("inspect=%#v", actions.lastInspect)
	}
}

func TestEvidenceIPCV2RejectsCrossActionAndInvalidContractsBeforeDispatch(t *testing.T) {
	bad := []RequestV2{
		{IPVersion: 2, Kind: "request", RequestID: "x", Action: "start", OperationID: "op", CWD: "/", Command: "true", Evidence: &core.Contract{VerificationKind: "bogus"}},
		{IPVersion: 2, Kind: "request", RequestID: "x", Action: "inspect.evidence", OperationID: "../bad", MaxRecords: 1},
		{IPVersion: 2, Kind: "request", RequestID: "x", Action: "inspect.evidence", RevalidateArtifacts: true, MaxRecords: core.MaxRevalidateRecords + 1},
	}
	for _, req := range bad {
		if err := validateRequestV2(req); err == nil {
			t.Fatalf("accepted %#v", req)
		}
	}
	for _, raw := range [][]byte{
		[]byte(`{"ipc_version":2,"kind":"request","request_id":"x","action":"poll","session_id":"s","evidence":{"verification_kind":"test"}}`),
		[]byte(`{"ipc_version":2,"kind":"request","request_id":"x","action":"start","operation_id":"op","cwd":"/","command":"true","revalidate_artifacts":true}`),
	} {
		if _, err := decodeRequestV2(bytesReaderV2(raw)); err == nil {
			t.Fatalf("cross action accepted: %s", raw)
		}
	}
}
