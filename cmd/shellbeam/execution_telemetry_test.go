package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	telemetryapp "github.com/maemreyo/shellbeam/internal/app/telemetry"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	observation "github.com/maemreyo/shellbeam/internal/core/observation"
	core "github.com/maemreyo/shellbeam/internal/core/telemetry"
)

func TestDaemonCatalogAdvertisesBoundedTelemetryWithoutResourceOverclaim(t *testing.T) {
	catalog := daemonCatalog(capability.Limits{})
	if catalog.Features[capability.FeatureExecutionTelemetry] != capability.Available {
		t.Fatalf("telemetry feature=%q", catalog.Features[capability.FeatureExecutionTelemetry])
	}
	if len(catalog.TelemetrySchemaVersions) != 1 || catalog.TelemetrySchemaVersions[0] != 1 {
		t.Fatalf("telemetry schemas=%v", catalog.TelemetrySchemaVersions)
	}
	if catalog.Limits.TelemetryMaxSamples < 1 || catalog.Limits.TelemetryMetadataBytes < 1 || catalog.Limits.TelemetryMaxKeys < 1 || catalog.Limits.TelemetryMaxKeysPerRepository < 1 || catalog.Limits.TelemetryMaxSamplesPerKey < 1 || catalog.Limits.TelemetryRetentionAgeMS < 1 || catalog.Limits.TelemetryInspectSamples != telemetryapp.MaxInspectSamples {
		t.Fatalf("telemetry limits=%#v", catalog.Limits)
	}
	if catalog.ResourceObservation == nil || catalog.ResourceObservation.CPUTime != capability.ResourcePlatformReported || catalog.ResourceObservation.MaxRSS != capability.ResourcePlatformReported || catalog.ResourceObservation.IOBytes != capability.ResourceUnavailable || catalog.ResourceObservation.ProcessCountPeak != capability.ResourceSampled {
		t.Fatalf("resource observation overclaimed=%#v", catalog.ResourceObservation)
	}
}

