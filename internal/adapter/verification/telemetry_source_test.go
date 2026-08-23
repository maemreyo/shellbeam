package verification

import (
	"context"
	"strings"
	"testing"

	telemetryapp "github.com/maemreyo/shellbeam/internal/app/telemetry"
	telemetry "github.com/maemreyo/shellbeam/internal/core/telemetry"
)

type telemetryInspectFake struct {
	results map[string]telemetryapp.InspectResult
	calls   []telemetryapp.InspectRequest
}

func (f *telemetryInspectFake) Inspect(_ context.Context, req telemetryapp.InspectRequest) (telemetryapp.InspectResult, error) {
	f.calls = append(f.calls, req)
	return f.results[req.OperationID], nil
}

func telemetryInspectResult(op, key string) telemetryapp.InspectResult {
	latest := telemetry.PerformanceRecord{OperationID: op}
	summary := telemetry.Summary{CompatibilityKey: key, Samples: 3}
	return telemetryapp.InspectResult{SchemaVersion: telemetryapp.InspectSchemaVersion, Status: telemetryapp.InspectAvailable, OperationID: op, CompatibilityKey: key, Latest: &latest, Summary: &summary, SamplesReturned: 3, SamplesAvailable: 5}
}

func TestTelemetrySourceUsesBoundedOperationInspectAndDeduplicatesCompatibleCohorts(t *testing.T) {
	key := strings.Repeat("a", 64)
	fake := &telemetryInspectFake{results: map[string]telemetryapp.InspectResult{"op-a": telemetryInspectResult("op-a", key), "op-b": telemetryInspectResult("op-b", key), "op-c": telemetryInspectResult("op-c", strings.Repeat("b", 64))}}
	s := NewTelemetrySource(fake)
	got, err := s.Histories(context.Background(), []string{"op-b", "op-a", "op-c", "op-a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("compatible cohorts not deduped: %#v", got)
	}
	if len(fake.calls) != 3 {
		t.Fatalf("inspect calls=%d %#v", len(fake.calls), fake.calls)
	}
	for _, call := range fake.calls {
		if call.MaxSamples != 64 {
			t.Fatalf("unbounded inspect=%#v", call)
		}
	}
}
