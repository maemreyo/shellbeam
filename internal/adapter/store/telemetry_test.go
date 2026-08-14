package store

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/observation"
	"github.com/maemreyo/shellbeam/internal/core/session"
	core "github.com/maemreyo/shellbeam/internal/core/telemetry"
)

func TestTelemetryStoreIsIdempotentConflictClosedAndObservableOnce(t *testing.T) {
	r := openTelemetryRepository(t, telemetryTestLimits())
	record := telemetryRecord(t, "op-1", "session-1", strings.Repeat("c", 64), time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC), session.Success)
	if err := r.PutPerformanceRecord(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	if err := r.PutPerformanceRecord(context.Background(), record); err != nil {
		t.Fatalf("exact replay: %v", err)
	}
	got, err := r.GetPerformanceRecord(context.Background(), record.DerivationKey)
	if err != nil || got.DerivationKey != record.DerivationKey || got.WallMS != record.WallMS {
		t.Fatalf("record=%#v err=%v", got, err)
	}
	high, err := r.ObservationHighWatermark(context.Background())
	if err != nil || high != 1 {
		t.Fatalf("high=%d err=%v", high, err)
	}
	obligations, err := r.ListObservationObligations(context.Background(), 0, 10)
	if err != nil || len(obligations) != 1 || obligations[0].Kind != observation.EventTelemetryChanged || obligations[0].State != observation.ObligationCommitted {
		t.Fatalf("obligations=%#v err=%v", obligations, err)
	}
	if obligations[0].Correlation.OperationID != record.OperationID || obligations[0].Correlation.SessionID != record.SessionID || obligations[0].Correlation.RepositoryID != record.RepositoryID || obligations[0].Correlation.WorkspaceID != record.WorkspaceID {
		t.Fatalf("correlation=%#v", obligations[0].Correlation)
	}
	conflict := record
	conflict.WallMS++
	if err := r.PutPerformanceRecord(context.Background(), conflict); err == nil || !strings.Contains(err.Error(), "telemetry_record_conflict") {
		t.Fatalf("conflicting replay err=%v", err)
	}
	if replayHigh, _ := r.ObservationHighWatermark(context.Background()); replayHigh != high {
		t.Fatalf("conflict/replay allocated observation: %d -> %d", high, replayHigh)
	}
}

func TestTelemetryStoreFindsLatestOperationAndOnlyCompatibleHistory(t *testing.T) {
	limits := telemetryTestLimits()
	limits.MaxTelemetrySamplesPerKey = 8
	r := openTelemetryRepository(t, limits)
	base := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	first := telemetryRecord(t, "op-1", "session-1", strings.Repeat("c", 64), base, session.Success)
	second := telemetryRecord(t, "op-2", "session-2", strings.Repeat("c", 64), base.Add(time.Second), session.Failure)
	changed := telemetryRecord(t, "op-3", "session-3", strings.Repeat("d", 64), base.Add(2*time.Second), session.Success)
	for _, record := range []core.PerformanceRecord{first, second, changed} {
		if err := r.PutPerformanceRecord(context.Background(), record); err != nil {
			t.Fatal(err)
		}
	}
	found, ok, err := r.FindPerformanceByOperation(context.Background(), "op-2")
	if err != nil || !ok || found.DerivationKey != second.DerivationKey {
		t.Fatalf("found=%#v ok=%v err=%v", found, ok, err)
	}
	if _, ok, err := r.FindPerformanceByOperation(context.Background(), "op-missing"); err != nil || ok {
		t.Fatalf("missing ok=%v err=%v", ok, err)
	}
	key, err := core.CompatibilityKey(first)
	if err != nil {
		t.Fatal(err)
	}
	history, err := r.ListCompatiblePerformance(context.Background(), key, 8)
	if err != nil || len(history) != 2 {
		t.Fatalf("history=%#v err=%v", history, err)
	}
	available, err := r.CountCompatiblePerformance(context.Background(), key)
	if err != nil || available != 2 {
		t.Fatalf("available=%d err=%v", available, err)
	}
	if history[0].DerivationKey != second.DerivationKey || history[1].DerivationKey != first.DerivationKey {
		t.Fatalf("history not newest-first: %#v", history)
	}
	for _, record := range history {
		if record.CommandSemanticsFingerprint != first.CommandSemanticsFingerprint {
			t.Fatalf("incompatible record leaked: %#v", record)
		}
	}
	if _, err := r.ListCompatiblePerformance(context.Background(), key, 0); err == nil {
		t.Fatal("zero history limit accepted")
	}
}

