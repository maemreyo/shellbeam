package mcp

import (
	"context"
	"encoding/json"
	"testing"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	reproapp "github.com/maemreyo/shellbeam/internal/app/repro"
	telemetryapp "github.com/maemreyo/shellbeam/internal/app/telemetry"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	reprocore "github.com/maemreyo/shellbeam/internal/core/repro"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"
)

type telemetryReproClient struct {
	last       bridge.Request
	startCalls int
	dispatches int
}

func (c *telemetryReproClient) Forward(_ context.Context, req bridge.Request) (bridge.Response, error) {
	c.dispatches++
	c.last = req
	if req.Action == "start" {
		c.startCalls++
	}
	switch req.Action {
	case "inspect.telemetry":
		result := telemetryapp.InspectResult{SchemaVersion: 1, Status: telemetryapp.InspectUnavailable, OperationID: req.TelemetryInspect.OperationID}
		return bridge.Response{Telemetry: &result}, nil
	case "repro.create":
		capsule := reprocore.Capsule{ReproID: "repro_01K00000000000000000000000", CreateID: req.ReproCreate.CreateID}
		return bridge.Response{Capsule: &capsule}, nil
	case "inspect.repro":
		result := reproapp.InspectResult{SchemaVersion: 1, Capsule: reprocore.Capsule{ReproID: req.ReproID}}
		return bridge.Response{Repro: &result}, nil
	case "inspect.server":
		catalog := capability.Baseline(capability.Limits{})
		return bridge.Response{Server: &catalog}, nil
	}
	return bridge.Response{}, nil
}

func TestTelemetryAndReproMCPV2UseOnlyLocalShellAndNeverStart(t *testing.T) {
	client := &telemetryReproClient{}
	session, closeSession := currentSession(t, New(bridge.New(client), capability.Baseline(capability.Limits{})))
	defer closeSession()
	cases := []struct{ raw, action string }{
		{`{"action":"inspect.telemetry","operation_id":"op-1","max_samples":16}`, "inspect.telemetry"},
		{`{"action":"repro.create","repro_create_id":"repro-create-1","operation_id":"op-1"}`, "repro.create"},
		{`{"action":"inspect.repro","repro_id":"repro_01K00000000000000000000000"}`, "inspect.repro"},
	}
	for _, tc := range cases {
		res, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(tc.raw)})
		if err != nil || res.IsError || client.last.Action != tc.action {
			t.Fatalf("%s result=%#v req=%#v err=%v", tc.action, res, client.last, err)
		}
		if client.startCalls != 0 {
			t.Fatalf("%s invoked start", tc.action)
		}
	}
	if client.last.Action != "inspect.repro" {
		t.Fatalf("last=%#v", client.last)
	}
}

func TestTelemetryAndReproMCPV2FailClosedBeforeDispatch(t *testing.T) {
	client := &telemetryReproClient{}
	session, closeSession := currentSession(t, New(bridge.New(client), capability.Baseline(capability.Limits{})))
	defer closeSession()
	for _, raw := range []string{
		`{"action":"inspect.telemetry","operation_id":"op-1","max_samples":0}`,
		`{"action":"inspect.telemetry","operation_id":"op-1","max_samples":129}`,
		`{"action":"repro.create","repro_create_id":"repro-create-1","operation_id":"op-1","capture_policy":{"dependent_derivations":"future"}}`,
		`{"action":"repro.create","repro_create_id":"repro-create-1","operation_id":"op-1","environment":{"PATH":"secret"}}`,
		`{"action":"inspect.repro","repro_id":"bad"}`,
	} {
		before := client.dispatches
		res, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(raw)})
		if err != nil || !res.IsError {
			t.Fatalf("invalid accepted raw=%s result=%#v err=%v", raw, res, err)
		}
		if client.dispatches != before {
			t.Fatalf("invalid request dispatched raw=%s last=%#v", raw, client.last)
		}
	}
}
