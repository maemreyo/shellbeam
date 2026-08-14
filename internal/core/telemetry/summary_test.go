package telemetry

import (
	"strings"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/session"
)

func TestSummaryIsDeterministicAndRetainsFailureTimeoutCohorts(t *testing.T) {
	records := []PerformanceRecord{
		summarySample(100, 10, session.Success),
		summarySample(200, 20, session.Failure),
		summarySample(300, 30, session.Timeout),
		summarySample(400, 40, session.Success),
	}
	got, err := Summarize(records)
	if err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != 1 || got.PercentileMethod != PercentileNearestRankV1 || got.Samples != 4 {
		t.Fatalf("summary metadata=%#v", got)
	}
	if got.OutcomeCounts.Success != 2 || got.OutcomeCounts.Failure != 1 || got.OutcomeCounts.Timeout != 1 {
		t.Fatalf("cohorts=%#v", got.OutcomeCounts)
	}
	if got.WallMS.P50 != 200 || got.WallMS.P95 != 400 || got.OutputBytes.P50 != 20 || got.OutputBytes.P95 != 40 {
		t.Fatalf("percentiles wall=%#v output=%#v", got.WallMS, got.OutputBytes)
	}
	if got.TimeoutRate != 0.25 {
		t.Fatalf("timeout rate=%v", got.TimeoutRate)
	}
	again, err := Summarize([]PerformanceRecord{records[3], records[1], records[0], records[2]})
	if err != nil {
		t.Fatal(err)
	}
	if again.WallMS != got.WallMS || again.OutputBytes != got.OutputBytes || again.OutcomeCounts != got.OutcomeCounts || again.TimeoutRate != got.TimeoutRate {
		t.Fatalf("order changed summary first=%#v second=%#v", got, again)
	}
}

func TestSummaryReportsSourceHeterogeneityWithoutChangingCompatibility(t *testing.T) {
	first := summarySample(100, 10, session.Success)
	first.SourceContentDigest = strings.Repeat("a", 64)
	second := summarySample(200, 20, session.Success)
	second.SourceContentDigest = strings.Repeat("b", 64)
	third := summarySample(300, 30, session.Success)
	third.SourceContentDigest = ""
	got, err := Summarize([]PerformanceRecord{first, second, third})
	if err != nil {
		t.Fatal(err)
	}
	if got.SourceHeterogeneity.KnownDistinctDigests != 2 || got.SourceHeterogeneity.UnknownSamples != 1 {
		t.Fatalf("source heterogeneity=%#v", got.SourceHeterogeneity)
	}
}

func TestSummaryRejectsIncompatibleRecords(t *testing.T) {
	first := summarySample(100, 10, session.Success)
	second := summarySample(200, 20, session.Success)
	second.CommandSemanticsFingerprint = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	if _, err := Summarize([]PerformanceRecord{first, second}); err == nil {
		t.Fatal("incompatible records summarized together")
	}
	if _, err := Summarize(nil); err == nil {
		t.Fatal("empty summary accepted")
	}
}

func summarySample(wall, output int64, outcome session.Outcome) PerformanceRecord {
	record := validPerformanceRecord()
	record.WallMS = wall
	record.OutputBytes = output
	record.TerminalOutcome = outcome
	record.TimedOut = outcome == session.Timeout
	return record
}
