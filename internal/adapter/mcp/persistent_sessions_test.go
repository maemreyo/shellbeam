package mcp

import (
	"context"
	"encoding/json"
	"testing"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	persistent "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"
)

type persistentMCPClient struct {
	last    bridge.Request
	catalog capability.Catalog
}

func (c *persistentMCPClient) Forward(_ context.Context, request bridge.Request) (bridge.Response, error) {
	c.last = request
	switch request.Action {
	case "start":
		result := receipt.Result{SchemaVersion: 2, Operation: receipt.OperationResult{OperationID: request.Start.OperationID, SessionID: "persistent-mcp-session", State: receipt.OperationRunning}, Child: &receipt.ChildResult{State: receipt.ChildRunning}, Output: receipt.OutputResult{CanonicalStream: "combined"}}
		return bridge.Response{Result: &result}, nil
	case "inspect.sessions":
		return bridge.Response{Sessions: &persistent.InspectPage{Sessions: []persistent.Summary{{SessionID: "persistent-mcp-session", OperationID: "persistent-mcp-op", SessionName: "dev-server", Persistent: true, OwnershipStatus: persistent.OwnershipCurrent}}}}, nil
	case "inspect.server":
		return bridge.Response{Server: &c.catalog}, nil
	default:
		return bridge.Response{}, nil
	}
}

func TestPersistentSessionMCPV2ForwardsStartAndInspectThroughOneTool(t *testing.T) {
	catalog := capability.Baseline(capability.Limits{})
	client := &persistentMCPClient{catalog: catalog}
	session, closeSession := currentSession(t, New(bridge.New(client), catalog))
	defer closeSession()

	start, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(`{"action":"start","operation_id":"persistent-mcp-op","command":"sleep 10","cwd":"/tmp","persistent":true,"session_name":"dev-server"}`)})
	if err != nil || start.IsError {
		t.Fatalf("start=%#v err=%v", start, err)
	}
	if !client.last.Start.Persistent || client.last.Start.SessionName != "dev-server" || client.last.ProtocolVersion != 2 {
		t.Fatalf("forwarded start=%#v", client.last)
	}

	inspect, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(`{"action":"inspect.sessions","session_name":"dev-server","persistent_only":false,"max_records":10}`)})
	if err != nil || inspect.IsError {
		t.Fatalf("inspect=%#v err=%v", inspect, err)
	}
	if client.last.Action != "inspect.sessions" || client.last.SessionInspect.PersistentOnly == nil || *client.last.SessionInspect.PersistentOnly || client.last.SessionInspect.SessionName != "dev-server" {
		t.Fatalf("forwarded inspect=%#v", client.last)
	}
	body, ok := inspect.StructuredContent.(map[string]any)
	if !ok || body["sessions"] == nil {
		t.Fatalf("inspect body=%#v", inspect.StructuredContent)
	}

	tools, err := session.ListTools(context.Background(), nil)
	if err != nil || len(tools.Tools) != 1 || tools.Tools[0].Name != "local_shell" {
		t.Fatalf("tools=%#v err=%v", tools, err)
	}
}

func TestPersistentSessionLegacyMCPRejectsB1FieldsAndAction(t *testing.T) {
	catalog := capability.Baseline(capability.Limits{})
	client := &persistentMCPClient{catalog: catalog}
	server := New(bridge.New(client), catalog)
	forceLegacyDiscovery(server)
	st, ct := mcpgo.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go server.Run(ctx, st)
	sdk := mcpgo.NewClient(&mcpgo.Implementation{Name: "legacy-persistent", Version: "1"}, &mcpgo.ClientOptions{Capabilities: &mcpgo.ClientCapabilities{}})
	session, err := sdk.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	for _, raw := range []string{
		`{"action":"start","operation_id":"legacy-persistent-op","command":"true","cwd":"/tmp","persistent":true}`,
		`{"action":"inspect.sessions"}`,
	} {
		result, callErr := session.CallTool(ctx, &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(raw)})
		if callErr == nil && result != nil && !result.IsError {
			t.Fatalf("legacy accepted %s: %#v", raw, result)
		}
	}
}

func TestPersistentSessionLegacyCatalogOmitsB1Capabilities(t *testing.T) {
	modern := capability.Baseline(capability.Limits{LiveSessions: 4, SessionOutputBytes: 4096}).WithNamedSessions(4, 4096, 512)
	legacy := legacyCatalogView(modern)
	if _, ok := legacy.Features[capability.FeatureNamedSessions]; ok || len(legacy.PersistentSessionSchemaVersions) != 0 || len(legacy.SupervisorProtocolVersions) != 0 || legacy.PersistentNonTTY || legacy.PersistentContinuity != "" || legacy.Limits.PersistentSessions != 0 || legacy.Limits.PersistentSessionInspectRows != 0 {
		t.Fatalf("legacy leaked B1 capability: %#v", legacy)
	}
}