func TestTelemetryRetentionPerKeyIsChronologicalNotOutcomeBiased(t *testing.T) {
	limits := telemetryTestLimits()
	limits.MaxTelemetrySamplesPerKey = 2
	limits.MaxTelemetrySamples = 8
	r := openTelemetryRepository(t, limits)
	base := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	oldFailure := telemetryRecord(t, "op-old", "session-old", strings.Repeat("c", 64), base, session.Failure)
	middleSuccess := telemetryRecord(t, "op-mid", "session-mid", strings.Repeat("c", 64), base.Add(time.Second), session.Success)
	newTimeout := telemetryRecord(t, "op-new", "session-new", strings.Repeat("c", 64), base.Add(2*time.Second), session.Timeout)
	for _, record := range []core.PerformanceRecord{oldFailure, middleSuccess, newTimeout} {
		if err := r.PutPerformanceRecord(context.Background(), record); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.GetPerformanceRecord(context.Background(), oldFailure.DerivationKey); !errors.Is(err, ErrNotFound) {
		t.Fatalf("oldest failure was not chronologically evicted: %v", err)
	}
	key, _ := core.CompatibilityKey(middleSuccess)
	history, err := r.ListCompatiblePerformance(context.Background(), key, 8)
	if err != nil || len(history) != 2 || history[0].TerminalOutcome != session.Timeout || history[1].TerminalOutcome != session.Success {
		t.Fatalf("retained history=%#v err=%v", history, err)
	}
}

func TestTelemetryRetentionBoundsDistinctKeysGlobalSamplesBytesAndAge(t *testing.T) {
	limits := telemetryTestLimits()
	limits.MaxTelemetrySamples = 3
	limits.MaxTelemetryKeys = 2
	limits.MaxTelemetryKeysPerRepository = 2
	limits.MaxTelemetrySamplesPerKey = 3
	limits.MaxTelemetryAge = 24 * time.Hour
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	r := openTelemetryRepository(t, limits)
	r.now = func() time.Time { return now }

	old := telemetryRecord(t, "op-old", "session-old", strings.Repeat("a", 64), now.Add(-48*time.Hour), session.Success)
	keyB := telemetryRecord(t, "op-b", "session-b", strings.Repeat("b", 64), now.Add(-2*time.Hour), session.Success)
	keyC := telemetryRecord(t, "op-c", "session-c", strings.Repeat("c", 64), now.Add(-time.Hour), session.Failure)
	for _, record := range []core.PerformanceRecord{old, keyB, keyC} {
		if err := r.PutPerformanceRecord(context.Background(), record); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.GetPerformanceRecord(context.Background(), old.DerivationKey); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired sample retained: %v", err)
	}

	keyD := telemetryRecord(t, "op-d", "session-d", strings.Repeat("d", 64), now, session.Timeout)
	if err := r.PutPerformanceRecord(context.Background(), keyD); err != nil {
		t.Fatal(err)
	}
	if _, err := r.GetPerformanceRecord(context.Background(), keyB.DerivationKey); !errors.Is(err, ErrNotFound) {
		t.Fatalf("least-recent distinct key was not evicted: %v", err)
	}
	entries, err := r.telemetryEntriesLocked()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) > limits.MaxTelemetrySamples {
		t.Fatalf("sample cap exceeded: %d", len(entries))
	}
	keys := map[string]bool{}
	var bytes int64
	for _, entry := range entries {
		keys[entry.compatibilityKey] = true
		bytes += entry.size
	}
	if len(keys) > limits.MaxTelemetryKeys || bytes > limits.MaxTelemetryBytes {
		t.Fatalf("retention bounds keys=%d bytes=%d limits=%#v", len(keys), bytes, limits)
	}
}

