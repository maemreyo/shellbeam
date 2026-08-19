package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	traceapp "github.com/maemreyo/shellbeam/internal/app/inputtrace"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	trace "github.com/maemreyo/shellbeam/internal/core/inputtrace"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"strings"
	"testing"
)

func TestE27InputTraceMCPInputCarriesTraceModeAndInspectBound(t *testing.T) {
	raw := []byte(`{"action":"start","operation_id":"e27-mcp","cwd":"/tmp","command":"true","trace_mode":"best_effort"}`)
	in, err := decodeInput(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateForVersion(2, in, raw); err != nil {
		t.Fatal(err)
	}
	inspectRaw := []byte(`{"action":"inspect.trace","operation_id":"e27-mcp","max_resources":7}`)
	inspect, err := decodeInput(inspectRaw)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateForVersion(2, inspect, inspectRaw); err != nil {
		t.Fatal(err)
	}
	if _, err := json.Marshal(inspect); err != nil {
		t.Fatal(err)
	}
}

func TestE27InputTraceMCPV1RejectsTraceControlsAndV2BoundsInspect(t *testing.T) {
	traceRaw := []byte(`{"action":"start","operation_id":"e27-v1","cwd":"/tmp","command":"true","trace_mode":"best_effort"}`)
	traceInput, err := decodeInput(traceRaw)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateForVersion(1, traceInput, traceRaw); err == nil {
		t.Fatal("MCP v1 accepted trace_mode")
	}
	inspectRaw := []byte(`{"action":"inspect.trace","operation_id":"e27-v1","max_resources":1}`)
	inspect, err := decodeInput(inspectRaw)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateForVersion(1, inspect, inspectRaw); err == nil {
		t.Fatal("MCP v1 accepted inspect.trace")
	}
	for _, max := range []int{0, 513} {
		raw := []byte(fmt.Sprintf(`{"action":"inspect.trace","operation_id":"e27-v2","max_resources":%d}`, max))
		in, err := decodeInput(raw)
		if err != nil {
			t.Fatal(err)
		}
		if err := validateForVersion(2, in, raw); err == nil {
			t.Fatalf("MCP v2 accepted max_resources=%d", max)
		}
	}
}

func TestE27InputTraceMCPStartSummaryIsConciseAndDeepInspectCarriesRecord(t *testing.T) {
	result := receipt.Result{
		SchemaVersion: 2,
		Operation:     receipt.OperationResult{OperationID: "e27-mcp-status", SessionID: "s-e27", State: receipt.OperationRunning},
		Child:         &receipt.ChildResult{State: receipt.ChildRunning},
		Output:        receipt.OutputResult{CanonicalStream: "combined"},
	}
	terminal := traceapp.InspectResult{
		SchemaVersion: 1, Status: traceapp.InspectTerminal, OperationID: "e27-mcp-status", TraceID: "trace_01K00000000000000000000000",
		Record:            &trace.Record{SchemaVersion: trace.SchemaVersion, TraceID: "trace_01K00000000000000000000000", OperationID: "e27-mcp-status"},
		ResourcesReturned: 2, ResourcesAvailable: 4,
	}
	start := successV2(input{Action: "start", TraceMode: trace.ModeBestEffort}, bridge.Response{Result: &result, InputTrace: &terminal})
	if start.IsError || len(start.Content) != 1 {
		t.Fatalf("start=%#v", start)
	}
	startBody, ok := start.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("start structured=%T %#v", start.StructuredContent, start.StructuredContent)
	}
	status, ok := startBody["input_trace"].(map[string]any)
	if !ok || status["requested_mode"] != trace.ModeBestEffort || status["status"] != traceapp.InspectTerminal || status["trace_id"] != terminal.TraceID {
		t.Fatalf("start trace status=%#v", startBody["input_trace"])
	}
	encodedStart, err := json.Marshal(startBody)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encodedStart), `"record"`) || strings.Contains(string(encodedStart), "socket_path") || strings.Contains(string(encodedStart), "raw_events") {
		t.Fatalf("start summary leaked deep/private trace data: %s", encodedStart)
	}

	deep := successV2(input{Action: "inspect.trace"}, bridge.Response{InputTrace: &terminal})
	if deep.IsError || len(deep.Content) != 1 {
		t.Fatalf("deep=%#v", deep)
	}
	deepBody, ok := deep.StructuredContent.(map[string]any)
	if !ok || deepBody["input_trace"] == nil {
		t.Fatalf("deep structured=%#v", deep.StructuredContent)
	}
	encodedDeep, err := json.Marshal(deepBody)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encodedDeep), `"record"`) || !strings.Contains(string(encodedDeep), `"resources_returned":2`) {
		t.Fatalf("deep trace omitted record/resources: %s", encodedDeep)
	}
	if strings.Contains(string(encodedDeep), "socket_path") || strings.Contains(string(encodedDeep), "dylib_path") || strings.Contains(string(encodedDeep), `"raw_events":`) {
		t.Fatalf("deep trace leaked private data: %s", encodedDeep)
	}
}

func TestE27InputTraceOneToolDiscoveryKeepsLocalShellOnly(t *testing.T) {
	catalog := capability.Baseline(capability.Limits{})
	fake := &discoveryClient{catalog: catalog}
	session, closeSession := currentSession(t, New(bridge.New(fake), catalog))
	defer closeSession()
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	assertOneToolSchemaID(t, tools.Tools, "https://shellbeam.dev/schema/mcp-tool-input-v2.json")
	data, err := json.Marshal(tools.Tools[0].InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "inspect.trace") || !strings.Contains(string(data), "trace_mode") {
		t.Fatalf("one local_shell schema missing E27 surface: %s", data)
	}
}
