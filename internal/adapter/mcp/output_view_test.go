package mcp

import (
	"context"
	"encoding/json"
	"testing"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	"github.com/maemreyo/shellbeam/internal/app/outputview"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"
)

type outputViewBridgeClient struct {
	last       bridge.Request
	startCalls int
}

func (c *outputViewBridgeClient) Forward(_ context.Context, req bridge.Request) (bridge.Response, error) {
	c.last = req
	if req.Action == "start" {
		c.startCalls++
	}
	if req.Action == "read_output" {
		result := outputview.Result{SchemaVersion: 1, SessionID: req.OutputRead.SessionID, SelectorKind: req.OutputRead.Selector.Kind, RetentionState: outputview.RetentionRetained, FrozenCutBytes: 4, Text: "boom"}
		return bridge.Response{OutputView: &result}, nil
	}
	return bridge.Response{}, nil
}

func TestOutputViewMCPV2ForwardsSelectorWithoutSpawn(t *testing.T) {
	client := &outputViewBridgeClient{}
	session, closeSession := currentSession(t, New(bridge.New(client), capability.Baseline(capability.Limits{})))
	defer closeSession()
	res, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(`{"action":"read_output","session_id":"s","selector":{"kind":"search","mode":"literal","pattern":"boom","case_sensitive":true,"max_matches":2}}`)})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError || client.startCalls != 0 || client.last.Action != "read_output" || client.last.OutputRead.Selector.Pattern != "boom" {
		t.Fatalf("result=%#v request=%#v starts=%d", res, client.last, client.startCalls)
	}
	body, ok := res.StructuredContent.(map[string]any)
	if !ok || body["output_view"] == nil {
		t.Fatalf("structured=%#v", res.StructuredContent)
	}
}

func TestOutputViewMCPV2RejectsInvalidAndCrossActionFields(t *testing.T) {
	client := &outputViewBridgeClient{}
	session, closeSession := currentSession(t, New(bridge.New(client), capability.Baseline(capability.Limits{})))
	defer closeSession()
	for _, raw := range []string{
		`{"action":"read_output","session_id":"s"}`,
		`{"action":"read_output","session_id":"s","selector":{"kind":"tail","bytes":1,"lines":1}}`,
		`{"action":"read_output","session_id":"s","cursor":1,"selector":{"kind":"raw_range","max_bytes":1}}`,
		`{"action":"poll","session_id":"s","selector":{"kind":"raw_range","max_bytes":1}}`,
	} {
		res, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(raw)})
		if err != nil {
			t.Fatal(err)
		}
		if !res.IsError {
			t.Fatalf("invalid request accepted: %s => %#v", raw, res)
		}
	}
}
