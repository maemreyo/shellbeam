package mcp

import (
	"context"
	"encoding/json"
	"testing"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	observationapp "github.com/maemreyo/shellbeam/internal/app/observation"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	core "github.com/maemreyo/shellbeam/internal/core/observation"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"
)

type eventBridgeClient struct {
	last       bridge.Request
	startCalls int
}

func (c *eventBridgeClient) Forward(_ context.Context, req bridge.Request) (bridge.Response, error) {
	c.last = req
	if req.Action == "start" {
		c.startCalls++
	}
	if req.Action == "inspect.events" {
		result := observationapp.InspectResult{Continuity: core.ContinuityComplete, NextEventCursor: "evtcur_v1_abc.def"}
		return bridge.Response{Events: &result}, nil
	}
	return bridge.Response{}, nil
}

func TestMCPV2InspectEventsForwardsTypedTargetWithoutSpawn(t *testing.T) {
	client := &eventBridgeClient{}
	catalog := capability.Baseline(capability.Limits{}).WithEventJournal(256, 2048, 64, true)
	session, closeSession := currentSession(t, New(bridge.New(client), catalog))
	defer closeSession()
	res, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(`{"action":"inspect.events","target":{"kind":"operation","operation_id":"op-1"},"max_events":64}`)})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError || client.startCalls != 0 || client.last.Action != "inspect.events" || client.last.EventInspect.Target.OperationID != "op-1" || client.last.EventInspect.MaxEvents != 64 {
		t.Fatalf("result=%#v request=%#v starts=%d", res, client.last, client.startCalls)
	}
	body, ok := res.StructuredContent.(map[string]any)
	if !ok || body["events"] == nil {
		t.Fatalf("structured=%#v", res.StructuredContent)
	}
}

func TestMCPV2InspectEventsRejectsCrossActionAndMalformedCursor(t *testing.T) {
	client := &eventBridgeClient{}
	catalog := capability.Baseline(capability.Limits{}).WithEventJournal(256, 2048, 64, true)
	session, closeSession := currentSession(t, New(bridge.New(client), catalog))
	defer closeSession()
	for _, raw := range []string{
		`{"action":"inspect.events","target":{"kind":"operation","operation_id":"op-1"},"cursor":1,"max_events":64}`,
		`{"action":"inspect.events","target":{"kind":"operation","operation_id":"op-1"},"after_event_cursor":"outcur_v1_bad","max_events":64}`,
		`{"action":"inspect.events","target":{"kind":"operation","operation_id":"op-1"},"max_events":257}`,
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
