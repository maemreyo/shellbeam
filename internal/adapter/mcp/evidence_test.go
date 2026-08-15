package mcp

import (
	"context"
	"encoding/json"
	"testing"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	evidenceapp "github.com/maemreyo/shellbeam/internal/app/evidence"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	core "github.com/maemreyo/shellbeam/internal/core/evidence"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"
)

type evidenceMCPClient struct {
	last       bridge.Request
	startCalls int
}

func (c *evidenceMCPClient) Forward(_ context.Context, req bridge.Request) (bridge.Response, error) {
	c.last = req
	if req.Action == "start" {
		c.startCalls++
		result := receipt.Result{SchemaVersion: 2, Operation: receipt.OperationResult{OperationID: req.Start.OperationID, SessionID: "s", State: receipt.OperationRunning}, Child: &receipt.ChildResult{State: receipt.ChildRunning}, Output: receipt.OutputResult{CanonicalStream: "combined"}}
		return bridge.Response{Result: &result}, nil
	}
	if req.Action == "inspect.evidence" {
		value := evidenceapp.InspectResult{SchemaVersion: 1, Status: evidenceapp.InspectAvailable, IndexGeneration: 9}
		return bridge.Response{Evidence: &value}, nil
	}
	if req.Action == "inspect.server" {
		catalog := capability.Baseline(capability.Limits{})
		return bridge.Response{Server: &catalog}, nil
	}
	return bridge.Response{}, nil
}

func TestEvidenceMCPV2ForwardsClosedStartAndInspectUsingLocalShell(t *testing.T) {
	client := &evidenceMCPClient{}
	session, closeSession := currentSession(t, New(bridge.New(client), capability.Baseline(capability.Limits{})))
	defer closeSession()
	start, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(`{"action":"start","operation_id":"mcp-evidence","workspace_id":"ws_01K00000000000000000000000","command":"true","evidence":{"verification_kind":"test","source_scope":"full","expected_outputs":[{"path":"dist/report.json","kind":"file","digest":"sha256","required":true}]}}`)})
	if err != nil || start.IsError || client.startCalls != 1 || client.last.Start.Evidence == nil || client.last.Start.Evidence.VerificationKind != core.VerificationTest {
		t.Fatalf("start=%#v req=%#v err=%v", start, client.last, err)
	}
	res, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(`{"action":"inspect.evidence","workspace_id":"ws_01K00000000000000000000000","verification_kind":"test","result":"pass","max_records":2}`)})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError || client.startCalls != 1 || client.last.Action != "inspect.evidence" || client.last.EvidenceInspect.Filter.Result != core.ResultPass {
		t.Fatalf("res=%#v req=%#v starts=%d", res, client.last, client.startCalls)
	}
	body, ok := res.StructuredContent.(map[string]any)
	if !ok || body["evidence"] == nil {
		t.Fatalf("body=%#v", res.StructuredContent)
	}
}

func TestEvidenceMCPV2RejectsCrossActionAndUnknownEvidenceFields(t *testing.T) {
	client := &evidenceMCPClient{}
	session, closeSession := currentSession(t, New(bridge.New(client), capability.Baseline(capability.Limits{})))
	defer closeSession()
	for _, raw := range []string{
		`{"action":"poll","session_id":"s","revalidate_artifacts":true}`,
		`{"action":"start","operation_id":"op","cwd":"/","command":"true","evidence":{"verification_kind":"test","secret":"no"}}`,
	} {
		res, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(raw)})
		if err != nil {
			t.Fatal(err)
		}
		if !res.IsError {
			t.Fatalf("accepted %s", raw)
		}
	}
}
