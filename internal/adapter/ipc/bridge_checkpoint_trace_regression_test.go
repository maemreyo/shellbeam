//go:build linux || darwin

package ipc

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	inputtraceapp "github.com/maemreyo/shellbeam/internal/app/inputtrace"
	checkpointcore "github.com/maemreyo/shellbeam/internal/core/checkpoint"
	trace "github.com/maemreyo/shellbeam/internal/core/inputtrace"
)

func TestBridgeV2StartPreservesTraceMode(t *testing.T) {
	request := bridge.Request{ProtocolVersion: 2, Action: "start"}
	request.Start.OperationID = "trace-start-regression"
	request.Start.Command = "true"
	request.Start.CWD = "/tmp"
	request.Start.TraceMode = trace.ModeBestEffort

	got := requestV2FromBridge(request)
	if got.TraceMode != trace.ModeBestEffort {
		t.Fatalf("trace mode lost across bridge->IPC: %#v", got)
	}
}

func TestBridgeV2InspectTracePreservesInspectFields(t *testing.T) {
	request := bridge.Request{ProtocolVersion: 2, Action: "inspect.trace"}
	request.InputTraceInspect = inputtraceapp.InspectRequest{OperationID: "trace-inspect-regression", MaxResources: 17}

	got := requestV2FromBridge(request)
	if got.OperationID != request.InputTraceInspect.OperationID || got.MaxResources != request.InputTraceInspect.MaxResources {
		t.Fatalf("inspect.trace fields lost across bridge->IPC: %#v", got)
	}
}

func TestBridgeV2CheckpointResponsePreservesTypedPayload(t *testing.T) {
	checkpoint := checkpointcore.Checkpoint{CheckpointID: ipcCheckpointID}
	client := bridgeRegressionClient(t, ResponseV2{
		IPVersion: 2, Kind: "response", RequestID: "bridge", Action: "checkpoint_create", OK: true,
		Checkpoint: &checkpoint,
	})
	request := bridge.Request{ProtocolVersion: 2, Action: "checkpoint_create"}
	request.CheckpointCreate = checkpointcore.CreateRequest{
		CreateID: "checkpoint-response-regression", WorkspaceID: ipcCheckpointWorkspace, Paths: []string{"README.md"},
	}

	got, err := client.Forward(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if got.Checkpoint == nil || got.Checkpoint.CheckpointID != ipcCheckpointID {
		t.Fatalf("checkpoint payload lost across IPC->bridge: %#v", got)
	}
}

func TestBridgeV2InputTraceResponsePreservesTypedPayload(t *testing.T) {
	traceResult := inputtraceapp.InspectResult{
		SchemaVersion: 1, Status: inputtraceapp.InspectPending,
		OperationID: "trace-response-regression", TraceID: "trace_01K00000000000000000000000",
	}
	client := bridgeRegressionClient(t, ResponseV2{
		IPVersion: 2, Kind: "response", RequestID: "bridge", Action: "inspect.trace", OK: true,
		InputTrace: &traceResult,
	})
	request := bridge.Request{ProtocolVersion: 2, Action: "inspect.trace"}
	request.InputTraceInspect = inputtraceapp.InspectRequest{OperationID: traceResult.OperationID, MaxResources: 7}

	got, err := client.Forward(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if got.InputTrace == nil || got.InputTrace.TraceID != traceResult.TraceID {
		t.Fatalf("input trace payload lost across IPC->bridge: %#v", got)
	}
}

func TestBridgeV2FailurePreservesTypedDetails(t *testing.T) {
	client := bridgeRegressionClient(t, ResponseV2{
		IPVersion: 2, Kind: "response", RequestID: "bridge", Action: "inspect.workspace", OK: false,
		Error: &Error{Code: "workspace_root_missing", Message: "workspace root is missing", Retryable: false, Details: map[string]string{
			"workspace_id": "ws_01K00000000000000000000000",
			"reason":       "root_missing",
		}},
	})

	got, err := client.Forward(context.Background(), bridge.Request{ProtocolVersion: 2, Action: "inspect.workspace", WorkspaceID: "ws_01K00000000000000000000000"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Code != "workspace_root_missing" || got.Details["workspace_id"] != "ws_01K00000000000000000000000" || got.Details["reason"] != "root_missing" {
		t.Fatalf("typed failure details lost across IPC->bridge: %#v", got)
	}
}

func bridgeRegressionClient(t *testing.T, response ResponseV2) *Client {
	t.Helper()
	body, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	return &Client{http: &http.Client{Transport: roundTripV2Func(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header)}, nil
	})}}
}
