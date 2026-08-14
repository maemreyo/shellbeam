package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"
)

type discoveryClient struct {
	last       bridge.Request
	startCalls int
	catalog    capability.Catalog
}

func (f *discoveryClient) Forward(_ context.Context, r bridge.Request) (bridge.Response, error) {
	f.last = r
	if r.Action == "start" {
		f.startCalls++
		if r.ProtocolVersion == 2 {
			result := receipt.Result{
				SchemaVersion: 2,
				Operation:     receipt.OperationResult{OperationID: r.Start.OperationID, SessionID: "s", State: receipt.OperationRunning},
				Child:         &receipt.ChildResult{State: receipt.ChildRunning},
				Output:        receipt.OutputResult{CanonicalStream: "combined"},
			}
			return bridge.Response{Result: &result}, nil
		}
	}
	if r.Action == "inspect.server" {
		return bridge.Response{Server: &f.catalog}, nil
	}
	return bridge.Response{View: app.View{OperationID: r.Start.OperationID, SessionID: "s"}}, nil
}

func TestDiscoveryCurrentClientGetsV2ToolAndCatalog(t *testing.T) {
	catalog := capability.Baseline(capability.Limits{CommandBytes: 32768, ResponseBytes: 20000, LiveSessions: 4})
	fake := &discoveryClient{catalog: catalog}
	server := New(bridge.New(fake), catalog)
	st, ct := mcpgo.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go server.Run(ctx, st)
	client := mcpgo.NewClient(&mcpgo.Implementation{Name: "current", Version: "1"}, &mcpgo.ClientOptions{Capabilities: &mcpgo.ClientCapabilities{}})
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	init := session.InitializeResult()
	if init.ProtocolVersion != "2026-07-28" {
		t.Fatalf("protocol=%q", init.ProtocolVersion)
	}
	extension, ok := init.Capabilities.Extensions[ExtensionName].(map[string]any)
	if !ok || fmt.Sprint(extension["schema_version"]) != "1" || extension["catalog"] == nil {
		t.Fatalf("extension=%#v capabilities=%#v", extension, init.Capabilities)
	}
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertOneToolSchemaID(t, tools.Tools, "https://shellbeam.dev/schema/mcp-input-v2.json")
}

func TestLegacyClientKeepsV1ToolAndCapabilityFallback(t *testing.T) {
	catalog := capability.Baseline(capability.Limits{CommandBytes: 8192, LiveSessions: 2})
	fake := &discoveryClient{catalog: catalog}
	server := New(bridge.New(fake), catalog)
	st, ct := mcpgo.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	forceLegacyDiscovery(server)
	go server.Run(ctx, st)
	client := mcpgo.NewClient(&mcpgo.Implementation{Name: "legacy", Version: "1"}, &mcpgo.ClientOptions{Capabilities: &mcpgo.ClientCapabilities{}})
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	init := session.InitializeResult()
	if init.ProtocolVersion != "2025-11-25" {
		t.Fatalf("protocol=%q", init.ProtocolVersion)
	}
	if init.Capabilities.Extensions[ExtensionName] == nil {
		t.Fatalf("legacy capability fallback=%#v", init.Capabilities)
	}
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertOneToolSchemaID(t, tools.Tools, "https://shellbeam.dev/schema/mcp-input-v1.json")
}

