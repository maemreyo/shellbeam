//go:build linux || darwin

package ipc

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	reproapp "github.com/maemreyo/shellbeam/internal/app/repro"
	telemetryapp "github.com/maemreyo/shellbeam/internal/app/telemetry"
	reprocore "github.com/maemreyo/shellbeam/internal/core/repro"
)

type telemetryReproActions struct {
	fakeActions
	startCalls   int
	telemetryReq telemetryapp.InspectRequest
	reproCreate  reprocore.CreateRequest
	reproInspect string
}

func (a *telemetryReproActions) Start(ctx context.Context, req daemonapp.StartRequest) (daemonapp.View, error) {
	a.startCalls++
	return a.fakeActions.Start(ctx, req)
}
func (a *telemetryReproActions) InspectTelemetry(_ context.Context, req telemetryapp.InspectRequest) (telemetryapp.InspectResult, error) {
	a.telemetryReq = req
	return telemetryapp.InspectResult{SchemaVersion: 1, Status: telemetryapp.InspectUnavailable, OperationID: req.OperationID}, nil
}
func (a *telemetryReproActions) CreateRepro(_ context.Context, req reprocore.CreateRequest) (reprocore.Capsule, error) {
	a.reproCreate = req
	return reprocore.Capsule{ReproID: "repro_01K00000000000000000000000", CreateID: req.CreateID}, nil
}
func (a *telemetryReproActions) InspectRepro(_ context.Context, reproID string) (reproapp.InspectResult, error) {
	a.reproInspect = reproID
	return reproapp.InspectResult{SchemaVersion: 1, Capsule: reprocore.Capsule{ReproID: reproID}}, nil
}

func TestTelemetryAndReproIPCV2ForwardClosedActionsWithoutSpawn(t *testing.T) {
	actions := &telemetryReproActions{}
	runtime, err := os.MkdirTemp("/tmp", "shellbeam-a4-ipc-")
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
	client := NewClient(server.SocketPath())

	telemetryOut, err := client.CallV2(context.Background(), RequestV2{IPVersion: 2, Kind: "request", RequestID: "t", Action: "inspect.telemetry", OperationID: "op-1", MaxSamples: 16})
	if err != nil || !telemetryOut.OK || telemetryOut.Telemetry == nil || actions.telemetryReq.OperationID != "op-1" || actions.telemetryReq.MaxSamples != 16 {
		t.Fatalf("telemetry=%#v req=%#v err=%v", telemetryOut, actions.telemetryReq, err)
	}
	createOut, err := client.CallV2(context.Background(), RequestV2{IPVersion: 2, Kind: "request", RequestID: "r", Action: "repro.create", ReproCreateID: "repro-create-1", OperationID: "op-1", CapturePolicy: &reprocore.CapturePolicy{DependentDerivations: reprocore.CaptureCurrent}})
	if err != nil || !createOut.OK || createOut.Capsule == nil || actions.reproCreate.CreateID != "repro-create-1" || actions.reproCreate.Policy.DependentDerivations != reprocore.CaptureCurrent {
		t.Fatalf("create=%#v req=%#v err=%v", createOut, actions.reproCreate, err)
	}
	inspectOut, err := client.CallV2(context.Background(), RequestV2{IPVersion: 2, Kind: "request", RequestID: "i", Action: "inspect.repro", ReproID: "repro_01K00000000000000000000000"})
	if err != nil || !inspectOut.OK || inspectOut.Repro == nil || actions.reproInspect != "repro_01K00000000000000000000000" {
		t.Fatalf("inspect=%#v id=%q err=%v", inspectOut, actions.reproInspect, err)
	}
	if actions.startCalls != 0 {
		t.Fatalf("A4 action spawned through start: %d", actions.startCalls)
	}
}

func TestTelemetryAndReproIPCV2RejectInvalidBoundsPolicyIDsAndCrossActionFields(t *testing.T) {
	for _, raw := range []string{
		`{"ipc_version":2,"kind":"request","request_id":"x","action":"inspect.telemetry","operation_id":"op-1","max_samples":0}`,
		`{"ipc_version":2,"kind":"request","request_id":"x","action":"inspect.telemetry","operation_id":"op-1","max_samples":129}`,
		`{"ipc_version":2,"kind":"request","request_id":"x","action":"inspect.telemetry","operation_id":"op-1","max_samples":1,"command":"env"}`,
		`{"ipc_version":2,"kind":"request","request_id":"x","action":"repro.create","repro_create_id":"repro-create-1","operation_id":"op-1","capture_policy":{"dependent_derivations":"future"}}`,
		`{"ipc_version":2,"kind":"request","request_id":"x","action":"repro.create","repro_create_id":"repro-create-1","operation_id":"op-1","stdin":"secret"}`,
		`{"ipc_version":2,"kind":"request","request_id":"x","action":"inspect.repro","repro_id":"../bad"}`,
	} {
		if _, err := decodeRequestV2(bytesReaderV2([]byte(raw))); err == nil {
			t.Fatalf("accepted invalid %s", raw)
		}
	}
}
