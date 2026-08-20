package verification

import (
	"context"
	"fmt"
	"sort"

	telemetryapp "github.com/maemreyo/shellbeam/internal/app/telemetry"
	app "github.com/maemreyo/shellbeam/internal/app/verification"
)

const verificationTelemetryMaxSamples = 64

type telemetryInspector interface {
	Inspect(context.Context, telemetryapp.InspectRequest) (telemetryapp.InspectResult, error)
}
type TelemetrySource struct{ service telemetryInspector }

func NewTelemetrySource(service telemetryInspector) *TelemetrySource {
	return &TelemetrySource{service: service}
}
func (s *TelemetrySource) Histories(ctx context.Context, operationIDs []string) (map[string]app.CostHistory, error) {
	if s == nil || s.service == nil {
		return nil, fmt.Errorf("telemetry source unavailable")
	}
	ids := append([]string(nil), operationIDs...)
	sort.Strings(ids)
	ids = uniqueStrings(ids)
	if len(ids) > 128 {
		return nil, fmt.Errorf("too many telemetry operations")
	}
	out := make(map[string]app.CostHistory)
	seen := map[string]bool{}
	for _, op := range ids {
		result, err := s.service.Inspect(ctx, telemetryapp.InspectRequest{OperationID: op, MaxSamples: verificationTelemetryMaxSamples})
		if err != nil {
			return nil, err
		}
		if result.Status != telemetryapp.InspectAvailable || result.CompatibilityKey == "" || result.Latest == nil || result.Summary == nil {
			continue
		}
		if seen[result.CompatibilityKey] {
			continue
		}
		seen[result.CompatibilityKey] = true
		latest, summary := *result.Latest, *result.Summary
		out[op] = app.CostHistory{OperationID: op, CompatibilityKey: result.CompatibilityKey, Latest: &latest, Summary: &summary, SamplesReturned: result.SamplesReturned, SamplesAvailable: result.SamplesAvailable}
	}
	return out, nil
}
func uniqueStrings(values []string) []string {
	n := 0
	for _, v := range values {
		if v == "" {
			continue
		}
		if n == 0 || values[n-1] != v {
			values[n] = v
			n++
		}
	}
	return values[:n]
}