func TestTelemetryRetentionHonorsMetadataByteCeiling(t *testing.T) {
	limits := telemetryTestLimits()
	base := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	first := telemetryRecord(t, "op-byte-1", "session-byte-1", strings.Repeat("c", 64), base, session.Success)
	second := telemetryRecord(t, "op-byte-2", "session-byte-2", strings.Repeat("c", 64), base.Add(time.Second), session.Success)
	encoded, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	limits.MaxTelemetryBytes = int64(len(encoded)+1)*2 - 1
	limits.MaxTelemetrySamples = 8
	limits.MaxTelemetrySamplesPerKey = 8
	r := openTelemetryRepository(t, limits)
	if err := r.PutPerformanceRecord(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if err := r.PutPerformanceRecord(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	if _, err := r.GetPerformanceRecord(context.Background(), first.DerivationKey); !errors.Is(err, ErrNotFound) {
		t.Fatalf("byte ceiling did not evict oldest: %v", err)
	}
	if _, err := r.GetPerformanceRecord(context.Background(), second.DerivationKey); err != nil {
		t.Fatalf("new sample missing: %v", err)
	}
}

func TestTelemetryPreparedObservationGapReconcilesAfterRestart(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	limits := telemetryTestLimits()
	r := openTelemetryRepositoryAt(t, root, limits)
	record := telemetryRecord(t, "op-gap", "session-gap", strings.Repeat("c", 64), time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC), session.Success)
	r.writer = failNthAtomicWriter("replace.rename", 1)
	if err := r.PutPerformanceRecord(context.Background(), record); err != nil {
		t.Fatalf("canonical telemetry write failed: %v", err)
	}
	listed, err := r.ListObservationObligations(context.Background(), 0, 10)
	if err != nil || len(listed) != 1 || listed[0].State != observation.ObligationPrepared {
		t.Fatalf("before restart=%#v err=%v", listed, err)
	}
	r = openTelemetryRepositoryAt(t, root, limits)
	if err := r.AbandonUnresolved(context.Background(), "restart"); err != nil {
		t.Fatal(err)
	}
	listed, err = r.ListObservationObligations(context.Background(), 0, 10)
	if err != nil || len(listed) != 1 || listed[0].State != observation.ObligationCommitted {
		t.Fatalf("after restart=%#v err=%v", listed, err)
	}
}

func openTelemetryRepository(t *testing.T, limits Limits) *Repository {
	t.Helper()
	return openTelemetryRepositoryAt(t, filepath.Join(t.TempDir(), "state"), limits)
}

func openTelemetryRepositoryAt(t *testing.T, root string, limits Limits) *Repository {
	t.Helper()
	r, err := Open(root, limits)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func telemetryTestLimits() Limits {
	return Limits{
		MaxSessions: 8, MaxSessionOutput: 1 << 20, MaxTotalState: 16 << 20, ControlReserve: 1024,
		MaxTelemetrySamples: 8, MaxTelemetryBytes: 2 << 20, MaxTelemetryKeys: 8,
		MaxTelemetryKeysPerRepository: 8, MaxTelemetrySamplesPerKey: 8, MaxTelemetryAge: 7 * 24 * time.Hour,
	}
}

func telemetryRecord(t *testing.T, operationID, sessionID, semantics string, capturedAt time.Time, outcome session.Outcome) core.PerformanceRecord {
	t.Helper()
	producer := core.Producer{ProducerID: "shellbeam.telemetry", ProducerVersion: 1, CapabilityVersion: 1}
	receiptDigest := digestForID(operationID)
	configDigest := strings.Repeat("b", 64)
	key, err := core.DerivationKey(receiptDigest, producer, 1, configDigest)
	if err != nil {
		t.Fatal(err)
	}
	unavailable := core.IntMetric{Quality: core.MetricUnavailable}
	record := core.PerformanceRecord{
		SchemaVersion: core.SchemaVersion, DerivationKey: key, DerivationSchemaVersion: 1, DerivationConfigDigest: configDigest,
		Producer: producer, OperationID: operationID, SessionID: sessionID, ReceiptDigest: receiptDigest,
		RepositoryID: "repo_01K00000000000000000000000", WorkspaceID: "ws_01K00000000000000000000000", ActivityID: "activity-1",
		CommandSemanticsFingerprint: semantics, ScopeClass: core.ScopeArgv, Platform: "darwin", Architecture: "arm64",
		WallMS: 100, OutputBytes: 10, TerminalOutcome: outcome, TimedOut: outcome == session.Timeout, CapturedAt: capturedAt,
		Lifecycle: core.LifecycleTerminal, Completeness: core.CompletenessPartial,
		Resources: core.ResourceMetrics{CPUUserMS: unavailable, CPUSystemMS: unavailable, MaxRSSBytes: unavailable, ReadBytes: unavailable, WriteBytes: unavailable, ProcessCountPeak: unavailable},
	}
	if err := record.Validate(); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	return record
}

func digestForID(value string) string {
	const hexDigits = "0123456789abcdef"
	var b strings.Builder
	for i := 0; i < 64; i++ {
		b.WriteByte(hexDigits[(int(value[i%len(value)])+i)%len(hexDigits)])
	}
	return b.String()
}
