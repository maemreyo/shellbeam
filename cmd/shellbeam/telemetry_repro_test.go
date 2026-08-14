package main

import (
	"context"
	"reflect"
	"testing"
	"time"

	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	telemetryapp "github.com/maemreyo/shellbeam/internal/app/telemetry"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	observation "github.com/maemreyo/shellbeam/internal/core/observation"
	reprocore "github.com/maemreyo/shellbeam/internal/core/repro"
)

func TestA4DaemonComposesTelemetryAndExactlyOnceReproWithoutChangingChildTruth(t *testing.T) {
	stateDir, runtimeDir := a1RuntimeDirs(t)
	client, stop := startExecutionObservationDaemon(t, stateDir, runtimeDir)
	defer stop()

	server, err := client.CallV2(context.Background(), ipcadapter.RequestV2{IPVersion: 2, Kind: "request", RequestID: "a4-server", Action: "inspect.server"})
	if err != nil || !server.OK || server.Server == nil {
		t.Fatalf("inspect.server=%#v err=%v", server, err)
	}
	if server.Server.Features[capability.FeatureExecutionTelemetry] != capability.Available || server.Server.Features[capability.FeatureReproductionCapsules] != capability.Available {
		t.Fatalf("A4 capability catalog=%#v", server.Server)
	}

	start, err := client.CallV2(context.Background(), ipcadapter.RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "a4-start", Action: "start",
		OperationID: "a4-repro-daemon", CWD: "/tmp", Argv: []string{"/bin/sh", "-c", "sleep 0.35; printf a4-ok"}, YieldMS: 0, MaxOutputBytes: 4096,
	})
	if err != nil || !start.OK || start.Result == nil || start.Result.Operation.State == "terminal" {
		t.Fatalf("start=%#v err=%v", start, err)
	}

	preTelemetry, err := client.CallV2(context.Background(), ipcadapter.RequestV2{IPVersion: 2, Kind: "request", RequestID: "a4-pre-telemetry", Action: "inspect.telemetry", OperationID: "a4-repro-daemon", MaxSamples: telemetryapp.MaxInspectSamples})
	if err != nil || !preTelemetry.OK || preTelemetry.Telemetry == nil || preTelemetry.Telemetry.Status != telemetryapp.InspectUnavailable || preTelemetry.Telemetry.SamplesAvailable != 0 {
		t.Fatalf("pre-terminal telemetry=%#v err=%v", preTelemetry, err)
	}
	preRepro, err := client.CallV2(context.Background(), ipcadapter.RequestV2{IPVersion: 2, Kind: "request", RequestID: "a4-pre-repro", Action: "repro.create", ReproCreateID: "a4-repro-create-pre", OperationID: "a4-repro-daemon", CapturePolicy: &reprocore.CapturePolicy{DependentDerivations: reprocore.CaptureCurrent}})
	if err != nil {
		t.Fatal(err)
	}
	if preRepro.OK || preRepro.Error == nil || preRepro.Error.Code != "repro_materialization_unavailable" {
		t.Fatalf("pre-terminal repro should fail only explicit caller: %#v", preRepro)
	}

	terminal := pollA22Terminal(t, client, "a4-repro-daemon", start.Result.Operation.SessionID)
	assertA1ChildSuccess(t, terminal)

	telemetry := waitA4TelemetryIPC(t, client, "a4-repro-daemon")
	if telemetry.SamplesAvailable != 1 || telemetry.SamplesReturned != 1 || telemetry.Latest == nil || telemetry.Latest.OperationID != "a4-repro-daemon" {
		t.Fatalf("telemetry=%#v", telemetry)
	}

	createRequest := ipcadapter.RequestV2{IPVersion: 2, Kind: "request", RequestID: "a4-repro-create-1", Action: "repro.create", ReproCreateID: "a4-repro-create-1", OperationID: "a4-repro-daemon", CapturePolicy: &reprocore.CapturePolicy{DependentDerivations: reprocore.CaptureCurrent}}
	created, err := client.CallV2(context.Background(), createRequest)
	if err != nil || !created.OK || created.Capsule == nil {
		t.Fatalf("repro.create=%#v err=%v", created, err)
	}
	if created.Capsule.Execution.OperationID != "a4-repro-daemon" || created.Capsule.ReproID == "" || created.Capsule.CaptureCutDigest == "" {
		t.Fatalf("capsule=%#v", created.Capsule)
	}

	createRequest.RequestID = "a4-repro-create-retry"
	retry, err := client.CallV2(context.Background(), createRequest)
	if err != nil || !retry.OK || retry.Capsule == nil || !reflect.DeepEqual(*created.Capsule, *retry.Capsule) {
		t.Fatalf("retry mutated exactly-once capsule\ncreated=%#v\nretry=%#v err=%v", created.Capsule, retry.Capsule, err)
	}
	inspected, err := client.CallV2(context.Background(), ipcadapter.RequestV2{IPVersion: 2, Kind: "request", RequestID: "a4-repro-inspect", Action: "inspect.repro", ReproID: created.Capsule.ReproID})
	if err != nil || !inspected.OK || inspected.Repro == nil || !reflect.DeepEqual(inspected.Repro.Capsule, *created.Capsule) {
		t.Fatalf("inspect.repro=%#v err=%v", inspected, err)
	}

	telemetryEvents, reproEvents := waitA4ObservationEvents(t, client, "a4-repro-daemon")
	if telemetryEvents != 1 || reproEvents != 1 {
		t.Fatalf("A4 event counts telemetry=%d repro=%d", telemetryEvents, reproEvents)
	}
}

func waitA4TelemetryIPC(t *testing.T, client *ipcadapter.Client, operationID string) telemetryapp.InspectResult {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.CallV2(context.Background(), ipcadapter.RequestV2{IPVersion: 2, Kind: "request", RequestID: "a4-telemetry-wait", Action: "inspect.telemetry", OperationID: operationID, MaxSamples: telemetryapp.MaxInspectSamples})
		if err != nil {
			t.Fatal(err)
		}
		if response.OK && response.Telemetry != nil && response.Telemetry.Status == telemetryapp.InspectAvailable {
			return *response.Telemetry
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("telemetry %s did not become available", operationID)
	return telemetryapp.InspectResult{}
}

func waitA4ObservationEvents(t *testing.T, client *ipcadapter.Client, operationID string) (int, int) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.CallV2(context.Background(), ipcadapter.RequestV2{IPVersion: 2, Kind: "request", RequestID: "a4-events-wait", Action: "inspect.events", Target: &observation.Target{Kind: observation.TargetOperation, OperationID: operationID}, MaxEvents: 64})
		if err != nil {
			t.Fatal(err)
		}
		telemetryEvents, reproEvents := 0, 0
		if response.OK && response.Events != nil {
			for _, event := range response.Events.Events {
				switch event.Kind {
				case observation.EventTelemetryChanged:
					telemetryEvents++
				case observation.EventReproRecorded:
					reproEvents++
				}
			}
		}
		if telemetryEvents >= 1 && reproEvents >= 1 {
			return telemetryEvents, reproEvents
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("A4 events %s did not materialize", operationID)
	return 0, 0
}
