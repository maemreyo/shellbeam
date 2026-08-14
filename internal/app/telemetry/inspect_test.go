package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/session"
	core "github.com/maemreyo/shellbeam/internal/core/telemetry"
)

func (r *memoryTelemetryRepository) FindPerformanceByOperation(ctx context.Context, operationID string) (core.PerformanceRecord, bool, error) {
	if err := ctx.Err(); err != nil {
		return core.PerformanceRecord{}, false, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var selected *core.PerformanceRecord
	for _, record := range r.records {
		if record.OperationID != operationID {
			continue
		}
		candidate := record
		if selected == nil || candidate.CapturedAt.After(selected.CapturedAt) || candidate.CapturedAt.Equal(selected.CapturedAt) && candidate.DerivationKey > selected.DerivationKey {
			selected = &candidate
		}
	}
	if selected == nil {
		return core.PerformanceRecord{}, false, nil
	}
	return *selected, true, nil
}

func (r *memoryTelemetryRepository) ListCompatiblePerformance(ctx context.Context, compatibilityKey string, limit int) ([]core.PerformanceRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var records []core.PerformanceRecord
	for _, record := range r.records {
		key, err := core.CompatibilityKey(record)
		if err != nil {
			return nil, err
		}
		if key == compatibilityKey {
			records = append(records, record)
		}
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].CapturedAt.Equal(records[j].CapturedAt) {
			return records[i].DerivationKey > records[j].DerivationKey
		}
		return records[i].CapturedAt.After(records[j].CapturedAt)
	})
	if limit < len(records) {
		records = records[:limit]
	}
	return records, nil
}

func (r *memoryTelemetryRepository) CountCompatiblePerformance(ctx context.Context, compatibilityKey string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for _, record := range r.records {
		key, err := core.CompatibilityKey(record)
		if err != nil {
			return 0, err
		}
		if key == compatibilityKey {
			count++
		}
	}
	return count, nil
}

func TestInspectTelemetryReturnsUnavailableWithoutSample(t *testing.T) {
	repo := telemetryFixture(time.Now().UTC().Add(-time.Second), time.Now().UTC(), session.Completed, session.Success)
	repo.records = map[string]core.PerformanceRecord{}
	service, err := newService(repo, "darwin", "arm64")
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.Inspect(context.Background(), InspectRequest{OperationID: "op-missing", MaxSamples: 10})
	if err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != 1 || got.Status != InspectUnavailable || got.OperationID != "op-missing" || got.Latest != nil || got.Summary != nil || got.SamplesReturned != 0 || got.SamplesAvailable != 0 {
		t.Fatalf("unavailable result=%#v", got)
	}
}

