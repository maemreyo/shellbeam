package telemetry

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/session"
)

const PercentileNearestRankV1 = "nearest_rank_v1"

type Percentiles struct {
	P50 int64 `json:"p50"`
	P95 int64 `json:"p95"`
}

type OutcomeCounts struct {
	Success   int `json:"success"`
	Failure   int `json:"failure"`
	Timeout   int `json:"timeout"`
	Killed    int `json:"killed"`
	Ambiguous int `json:"ambiguous"`
}

type SourceHeterogeneity struct {
	KnownDistinctDigests int `json:"known_distinct_digests"`
	UnknownSamples       int `json:"unknown_samples"`
}

type Summary struct {
	SchemaVersion       int                 `json:"schema_version"`
	CompatibilityKey    string              `json:"compatibility_key"`
	PercentileMethod    string              `json:"percentile_method"`
	Samples             int                 `json:"samples"`
	FirstCapturedAt     time.Time           `json:"first_captured_at"`
	LastCapturedAt      time.Time           `json:"last_captured_at"`
	WallMS              Percentiles         `json:"wall_ms"`
	OutputBytes         Percentiles         `json:"output_bytes"`
	OutcomeCounts       OutcomeCounts       `json:"outcome_counts"`
	SourceHeterogeneity SourceHeterogeneity `json:"source_heterogeneity"`
	TimeoutRate         float64             `json:"timeout_rate"`
}

func Summarize(records []PerformanceRecord) (Summary, error) {
	if len(records) == 0 {
		return Summary{}, fmt.Errorf("telemetry summary requires samples")
	}
	compatibility, err := CompatibilityKey(records[0])
	if err != nil {
		return Summary{}, err
	}
	walls := make([]int64, 0, len(records))
	outputs := make([]int64, 0, len(records))
	sourceDigests := map[string]struct{}{}
	out := Summary{SchemaVersion: 1, CompatibilityKey: compatibility, PercentileMethod: PercentileNearestRankV1, Samples: len(records)}
	for index, record := range records {
		key, err := CompatibilityKey(record)
		if err != nil {
			return Summary{}, err
		}
		if key != compatibility {
			return Summary{}, fmt.Errorf("telemetry history is incompatible")
		}
		walls = append(walls, record.WallMS)
		outputs = append(outputs, record.OutputBytes)
		if record.SourceContentDigest == "" {
			out.SourceHeterogeneity.UnknownSamples++
		} else {
			sourceDigests[record.SourceContentDigest] = struct{}{}
		}
		if index == 0 || record.CapturedAt.Before(out.FirstCapturedAt) {
			out.FirstCapturedAt = record.CapturedAt
		}
		if index == 0 || record.CapturedAt.After(out.LastCapturedAt) {
			out.LastCapturedAt = record.CapturedAt
		}
		switch record.TerminalOutcome {
		case session.Success:
			out.OutcomeCounts.Success++
		case session.Failure:
			out.OutcomeCounts.Failure++
		case session.Timeout:
			out.OutcomeCounts.Timeout++
		case session.KilledOutcome:
			out.OutcomeCounts.Killed++
		case session.Ambiguous:
			out.OutcomeCounts.Ambiguous++
		}
	}
	sort.Slice(walls, func(i, j int) bool { return walls[i] < walls[j] })
	sort.Slice(outputs, func(i, j int) bool { return outputs[i] < outputs[j] })
	out.WallMS = Percentiles{P50: nearestRank(walls, 0.50), P95: nearestRank(walls, 0.95)}
	out.OutputBytes = Percentiles{P50: nearestRank(outputs, 0.50), P95: nearestRank(outputs, 0.95)}
	out.SourceHeterogeneity.KnownDistinctDigests = len(sourceDigests)
	out.TimeoutRate = float64(out.OutcomeCounts.Timeout) / float64(len(records))
	return out, nil
}

func nearestRank(sorted []int64, percentile float64) int64 {
	rank := int(math.Ceil(percentile * float64(len(sorted))))
	if rank < 1 {
		rank = 1
	}
	if rank > len(sorted) {
		rank = len(sorted)
	}
	return sorted[rank-1]
}
