package ipc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	traceapp "github.com/maemreyo/shellbeam/internal/app/inputtrace"
	trace "github.com/maemreyo/shellbeam/internal/core/inputtrace"
)

func TestE27InputTraceIPCRequestAndResponseCarryOnlyTypedTraceFields(t *testing.T) {
	start := RequestV2{Action: "start", OperationID: "e27-ipc-start", Command: "true", CWD: "/tmp", TraceMode: trace.ModeBestEffort}
	b, err := json.Marshal(start)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"trace_mode":"best_effort"`) {
		t.Fatalf("start=%s", b)
	}
	inspect := RequestV2{Action: "inspect.trace", OperationID: "e27-ipc-start", MaxResources: 7}
	b, err = json.Marshal(inspect)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"max_resources":7`) {
		t.Fatalf("inspect=%s", b)
	}
	result := traceapp.InspectResult{SchemaVersion: 1, Status: traceapp.InspectPending, OperationID: "e27-ipc-start", TraceID: "trace_01K00000000000000000000000"}
	response := ResponseV2{Action: "inspect.trace", OK: true, InputTrace: &result}
	b, err = json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if !strings.Contains(text, `"input_trace"`) || strings.Contains(text, "socket_path") || strings.Contains(text, "raw_events") {
		t.Fatalf("response=%s", b)
	}
}

func TestE27InputTraceIPCValidationIsClosedBoundedAndV2Only(t *testing.T) {
	validTyped := RequestV2{IPVersion: 2, Kind: "request", RequestID: "typed", Action: "start", OperationID: "e27-typed", WorkspaceID: "ws_01K00000000000000000000000", ProjectCommandID: "test", TraceMode: trace.ModeBestEffort}
	if err := validateRequestV2(validTyped); err != nil {
		t.Fatalf("typed traced start rejected: %v", err)
	}
	invalidMode := validTyped
	invalidMode.TraceMode = trace.Mode("sometimes")
	if err := validateRequestV2(invalidMode); err == nil {
		t.Fatal("invalid trace mode accepted")
	}
	for _, max := range []int{0, trace.MaxPublicResources + 1} {
		req := RequestV2{IPVersion: 2, Kind: "request", RequestID: "inspect", Action: "inspect.trace", OperationID: "e27-inspect", MaxResources: max}
		if err := validateRequestV2(req); err == nil {
			t.Fatalf("inspect.trace max_resources=%d accepted", max)
		}
	}
	validInspect := RequestV2{IPVersion: 2, Kind: "request", RequestID: "inspect", Action: "inspect.trace", OperationID: "e27-inspect", MaxResources: trace.MaxPublicResources}
	if err := validateRequestV2(validInspect); err != nil {
		t.Fatalf("bounded inspect rejected: %v", err)
	}
	legacy := []byte(`{"ipc_version":1,"request_id":"legacy","payload":{"action":"start","operation_id":"e27-legacy","command":"true","cwd":"/tmp","trace_mode":"best_effort"}}`)
	if _, err := decodeRequest(bytes.NewReader(legacy)); err == nil {
		t.Fatal("IPC v1 accepted trace_mode")
	}
}

type e27InputTraceActions struct {
	fakeActions
	calls  int
	last   traceapp.InspectRequest
	result traceapp.InspectResult
}

func (a *e27InputTraceActions) InspectInputTrace(_ context.Context, req traceapp.InspectRequest) (traceapp.InspectResult, error) {
	a.calls++
	a.last = req
	return a.result, nil
}

func TestE27InputTraceIPCRoutesDeepInspectAndStartStatusWithoutOffPathWork(t *testing.T) {
	full := traceapp.InspectResult{
		SchemaVersion: 1, Status: traceapp.InspectTerminal, OperationID: "e27-route", TraceID: "trace_01K00000000000000000000000",
		Record:            &trace.Record{SchemaVersion: trace.SchemaVersion, TraceID: "trace_01K00000000000000000000000", OperationID: "e27-route"},
		ResourcesReturned: 3, ResourcesAvailable: 9,
	}
	actions := &e27InputTraceActions{result: full}
	server := &Server{actions: actions}
	var deep ResponseV2
	if err := server.inspectInputTraceV2(context.Background(), RequestV2{Action: "inspect.trace", OperationID: "e27-route", MaxResources: 7}, &deep); err != nil {
		t.Fatal(err)
	}
	if deep.InputTrace == nil || deep.InputTrace.Record == nil || deep.InputTrace.ResourcesReturned != 3 || actions.calls != 1 || actions.last.MaxResources != 7 {
		t.Fatalf("deep=%#v calls=%d last=%#v", deep.InputTrace, actions.calls, actions.last)
	}

	var off ResponseV2
	if err := server.decorateStartInputTraceV2(context.Background(), RequestV2{Action: "start", OperationID: "e27-route"}, &off); err != nil {
		t.Fatal(err)
	}
	if actions.calls != 1 || off.InputTrace != nil {
		t.Fatalf("off path touched inspector calls=%d response=%#v", actions.calls, off.InputTrace)
	}

	var status ResponseV2
	if err := server.decorateStartInputTraceV2(context.Background(), RequestV2{Action: "start", OperationID: "e27-route", TraceMode: trace.ModeBestEffort}, &status); err != nil {
		t.Fatal(err)
	}
	if actions.calls != 2 || actions.last.MaxResources != 1 || status.InputTrace == nil || status.InputTrace.Record != nil || status.InputTrace.ResourcesReturned != 0 || status.InputTrace.ResourcesAvailable != 0 {
		t.Fatalf("status=%#v calls=%d last=%#v", status.InputTrace, actions.calls, actions.last)
	}
}

func TestE27InputTraceIPCErrorFinalizationClearsTracePayload(t *testing.T) {
	traceResult := traceapp.InspectResult{SchemaVersion: 1, Status: traceapp.InspectPending, OperationID: "e27-clear"}
	resp := finalizeResponseV2(ResponseV2{Action: "inspect.trace", InputTrace: &traceResult}, errors.New("trace failure"))
	if resp.OK || resp.Error == nil || resp.InputTrace != nil {
		t.Fatalf("error finalization leaked trace payload: %#v", resp)
	}
}
