//go:build linux || darwin

package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

type structuredInspectActions struct {
	fakeActions
	startCalls int
	last       structuredapp.InspectRequest
}

func (a *structuredInspectActions) Start(ctx context.Context, req daemonapp.StartRequest) (daemonapp.View, error) {
	a.startCalls++
	return a.fakeActions.Start(ctx, req)
}
func (a *structuredInspectActions) InspectStructured(_ context.Context, req structuredapp.InspectRequest) (structuredapp.InspectResult, error) {
	a.last = req
	return structuredapp.InspectResult{SchemaVersion: 1, OperationID: req.OperationID, Status: structuredapp.InspectTerminal, ParseOutcome: core.ParseComplete, Completeness: core.CompletenessComplete, SourceKind: core.StructuredInputArtifactBlob, SourceState: structuredapp.InputSourceRetained, SemanticsCoverage: &core.ProducerSemanticsCoverage{Namespace: "pytest", VocabularyVersion: 1, Format: "junit-xml", Family: "xunit2", MechanicallyObservable: []string{"coarse:pass"}, Unavailable: []string{"pytest:xpass_exact"}}, Summary: structuredapp.InspectSummary{DetailsStatus: structuredapp.DetailsAvailable, RecordsTotalExact: true}}, nil
}

func TestStructuredInspectIPCV2ForwardsClosedFiltersWithoutSpawn(t *testing.T) {
	actions := &structuredInspectActions{}
	runtime, err := os.MkdirTemp("/tmp", "shellbeam-structured-ipc-")
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
	got, err := NewClient(server.SocketPath()).CallV2(context.Background(), RequestV2{IPVersion: 2, Kind: "request", RequestID: "structured", Action: "inspect.structured", OperationID: "op-1", RecordKind: core.RecordDiagnostic, Severity: core.SeverityError, Path: "internal/a.go", MaxRecords: 10})
	if err != nil {
		t.Fatal(err)
	}
	if !got.OK || got.Structured == nil || got.Structured.Status != structuredapp.InspectTerminal || got.Structured.SourceKind != core.StructuredInputArtifactBlob || got.Structured.SourceState != structuredapp.InputSourceRetained || got.Structured.SemanticsCoverage == nil || got.Structured.SemanticsCoverage.Family != "xunit2" || actions.startCalls != 0 {
		t.Fatalf("response=%#v starts=%d", got, actions.startCalls)
	}
	if actions.last.OperationID != "op-1" || actions.last.Filter.RecordKind != core.RecordDiagnostic || actions.last.Filter.Path != "internal/a.go" || actions.last.MaxRecords != 10 {
		t.Fatalf("request=%#v", actions.last)
	}
	encoded, _ := json.Marshal(got.Structured)
	for _, forbidden := range []string{`"xpassed"`, `"xpass_count"`, `"error_phase"`, `"xfail_execution_state"`} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("invented completion claim %s in %s", forbidden, encoded)
		}
	}
}

func TestStructuredInspectIPCV2RejectsInvalidFilterAndCrossActionFields(t *testing.T) {
	bad := []RequestV2{
		{IPVersion: 2, Kind: "request", RequestID: "x", Action: "inspect.structured", OperationID: "op-1", Severity: "bogus", MaxRecords: 10},
		{IPVersion: 2, Kind: "request", RequestID: "x", Action: "inspect.structured", OperationID: "op-1", Path: "../secret", MaxRecords: 10},
		{IPVersion: 2, Kind: "request", RequestID: "x", Action: "inspect.structured", OperationID: "op-1", MaxRecords: 0},
	}
	for _, req := range bad {
		if err := validateRequestV2(req); err == nil {
			t.Fatalf("accepted %#v", req)
		}
	}
	raw := []byte(`{"ipc_version":2,"kind":"request","request_id":"x","action":"poll","session_id":"s","max_records":10}`)
	if _, err := decodeRequestV2(bytesReaderV2(raw)); err == nil {
		t.Fatal("poll accepted structured max_records")
	}
}
