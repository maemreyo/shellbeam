package mcp

import (
	"context"
	"encoding/json"
	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"
	"strings"
	"testing"
)

type fakeClient struct{ last bridge.Request }

func (f *fakeClient) Forward(_ context.Context, r bridge.Request) (bridge.Response, error) {
	f.last = r
	return bridge.Response{View: app.View{OperationID: r.Start.OperationID, SessionID: "s"}}, nil
}

func TestMetadata(t *testing.T) {
	if len(Instructions) > 512 && Instructions[:10] == "" {
		t.Fatal("instructions")
	}
	tool := ToolDefinition()
	if tool.Name != "local_shell" || tool.Annotations == nil || tool.Annotations.ReadOnlyHint || tool.Annotations.IdempotentHint {
		t.Fatalf("%#v", tool)
	}
	for _, tool := range []*mcpgo.Tool{ToolDefinition(), ToolDefinitionV2()} {
		if !strings.Contains(tool.Description, "inspect.project") || !strings.Contains(tool.Description, "agent_bootstrap") {
			t.Fatalf("tool description missing repository bootstrap pointer: %q", tool.Description)
		}
	}
}
func TestInMemoryConformance(t *testing.T) {
	st, ct := mcpgo.NewInMemoryTransports()
	fake := &fakeClient{}
	server := New(bridge.New(fake), capability.Baseline(capability.Limits{}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	forceLegacyDiscovery(server)
	go server.Run(ctx, st)
	client := mcpgo.NewClient(&mcpgo.Implementation{Name: "test", Version: "1"}, nil)
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 1 || tools.Tools[0].Name != "local_shell" {
		t.Fatalf("%#v", tools.Tools)
	}
	res, err := session.CallTool(ctx, &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(`{"action":"start","operation_id":"o","command":"true","cwd":"/"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("%#v", res)
	}
	if fake.last.Start.YieldMS != 10000 || fake.last.Start.MaxOutputBytes != 20000 {
		t.Fatalf("defaults=%#v", fake.last.Start)
	}
	_, err = session.CallTool(ctx, &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(`{"action":"start","operation_id":"z","command":"true","cwd":"/","yield_time_ms":0,"max_output_bytes":0}`)})
	if err != nil {
		t.Fatal(err)
	}
	if fake.last.Start.YieldMS != 0 || fake.last.Start.MaxOutputBytes != 0 {
		t.Fatalf("explicit zeros=%#v", fake.last.Start)
	}
}

type typedFailureMCPClient struct{}

func (typedFailureMCPClient) Forward(_ context.Context, _ bridge.Request) (bridge.Response, error) {
	return bridge.Response{
		Code: "workspace_root_missing",
		Details: map[string]string{
			"workspace_id": "ws_01K00000000000000000000000",
			"reason":       "root_missing",
		},
	}, nil
}

func TestMCPV2FailurePreservesSafeDetails(t *testing.T) {
	catalog := capability.Baseline(capability.Limits{})
	session, closeSession := currentSession(t, New(bridge.New(typedFailureMCPClient{}), catalog))
	defer closeSession()
	res, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(`{"action":"inspect.workspace","workspace_id":"ws_01K00000000000000000000000"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatalf("result=%#v", res)
	}
	body, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("structured=%T %#v", res.StructuredContent, res.StructuredContent)
	}
	errBody, _ := body["error"].(map[string]any)
	details, _ := errBody["details"].(map[string]any)
	if errBody["code"] != "workspace_root_missing" || details["workspace_id"] != "ws_01K00000000000000000000000" || details["reason"] != "root_missing" {
		t.Fatalf("body=%#v", body)
	}
}
