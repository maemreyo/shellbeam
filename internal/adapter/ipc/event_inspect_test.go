package ipc

import (
	"context"
	"errors"
	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	daemon "github.com/maemreyo/shellbeam/internal/app/daemon"
	"os"
	"strings"
	"testing"

	observationapp "github.com/maemreyo/shellbeam/internal/app/observation"
	core "github.com/maemreyo/shellbeam/internal/core/observation"
)

func TestEventInspectBridgeRequestRoundTripsToV2(t *testing.T) {
	bridgeReq := bridge.Request{ProtocolVersion: 2, Action: "inspect.events", EventInspect: observationapp.InspectRequest{Target: core.Target{Kind: core.TargetOperation, OperationID: "op-1"}, AfterEventCursor: "evtcur_v1_abc.def", MaxEvents: 64}}
	req := requestV2FromBridge(bridgeReq)
	if err := validateRequestV2(req); err != nil {
		t.Fatal(err)
	}
	if req.Target == nil || req.Target.OperationID != "op-1" || req.AfterEventCursor != "evtcur_v1_abc.def" || req.MaxEvents != 64 {
		t.Fatalf("request=%#v", req)
	}
}

func TestEventInspectIPCV2NeverUsesExecutionActions(t *testing.T) {
	actions := &eventInspectActions{result: observationapp.InspectResult{Continuity: core.ContinuityComplete, NextEventCursor: "evtcur_v1_abc.def"}}
	resp := ResponseV2{IPVersion: 2, Kind: "response", RequestID: "events", Action: "inspect.events", OK: true, Events: &actions.result}
	if resp.Events == nil || resp.Events.Continuity != core.ContinuityComplete {
		t.Fatalf("response=%#v", resp)
	}
	got, err := actions.InspectEvents(context.Background(), observationapp.InspectRequest{Target: core.Target{Kind: core.TargetOperation, OperationID: "op-1"}, MaxEvents: 64})
	if err != nil || got.Continuity != core.ContinuityComplete || actions.startCalls != 0 {
		t.Fatalf("got=%#v starts=%d err=%v", got, actions.startCalls, err)
	}
}

type eventInspectActions struct {
	fakeActions
	result     observationapp.InspectResult
	startCalls int
}

func (a *eventInspectActions) Start(ctx context.Context, req daemon.StartRequest) (daemon.View, error) {
	a.startCalls++
	return a.fakeActions.Start(ctx, req)
}
func (a *eventInspectActions) InspectEvents(context.Context, observationapp.InspectRequest) (observationapp.InspectResult, error) {
	return a.result, nil
}

func TestEventInspectIPCV2DecodeRejectsCrossActionAndMalformedCursor(t *testing.T) {
	valid := []byte(`{"ipc_version":2,"kind":"request","request_id":"events","action":"inspect.events","target":{"kind":"operation","operation_id":"op-1"},"max_events":64}`)
	got, err := decodeRequestV2(bytesReaderV2(valid))
	if err != nil || got.Target == nil || got.Target.OperationID != "op-1" {
		t.Fatalf("decoded=%#v err=%v", got, err)
	}
	invalid := [][]byte{
		[]byte(`{"ipc_version":2,"kind":"request","request_id":"events","action":"inspect.events","target":{"kind":"operation","operation_id":"op-1"},"cursor":1,"max_events":64}`),
		[]byte(`{"ipc_version":2,"kind":"request","request_id":"events","action":"inspect.events","target":{"kind":"operation","operation_id":"op-1"},"after_event_cursor":"outcur_v1_bad","max_events":64}`),
		[]byte(`{"ipc_version":2,"kind":"request","request_id":"events","action":"inspect.events","target":{"kind":"operation","operation_id":"op-1"},"max_events":257}`),
	}
	for _, raw := range invalid {
		if _, err := decodeRequestV2(bytesReaderV2(raw)); err == nil {
			t.Fatalf("invalid request accepted: %s", raw)
		}
	}
}

func TestEventInspectIPCV2UsesEventActionsWithoutSpawn(t *testing.T) {
	actions := &eventInspectActions{result: observationapp.InspectResult{Continuity: core.ContinuityComplete, NextEventCursor: "evtcur_v1_abc.def"}}
	runtime, err := os.MkdirTemp("/tmp", "shellbeam-events-ipc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtime) })
	server, err := Listen(runtime, actions)
	if err != nil {
		if errors.Is(err, os.ErrPermission) || strings.Contains(err.Error(), "operation not permitted") {
			t.Skip("sandbox blocks Unix sockets")
		}
		t.Fatal(err)
	}
	defer server.Close()
	go server.Serve()
	got, err := NewClient(server.SocketPath()).CallV2(context.Background(), RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "events", Action: "inspect.events",
		Target: &core.Target{Kind: core.TargetOperation, OperationID: "op-1"}, MaxEvents: 64,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.OK || got.Events == nil || got.Events.Continuity != core.ContinuityComplete || actions.startCalls != 0 {
		t.Fatalf("response=%#v starts=%d", got, actions.startCalls)
	}
}