func TestTelemetryDaemonCollectsOnlyAfterTerminalAndRestartDoesNotDuplicate(t *testing.T) {
	stateDir, runtimeDir := a1RuntimeDirs(t)
	client, stop := startExecutionObservationDaemon(t, stateDir, runtimeDir)

	server, err := client.CallV2(context.Background(), ipcadapter.RequestV2{IPVersion: 2, Kind: "request", RequestID: "telemetry-server", Action: "inspect.server"})
	if err != nil || !server.OK || server.Server == nil || server.Server.Features[capability.FeatureExecutionTelemetry] != capability.Available {
		stop()
		t.Fatalf("inspect.server=%#v err=%v", server, err)
	}
	start, err := client.CallV2(context.Background(), ipcadapter.RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "telemetry-start", Action: "start",
		OperationID: "telemetry-daemon-sample", CWD: "/tmp", Argv: []string{"/bin/sh", "-c", "sleep 0.5"}, YieldMS: 0, MaxOutputBytes: 4096,
	})
	if err != nil || !start.OK || start.Result == nil || start.Result.Operation.State == "terminal" {
		stop()
		t.Fatalf("start=%#v err=%v", start, err)
	}

	store := openA1Store(t, stateDir)
	if _, found, err := store.FindPerformanceByOperation(context.Background(), "telemetry-daemon-sample"); err != nil || found {
		stop()
		t.Fatalf("pre-terminal telemetry found=%v err=%v", found, err)
	}
	// Read-only/ordinary observation paths must not cause telemetry derivation.
	if _, err := client.CallV2(context.Background(), ipcadapter.RequestV2{IPVersion: 2, Kind: "request", RequestID: "telemetry-events", Action: "inspect.events", Target: &observation.Target{Kind: observation.TargetOperation, OperationID: "telemetry-daemon-sample"}, MaxEvents: 16}); err != nil {
		stop()
		t.Fatal(err)
	}
	if _, err := client.CallV2(context.Background(), ipcadapter.RequestV2{IPVersion: 2, Kind: "request", RequestID: "telemetry-structured", Action: "inspect.structured", OperationID: "telemetry-daemon-sample", MaxRecords: 16}); err != nil {
		stop()
		t.Fatal(err)
	}
	if _, found, err := store.FindPerformanceByOperation(context.Background(), "telemetry-daemon-sample"); err != nil || found {
		stop()
		t.Fatalf("inspect path created pre-terminal telemetry found=%v err=%v", found, err)
	}

	terminal := pollA22Terminal(t, client, "telemetry-daemon-sample", start.Result.Operation.SessionID)
	assertA1ChildSuccess(t, terminal)
	record := waitDaemonTelemetry(t, store, "telemetry-daemon-sample")
	for name, metric := range map[string]core.IntMetric{"cpu_user": record.Resources.CPUUserMS, "cpu_system": record.Resources.CPUSystemMS, "max_rss": record.Resources.MaxRSSBytes, "process_peak": record.Resources.ProcessCountPeak} {
		if metric.Quality == core.MetricUnavailable || metric.Value == nil || *metric.Value < 0 {
			stop()
			t.Fatalf("daemon resource %s unavailable: %#v", name, metric)
		}
	}
	if record.Resources.ReadBytes.Quality != core.MetricUnavailable || record.Resources.WriteBytes.Quality != core.MetricUnavailable {
		stop()
		t.Fatalf("daemon I/O bytes overclaimed: %#v", record.Resources)
	}
	waitDaemonTelemetryEvent(t, client, "telemetry-daemon-sample")
	key, err := core.CompatibilityKey(record)
	if err != nil {
		stop()
		t.Fatal(err)
	}
	available, err := store.CountCompatiblePerformance(context.Background(), key)
	if err != nil || available != 1 {
		stop()
		t.Fatalf("compatible samples=%d err=%v", available, err)
	}
	stop()

	// Inspect the persisted history with the application service before restart.
	inspector, err := telemetryapp.New(store)
	if err != nil {
		t.Fatal(err)
	}
	view, err := inspector.Inspect(context.Background(), telemetryapp.InspectRequest{OperationID: "telemetry-daemon-sample", MaxSamples: telemetryapp.MaxInspectSamples})
	if err != nil || view.Status != telemetryapp.InspectAvailable || view.SamplesAvailable != 1 || view.SamplesReturned != 1 {
		t.Fatalf("persisted telemetry view=%#v err=%v", view, err)
	}

	restartRuntime := filepath.Join(filepath.Dir(runtimeDir), "run-restart")
	_, stopRestart := startExecutionObservationDaemon(t, stateDir, restartRuntime)
	time.Sleep(150 * time.Millisecond)
	stopRestart()

	reopened := openA1Store(t, stateDir)
	after, found, err := reopened.FindPerformanceByOperation(context.Background(), "telemetry-daemon-sample")
	if err != nil || !found || after.DerivationKey != record.DerivationKey {
		t.Fatalf("restart telemetry=%#v found=%v err=%v", after, found, err)
	}
	afterKey, _ := core.CompatibilityKey(after)
	available, err = reopened.CountCompatiblePerformance(context.Background(), afterKey)
	if err != nil || available != 1 {
		t.Fatalf("restart duplicated telemetry samples=%d err=%v", available, err)
	}
}

func waitDaemonTelemetry(t *testing.T, store *storeadapter.Repository, operationID string) core.PerformanceRecord {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		record, found, err := store.FindPerformanceByOperation(context.Background(), operationID)
		if err != nil {
			t.Fatal(err)
		}
		if found {
			return record
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("telemetry %s did not become durable", operationID)
	return core.PerformanceRecord{}
}

func waitDaemonTelemetryEvent(t *testing.T, client *ipcadapter.Client, operationID string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.CallV2(context.Background(), ipcadapter.RequestV2{
			IPVersion: 2, Kind: "request", RequestID: "telemetry-event-wait", Action: "inspect.events",
			Target: &observation.Target{Kind: observation.TargetOperation, OperationID: operationID}, MaxEvents: 16,
		})
		if err != nil {
			t.Fatal(err)
		}
		if response.OK && response.Events != nil {
			for _, event := range response.Events.Events {
				if event.Kind == observation.EventTelemetryChanged {
					return
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("telemetry event %s did not materialize", operationID)
}
