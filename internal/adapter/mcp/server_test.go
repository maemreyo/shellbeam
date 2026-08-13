package mcp

import (
	"context"
	"encoding/json"
	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"
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
}
func TestInMemoryConformance(t *testing.T) {
	st, ct := mcpgo.NewInMemoryTransports()
	fake := &fakeClient{}
	server := New(bridge.New(fake))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
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
