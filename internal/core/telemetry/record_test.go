package telemetry

import (
	"strings"
	"testing"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/session"
)

func TestDerivationIdentityIsStableAndVersioned(t *testing.T) {
	producer := Producer{ProducerID: "shellbeam.telemetry", ProducerVersion: 1, CapabilityVersion: 1}
	receiptDigest := strings.Repeat("a", 64)
	configDigest := strings.Repeat("b", 64)
	first, err := DerivationKey(receiptDigest, producer, 1, configDigest)
	if err != nil {
		t.Fatal(err)
	}
	second, err := DerivationKey(receiptDigest, producer, 1, configDigest)
	if err != nil || first != second || len(first) != 64 {
		t.Fatalf("keys first=%q second=%q err=%v", first, second, err)
	}
	changedProducer := producer
	changedProducer.ProducerVersion++
	changed, err := DerivationKey(receiptDigest, changedProducer, 1, configDigest)
	if err != nil {
		t.Fatal(err)
	}
	if changed == first {
		t.Fatal("producer version did not change derivation key")
	}
	changed, err = DerivationKey(receiptDigest, producer, 2, configDigest)
	if err != nil {
		t.Fatal(err)
	}
	if changed == first {
		t.Fatal("schema version did not change derivation key")
	}
}

func TestPerformanceRecordKeepsExecutionSemanticsInCompatibilityKey(t *testing.T) {
	first := validPerformanceRecord()
	firstKey, err := CompatibilityKey(first)
	if err != nil {
		t.Fatal(err)
	}
	changedSemantics := first
	changedSemantics.CommandSemanticsFingerprint = strings.Repeat("d", 64)
	secondKey, err := CompatibilityKey(changedSemantics)
	if err != nil {
		t.Fatal(err)
	}
	if firstKey == secondKey {
		t.Fatal("changed execution semantics merged into one compatibility key")
	}
	changedLabel := first
	changedLabel.ProjectCommandID = "same-human-label-can-change"
	labelKey, err := CompatibilityKey(changedLabel)
	if err != nil {
		t.Fatal(err)
	}
	if labelKey != firstKey {
		t.Fatal("display-only project command id changed compatibility key")
	}
}

func TestPerformanceRecordCompatibilitySeparatesPlatformAndFingerprintSchemas(t *testing.T) {
	base := validPerformanceRecord()
	base.EnvironmentFingerprint = strings.Repeat("e", 64)
	base.EnvironmentSchemaVersion = 1
	base.ToolchainFingerprint = strings.Repeat("f", 64)
	base.ToolchainSchemaVersion = 1
	first, err := CompatibilityKey(base)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*PerformanceRecord){
		"platform":           func(v *PerformanceRecord) { v.Platform = "linux" },
		"architecture":       func(v *PerformanceRecord) { v.Architecture = "amd64" },
		"environment schema": func(v *PerformanceRecord) { v.EnvironmentSchemaVersion = 2 },
		"toolchain schema":   func(v *PerformanceRecord) { v.ToolchainSchemaVersion = 2 },
	} {
		t.Run(name, func(t *testing.T) {
			changed := base
			mutate(&changed)
			got, err := CompatibilityKey(changed)
			if err != nil {
				t.Fatal(err)
			}
			if got == first {
				t.Fatalf("%s did not split compatibility", name)
			}
		})
	}
}

func TestUnavailableResourceMetricCannotPretendZeroIsObserved(t *testing.T) {
	record := validPerformanceRecord()
	zero := int64(0)
	record.Resources.MaxRSSBytes = IntMetric{Quality: MetricUnavailable, Value: &zero}
	if err := record.Validate(); err == nil {
		t.Fatal("unavailable metric accepted a value")
	}
	record = validPerformanceRecord()
	record.Resources.CPUUserMS = IntMetric{Quality: MetricExact}
	if err := record.Validate(); err == nil {
		t.Fatal("observed metric without a value accepted")
	}
}

func TestPerformanceRecordRejectsInvalidIdentityAndCounters(t *testing.T) {
	for name, mutate := range map[string]func(*PerformanceRecord){
		"bad operation":                          func(v *PerformanceRecord) { v.OperationID = "../bad" },
		"bad receipt digest":                     func(v *PerformanceRecord) { v.ReceiptDigest = "bad" },
		"negative wall":                          func(v *PerformanceRecord) { v.WallMS = -1 },
		"timeout mismatch":                       func(v *PerformanceRecord) { v.TimedOut = true },
		"workspace without repository":           func(v *PerformanceRecord) { v.RepositoryID = "" },
		"fingerprint schema without fingerprint": func(v *PerformanceRecord) { v.EnvironmentSchemaVersion = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			got := validPerformanceRecord()
			mutate(&got)
			if err := got.Validate(); err == nil {
				t.Fatalf("invalid record accepted: %#v", got)
			}
		})
	}
}

func validPerformanceRecord() PerformanceRecord {
	unavailable := IntMetric{Quality: MetricUnavailable}
	producer := Producer{ProducerID: "shellbeam.telemetry", ProducerVersion: 1, CapabilityVersion: 1}
	receiptDigest := strings.Repeat("a", 64)
	configDigest := strings.Repeat("b", 64)
	key, _ := DerivationKey(receiptDigest, producer, 1, configDigest)
	return PerformanceRecord{
		SchemaVersion: SchemaVersion, DerivationKey: key, DerivationSchemaVersion: 1, DerivationConfigDigest: configDigest,
		Producer: producer, OperationID: "op-1", SessionID: "session-1", ReceiptDigest: receiptDigest,
		RepositoryID: "repo_01K00000000000000000000000", WorkspaceID: "ws_01K00000000000000000000000", ActivityID: "activity-1",
		ProjectCommandID: "test_full", CommandSemanticsFingerprint: strings.Repeat("c", 64), ScopeClass: ScopeArgv,
		Platform: "darwin", Architecture: "arm64", WallMS: 250, OutputBytes: 1234, InputAcceptedBytes: 4, InputDeliveredBytes: 4,
		TerminalOutcome: session.Success, TimedOut: false, CapturedAt: time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC),
		Lifecycle: LifecycleTerminal, Completeness: CompletenessPartial,
		Resources: ResourceMetrics{
			CPUUserMS: unavailable, CPUSystemMS: unavailable, MaxRSSBytes: unavailable,
			ReadBytes: unavailable, WriteBytes: unavailable, ProcessCountPeak: unavailable,
		},
	}
}