func TestInspectTelemetryReturnsBoundedCompatibleHistoryAndSourceHeterogeneity(t *testing.T) {
	base := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	repo := telemetryFixture(base, base.Add(time.Second), session.Completed, session.Success)
	repo.records = map[string]core.PerformanceRecord{}
	first := inspectRecord(t, "op-1", "session-1", strings.Repeat("c", 64), base, session.Success)
	first.SourceContentDigest = strings.Repeat("1", 64)
	second := inspectRecord(t, "op-2", "session-2", strings.Repeat("c", 64), base.Add(time.Second), session.Failure)
	second.SourceContentDigest = strings.Repeat("2", 64)
	third := inspectRecord(t, "op-3", "session-3", strings.Repeat("c", 64), base.Add(2*time.Second), session.Timeout)
	third.SourceContentDigest = ""
	for _, record := range []core.PerformanceRecord{first, second, third} {
		if err := record.Validate(); err != nil {
			t.Fatal(err)
		}
		repo.records[record.DerivationKey] = record
	}
	service, _ := newService(repo, "darwin", "arm64")
	got, err := service.Inspect(context.Background(), InspectRequest{OperationID: "op-3", MaxSamples: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != InspectAvailable || got.Latest == nil || got.Latest.OperationID != "op-3" || got.SamplesReturned != 2 || got.SamplesAvailable != 3 || got.Summary == nil || got.Summary.Samples != 2 {
		t.Fatalf("bounded result=%#v", got)
	}
	if got.Summary.OutcomeCounts.Timeout != 1 || got.Summary.OutcomeCounts.Failure != 1 {
		t.Fatalf("summary cohorts=%#v", got.Summary.OutcomeCounts)
	}
	if got.Summary.SourceHeterogeneity.KnownDistinctDigests != 1 || got.Summary.SourceHeterogeneity.UnknownSamples != 1 {
		t.Fatalf("source heterogeneity=%#v", got.Summary.SourceHeterogeneity)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"performance_regression", "prediction", "recommendation"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("inspection exposed verdict %q: %s", forbidden, encoded)
		}
	}
}

func TestInspectTelemetrySeparatesSameLabelAndIncompatiblePlatformEnvironmentToolchain(t *testing.T) {
	baseTime := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	base := inspectRecord(t, "op-base", "session-base", strings.Repeat("c", 64), baseTime, session.Success)
	base.ProjectCommandID = "test_full"
	base.EnvironmentFingerprint = strings.Repeat("e", 64)
	base.EnvironmentSchemaVersion = 1
	base.ToolchainFingerprint = strings.Repeat("f", 64)
	base.ToolchainSchemaVersion = 1

	variants := []core.PerformanceRecord{}
	for index, mutate := range []func(*core.PerformanceRecord){
		func(v *core.PerformanceRecord) { v.CommandSemanticsFingerprint = strings.Repeat("d", 64) },
		func(v *core.PerformanceRecord) { v.Platform = "linux" },
		func(v *core.PerformanceRecord) { v.EnvironmentFingerprint = strings.Repeat("a", 64) },
		func(v *core.PerformanceRecord) { v.ToolchainFingerprint = strings.Repeat("b", 64) },
	} {
		variant := inspectRecord(t, fmt.Sprintf("op-v%d", index), fmt.Sprintf("session-v%d", index), strings.Repeat("c", 64), baseTime.Add(time.Duration(index+1)*time.Second), session.Success)
		variant.ProjectCommandID = "test_full"
		variant.EnvironmentFingerprint, variant.EnvironmentSchemaVersion = base.EnvironmentFingerprint, 1
		variant.ToolchainFingerprint, variant.ToolchainSchemaVersion = base.ToolchainFingerprint, 1
		mutate(&variant)
		if err := variant.Validate(); err != nil {
			t.Fatalf("variant %d: %v", index, err)
		}
		variants = append(variants, variant)
	}
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	repo := telemetryFixture(baseTime, baseTime.Add(time.Second), session.Completed, session.Success)
	repo.records = map[string]core.PerformanceRecord{base.DerivationKey: base}
	for _, variant := range variants {
		repo.records[variant.DerivationKey] = variant
	}
	service, _ := newService(repo, "darwin", "arm64")
	got, err := service.Inspect(context.Background(), InspectRequest{OperationID: base.OperationID, MaxSamples: MaxInspectSamples})
	if err != nil {
		t.Fatal(err)
	}
	if got.SamplesAvailable != 1 || got.SamplesReturned != 1 || got.Summary == nil || got.Summary.Samples != 1 {
		t.Fatalf("incompatible history blended: %#v", got)
	}
}

func TestInspectTelemetryValidatesBoundsBeforeHistoryAccess(t *testing.T) {
	repo := telemetryFixture(time.Now().UTC().Add(-time.Second), time.Now().UTC(), session.Completed, session.Success)
	service, _ := newService(repo, "darwin", "arm64")
	for _, request := range []InspectRequest{
		{}, {OperationID: "../bad", MaxSamples: 1}, {OperationID: "op-1", MaxSamples: 0}, {OperationID: "op-1", MaxSamples: MaxInspectSamples + 1},
	} {
		if _, err := service.Inspect(context.Background(), request); err == nil {
			t.Fatalf("invalid inspect accepted: %#v", request)
		}
	}
}

func inspectRecord(t *testing.T, operationID, sessionID, semantics string, capturedAt time.Time, outcome session.Outcome) core.PerformanceRecord {
	t.Helper()
	producer := core.Producer{ProducerID: "shellbeam.telemetry", ProducerVersion: 1, CapabilityVersion: 1}
	receiptDigest := digestForInspect(operationID)
	configDigest := strings.Repeat("b", 64)
	key, err := core.DerivationKey(receiptDigest, producer, 1, configDigest)
	if err != nil {
		t.Fatal(err)
	}
	unavailable := core.IntMetric{Quality: core.MetricUnavailable}
	return core.PerformanceRecord{
		SchemaVersion: core.SchemaVersion, DerivationKey: key, DerivationSchemaVersion: 1, DerivationConfigDigest: configDigest,
		Producer: producer, OperationID: operationID, SessionID: sessionID, ReceiptDigest: receiptDigest,
		RepositoryID: "repo_01K00000000000000000000000", WorkspaceID: "ws_01K00000000000000000000000",
		ProjectCommandID: "test_full", CommandSemanticsFingerprint: semantics, ScopeClass: core.ScopeArgv,
		Platform: "darwin", Architecture: "arm64", WallMS: 100, OutputBytes: 10,
		TerminalOutcome: outcome, TimedOut: outcome == session.Timeout, CapturedAt: capturedAt,
		Lifecycle: core.LifecycleTerminal, Completeness: core.CompletenessPartial,
		Resources: core.ResourceMetrics{CPUUserMS: unavailable, CPUSystemMS: unavailable, MaxRSSBytes: unavailable, ReadBytes: unavailable, WriteBytes: unavailable, ProcessCountPeak: unavailable},
	}
}

func digestForInspect(value string) string {
	const digits = "0123456789abcdef"
	var out strings.Builder
	for i := 0; i < 64; i++ {
		out.WriteByte(digits[(int(value[i%len(value)])+i)%len(digits)])
	}
	return out.String()
}
