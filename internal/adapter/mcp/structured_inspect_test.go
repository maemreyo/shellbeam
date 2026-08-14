package mcp

import (
	"context"
	"encoding/json"
	"testing"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"
)

type structuredInspectClient struct {
	last       bridge.Request
	startCalls int
}

func (c *structuredInspectClient) Forward(_ context.Context, req bridge.Request) (bridge.Response, error) {
	c.last = req
	if req.Action == "start" {
		c.startCalls++
	}
	if req.Action == "inspect.structured" {
		result := structuredapp.InspectResult{SchemaVersion: 1, OperationID: req.StructuredInspect.OperationID, Status: structuredapp.InspectTerminal, ParseOutcome: core.ParseComplete, Completeness: core.CompletenessComplete, Summary: structuredapp.InspectSummary{DetailsStatus: structuredapp.DetailsAvailable, RecordsTotalExact: true}}
		return bridge.Response{Structured: &result}, nil
	}
	if req.Action == "inspect.server" {
		catalog := capability.Baseline(capability.Limits{})
		return bridge.Response{Server: &catalog}, nil
	}
	return bridge.Response{}, nil
}

func TestStructuredInspectMCPV2ForwardsFiltersWithoutSpawn(t *testing.T) {
	client := &structuredInspectClient{}
	session, closeSession := currentSession(t, New(bridge.New(client), capability.Baseline(capability.Limits{})))
	defer closeSession()
	res, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(`{"action":"inspect.structured","operation_id":"op-1","record_kind":"diagnostic","severity":"error","path":"internal/a.go","max_records":10}`)})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError || client.startCalls != 0 || client.last.Action != "inspect.structured" || client.last.StructuredInspect.Filter.Path != "internal/a.go" {
		t.Fatalf("result=%#v request=%#v starts=%d", res, client.last, client.startCalls)
	}
	body, ok := res.StructuredContent.(map[string]any)
	if !ok || body["structured"] == nil {
		t.Fatalf("structured=%#v", res.StructuredContent)
	}
}
