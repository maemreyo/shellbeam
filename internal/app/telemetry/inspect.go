package telemetry

import (
	"context"
	"fmt"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	core "github.com/maemreyo/shellbeam/internal/core/telemetry"
)

const (
	InspectSchemaVersion = 1
	MaxInspectSamples    = 128
)

type InspectStatus string

const (
	InspectAvailable   InspectStatus = "available"
	InspectUnavailable InspectStatus = "unavailable"
)

type InspectRequest struct {
	OperationID string `json:"operation_id"`
	MaxSamples  int    `json:"max_samples"`
}

type InspectResult struct {
	SchemaVersion    int                     `json:"schema_version"`
	Status           InspectStatus           `json:"status"`
	OperationID      string                  `json:"operation_id"`
	CompatibilityKey string                  `json:"compatibility_key,omitempty"`
	Latest           *core.PerformanceRecord `json:"latest,omitempty"`
	Summary          *core.Summary           `json:"summary,omitempty"`
	SamplesReturned  int                     `json:"samples_returned"`
	SamplesAvailable int                     `json:"samples_available"`
}

func (s *Service) Inspect(ctx context.Context, request InspectRequest) (InspectResult, error) {
	if s == nil || s.repository == nil {
		return InspectResult{}, fmt.Errorf("telemetry repository unavailable")
	}
	if err := ctx.Err(); err != nil {
		return InspectResult{}, err
	}
	if _, err := operation.ParseID(request.OperationID); err != nil {
		return InspectResult{}, err
	}
	if request.MaxSamples < 1 || request.MaxSamples > MaxInspectSamples {
		return InspectResult{}, fmt.Errorf("invalid telemetry inspect sample bound")
	}
	history, ok := s.repository.(HistoryRepository)
	if !ok {
		return InspectResult{}, fmt.Errorf("telemetry history unavailable")
	}
	out := InspectResult{SchemaVersion: InspectSchemaVersion, Status: InspectUnavailable, OperationID: request.OperationID}
	latest, found, err := history.FindPerformanceByOperation(ctx, request.OperationID)
	if err != nil {
		return InspectResult{}, err
	}
	if !found {
		return out, nil
	}
	compatibilityKey, err := core.CompatibilityKey(latest)
	if err != nil {
		return InspectResult{}, err
	}
	records, err := history.ListCompatiblePerformance(ctx, compatibilityKey, request.MaxSamples)
	if err != nil {
		return InspectResult{}, err
	}
	if len(records) == 0 {
		return InspectResult{}, fmt.Errorf("telemetry history unavailable")
	}
	for _, record := range records {
		key, err := core.CompatibilityKey(record)
		if err != nil {
			return InspectResult{}, err
		}
		if key != compatibilityKey {
			return InspectResult{}, fmt.Errorf("telemetry_incompatible_history")
		}
	}
	summary, err := core.Summarize(records)
	if err != nil {
		return InspectResult{}, err
	}
	available, err := history.CountCompatiblePerformance(ctx, compatibilityKey)
	if err != nil {
		return InspectResult{}, err
	}
	if available < len(records) {
		// Concurrent retention may reduce the current count between the bounded
		// list and count reads. Never report fewer available samples than the
		// exact samples already returned in this response.
		available = len(records)
	}
	latestCopy := latest
	summaryCopy := summary
	out.Status = InspectAvailable
	out.CompatibilityKey = compatibilityKey
	out.Latest = &latestCopy
	out.Summary = &summaryCopy
	out.SamplesReturned = len(records)
	out.SamplesAvailable = available
	return out, nil
}