func TestInspectServerV2DoesNotStartCommand(t *testing.T) {
	catalog := capability.Baseline(capability.Limits{CommandBytes: 123})
	fake := &discoveryClient{catalog: catalog}
	session, closeSession := currentSession(t, New(bridge.New(fake), catalog))
	defer closeSession()
	res, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(`{"action":"inspect.server"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError || fake.last.Action != "inspect.server" || fake.last.ProtocolVersion != 2 || fake.startCalls != 0 {
		t.Fatalf("result=%#v last=%#v starts=%d", res, fake.last, fake.startCalls)
	}
}

func TestMCPV2UnsupportedFeatureReturnsFeatureUnavailable(t *testing.T) {
	catalog := capability.Baseline(capability.Limits{})
	fake := &discoveryClient{catalog: catalog}
	session, closeSession := currentSession(t, New(bridge.New(fake), catalog))
	defer closeSession()
	res, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(`{"action":"inspect.workspace"}`)})
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
	if fmt.Sprint(body["schema_version"]) != "2" || errBody["code"] != "feature_unavailable" {
		t.Fatalf("body=%#v", body)
	}
}

func forceLegacyDiscovery(server *mcpgo.Server) {
	server.AddReceivingMiddleware(func(next mcpgo.MethodHandler) mcpgo.MethodHandler {
		return func(ctx context.Context, method string, request mcpgo.Request) (mcpgo.Result, error) {
			if method == "server/discover" {
				return &mcpgo.DiscoverResult{
					Meta:              mcpgo.Meta{mcpgo.MetaKeyServerInfo: &mcpgo.Implementation{Name: "shellbeam", Version: "v2"}},
					SupportedVersions: []string{"2025-11-25"},
					Capabilities:      &mcpgo.ServerCapabilities{},
				}, nil
			}
			return next(ctx, method, request)
		}
	})
}

func currentSession(t *testing.T, server *mcpgo.Server) (*mcpgo.ClientSession, func()) {
	t.Helper()
	st, ct := mcpgo.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	go server.Run(ctx, st)
	client := mcpgo.NewClient(&mcpgo.Implementation{Name: "current", Version: "1"}, &mcpgo.ClientOptions{Capabilities: &mcpgo.ClientCapabilities{}})
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	return session, func() { _ = session.Close(); cancel() }
}

func assertOneToolSchemaID(t *testing.T, tools []*mcpgo.Tool, want string) {
	t.Helper()
	if len(tools) != 1 || tools[0].Name != "local_shell" {
		t.Fatalf("tools=%#v", tools)
	}
	data, err := json.Marshal(tools[0].InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	var schemaDoc map[string]any
	if err := json.Unmarshal(data, &schemaDoc); err != nil {
		t.Fatal(err)
	}
	if schemaDoc["$id"] != want {
		t.Fatalf("schema id=%#v want %q", schemaDoc["$id"], want)
	}
}

func TestMCPV2StartReturnsStructuredResult(t *testing.T) {
	catalog := capability.Baseline(capability.Limits{})
	fake := &discoveryClient{catalog: catalog}
	session, closeSession := currentSession(t, New(bridge.New(fake), catalog))
	defer closeSession()
	res, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(`{"action":"start","operation_id":"op","command":"true","cwd":"/tmp"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError || fake.last.ProtocolVersion != 2 || fake.startCalls != 1 {
		t.Fatalf("result=%#v last=%#v starts=%d", res, fake.last, fake.startCalls)
	}
	body, ok := res.StructuredContent.(map[string]any)
	if !ok || fmt.Sprint(body["schema_version"]) != "2" || body["result"] == nil {
		t.Fatalf("body=%#v", res.StructuredContent)
	}
}

func TestLegacyInspectServerUsesClosedNonSpawningFallback(t *testing.T) {
	catalog := capability.Baseline(capability.Limits{CommandBytes: 4096, LiveSessions: 2})
	fake := &discoveryClient{catalog: catalog}
	server := New(bridge.New(fake), catalog)
	forceLegacyDiscovery(server)
	st, ct := mcpgo.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go server.Run(ctx, st)
	client := mcpgo.NewClient(&mcpgo.Implementation{Name: "legacy-inspect", Version: "1"}, &mcpgo.ClientOptions{Capabilities: &mcpgo.ClientCapabilities{}})
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	res, err := session.CallTool(ctx, &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(`{"action":"inspect.server"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError || fake.startCalls != 0 || fake.last.Action != "inspect.server" {
		t.Fatalf("result=%#v last=%#v starts=%d", res, fake.last, fake.startCalls)
	}
	body, ok := res.StructuredContent.(map[string]any)
	if !ok || fmt.Sprint(body["schema_version"]) != "1" || body["server"] == nil {
		t.Fatalf("body=%#v", res.StructuredContent)
	}
}

func TestLegacyInspectServerRejectsCrossActionFields(t *testing.T) {
	catalog := capability.Baseline(capability.Limits{})
	fake := &discoveryClient{catalog: catalog}
	server := New(bridge.New(fake), catalog)
	forceLegacyDiscovery(server)
	st, ct := mcpgo.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go server.Run(ctx, st)
	client := mcpgo.NewClient(&mcpgo.Implementation{Name: "legacy-inspect-closed", Version: "1"}, nil)
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	res, err := session.CallTool(ctx, &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(`{"action":"inspect.server","session_id":"s"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || fake.last.Action != "" || fake.startCalls != 0 {
		t.Fatalf("result=%#v last=%#v starts=%d", res, fake.last, fake.startCalls)
	}
}

func TestMCPV2WorkspaceAddressAndHintAreForwarded(t *testing.T) {
	catalog := capability.Baseline(capability.Limits{})
	fake := &discoveryClient{catalog: catalog}
	session, closeSession := currentSession(t, New(bridge.New(fake), catalog))
	defer closeSession()
	res, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(`{"action":"start","operation_id":"op-ws","command":"true","workspace_id":"ws_01K00000000000000000000000","cwd":"src","workspace_hint":{"branch":"main"}}`)})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError || fake.last.Start.WorkspaceID != "ws_01K00000000000000000000000" || fake.last.Start.CWD != "src" || fake.last.Start.WorkspaceHint == nil || fake.last.Start.WorkspaceHint.Branch != "main" {
		t.Fatalf("result=%#v request=%#v", res, fake.last)
	}
}

func TestMCPV2ArgvIntentAreForwarded(t *testing.T) {
	catalog := capability.Baseline(capability.Limits{})
	fake := &discoveryClient{catalog: catalog}
	session, closeSession := currentSession(t, New(bridge.New(fake), catalog))
	defer closeSession()
	args := json.RawMessage(`{"action":"start","operation_id":"op-argv-forward","argv":["git","status"],"cwd":"/tmp","intent":{"kind":"inspect"}}`)
	res, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{Name: "local_shell", Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError || len(fake.last.Start.Argv) != 2 || fake.last.Start.Argv[0] != "git" || fake.last.Start.Intent == nil || fake.last.Start.Intent.Kind != "inspect" {
		t.Fatalf("result=%#v request=%#v", res, fake.last)
	}
}
