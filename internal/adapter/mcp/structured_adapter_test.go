package mcp

import (
	"context"
	"encoding/json"
	"testing"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"
)

type structuredAdapterClient struct{ last bridge.Request }

func (c *structuredAdapterClient) Forward(_ context.Context, r bridge.Request) (bridge.Response, error) {
	c.last = r
	result := receipt.Result{SchemaVersion: 2, Operation: receipt.OperationResult{OperationID: r.Start.OperationID, SessionID: "s", State: receipt.OperationRunning}, Child: &receipt.ChildResult{State: receipt.ChildRunning}, Output: receipt.OutputResult{CanonicalStream: "combined"}}
	return bridge.Response{Result: &result}, nil
}
func TestMCPStructuredAdapterStartForwardsMetadata(t *testing.T) {
	client := &structuredAdapterClient{}
	session, closeSession := currentSession(t, New(bridge.New(client), capability.Baseline(capability.Limits{})))
	defer closeSession()
	res, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(`{"action":"start","operation_id":"op","argv":["go","test","-json","./..."],"cwd":"/tmp","structured_adapter":"go-test-json"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError || client.last.Start.StructuredAdapter != "go-test-json" {
		t.Fatalf("result=%#v request=%#v", res, client.last)
	}
}
