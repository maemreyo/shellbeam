package verification

import (
	"reflect"
	"strings"
	"testing"
	"time"

	telemetry "github.com/maemreyo/shellbeam/internal/core/telemetry"
	core "github.com/maemreyo/shellbeam/internal/core/verification"
)

func telemetryValue(v int64, q telemetry.MetricQuality) telemetry.IntMetric {
	return telemetry.IntMetric{Quality: q, Value: &v}
}

func costHistoryFor(op, key string) CostHistory {
	wall := telemetry.Percentiles{P50: 100, P95: 250}
	output := telemetry.Percentiles{P50: 1000, P95: 4000}
	latest := telemetry.PerformanceRecord{OperationID: op, WallMS: 125, OutputBytes: 1500, Resources: telemetry.ResourceMetrics{CPUUserMS: telemetryValue(40, telemetry.MetricPlatformReported), CPUSystemMS: telemetry.IntMetric{Quality: telemetry.MetricUnavailable}, MaxRSSBytes: telemetryValue(2048, telemetry.MetricPlatformReported), ReadBytes: telemetry.IntMetric{Quality: telemetry.MetricUnavailable}, WriteBytes: telemetry.IntMetric{Quality: telemetry.MetricUnavailable}, ProcessCountPeak: telemetryValue(5, telemetry.MetricSampled)}}
	summary := telemetry.Summary{SchemaVersion: 1, CompatibilityKey: key, PercentileMethod: telemetry.PercentileNearestRankV1, Samples: 4, WallMS: wall, OutputBytes: output}
	return CostHistory{OperationID: op, CompatibilityKey: key, Latest: &latest, Summary: &summary, SamplesReturned: 4, SamplesAvailable: 4}
}

func costCandidate(op string) core.EvidenceCandidate {
	c := sufficiencyCandidate('d', core.ProviderIntegrationTest, core.AuthorityMechanical, core.CandidatePass)
	c.OperationID = op
	c.CompletedAt = time.Unix(10, 0).UTC()
	return c
}

func TestTelemetryCostProjectionPreservesObservedQuality(t *testing.T) {
	req := sufficiencyRequirement("integration", core.ProviderIntegrationTest)
	obl := sufficiencyObligation(req)
	candidate := costCandidate("op-cost")
	h := costHistoryFor(candidate.OperationID, strings.Repeat("a", 64))
	views := ProjectBoundRequirementCosts([]core.VerificationObligation{obl}, CandidateResultSet{Candidates: []core.EvidenceCandidate{candidate}, Coverage: core.CoverageComplete}, map[string]CostHistory{candidate.OperationID: h})
	if len(views) != 1 {
		t.Fatalf("views=%#v", views)
	}
	cost := views[0].Cost
	if cost.WallMS.Quality != core.CostQualityExact || cost.WallMS.P50 == nil || *cost.WallMS.P50 != 100 || cost.WallMS.P95 == nil || *cost.WallMS.P95 != 250 || cost.WallMS.Samples != 4 {
		t.Fatalf("wall=%#v", cost.WallMS)
	}
	if cost.CPUUserMS.Quality != core.CostQualityPlatformReported || cost.CPUUserMS.Latest == nil || *cost.CPUUserMS.Latest != 40 || cost.CPUUserMS.P50 != nil {
		t.Fatalf("cpu=%#v", cost.CPUUserMS)
	}
	if cost.MaxRSSBytes.Quality != core.CostQualityPlatformReported || cost.ProcessPeak.Quality != core.CostQualitySampled {
		t.Fatalf("resources=%#v", cost)
	}
	if cost.CPUSystemMS.Quality != core.CostQualityUnavailable || cost.CPUSystemMS.Latest != nil {
		t.Fatalf("missing metric invented: %#v", cost.CPUSystemMS)
	}
}

func TestMissingProviderModelCostRemainsUnavailable(t *testing.T) {
	req := sufficiencyRequirement("integration", core.ProviderIntegrationTest)
	obl := sufficiencyObligation(req)
	c := costCandidate("op-cost")
	view := ProjectBoundRequirementCosts([]core.VerificationObligation{obl}, CandidateResultSet{Candidates: []core.EvidenceCandidate{c}}, map[string]CostHistory{c.OperationID: costHistoryFor(c.OperationID, strings.Repeat("b", 64))})[0]
	if view.Cost.ProviderCost.Quality != core.CostQualityUnavailable || view.Cost.ModelCost.Quality != core.CostQualityUnavailable {
		t.Fatalf("invented provider/model cost: %#v", view.Cost)
	}
}

func TestCostProjectionDoesNotSelectProviderAlternative(t *testing.T) {
	one := sufficiencyRequirement("unit", core.ProviderFocusedBehaviorTest)
	two := sufficiencyRequirement("integration", core.ProviderIntegrationTest)
	obl := sufficiencyObligation(one, two)
	views := ProjectBoundRequirementCosts([]core.VerificationObligation{obl}, CandidateResultSet{}, nil)
	if len(views) != 2 || views[0].RequirementID == views[1].RequirementID {
		t.Fatalf("projection selected/collapsed requirements: %#v", views)
	}
}

func TestProviderExecutionSemanticsNeverChoosesUniversalConcurrency(t *testing.T) {
	parallel := true
	req := sufficiencyRequirement("integration", core.ProviderIntegrationTest)
	req.Requirement.Execution = core.ProviderExecutionSemantics{ParallelSafe: &parallel, SharedResources: []string{"db", "cache"}, ExclusiveResourceClass: "database", ExpectedWorkloadClass: "heavy"}
	view := ProjectBoundRequirementCosts([]core.VerificationObligation{sufficiencyObligation(req)}, CandidateResultSet{}, nil)[0]
	if !reflect.DeepEqual(view.Execution, req.Requirement.Execution) {
		t.Fatalf("execution rewritten: got=%#v want=%#v", view.Execution, req.Requirement.Execution)
	}
}

func TestCostProjectionCannotChangeSufficiency(t *testing.T) {
	req := sufficiencyRequirement("integration", core.ProviderIntegrationTest)
	obl := sufficiencyObligation(req)
	c := costCandidate("op-cost")
	before := EvaluateObligation(obl, CandidateResultSet{Candidates: []core.EvidenceCandidate{c}, Coverage: core.CoverageComplete}, available(core.ProviderIntegrationTest), nil, nil)
	_ = ProjectBoundRequirementCosts([]core.VerificationObligation{obl}, CandidateResultSet{Candidates: []core.EvidenceCandidate{c}}, map[string]CostHistory{c.OperationID: costHistoryFor(c.OperationID, strings.Repeat("c", 64))})
	after := EvaluateObligation(obl, CandidateResultSet{Candidates: []core.EvidenceCandidate{c}, Coverage: core.CoverageComplete}, available(core.ProviderIntegrationTest), nil, nil)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("cost rewrote sufficiency before=%#v after=%#v", before, after)
	}
}

func TestIncompatibleTelemetryCohortsAreNeverMerged(t *testing.T) {
	req := sufficiencyRequirement("integration", core.ProviderIntegrationTest)
	obl := sufficiencyObligation(req)
	a := costCandidate("op-a")
	b := costCandidate("op-b")
	b.EvidenceID = "ev_eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	views := ProjectBoundRequirementCosts([]core.VerificationObligation{obl}, CandidateResultSet{Candidates: []core.EvidenceCandidate{a, b}}, map[string]CostHistory{a.OperationID: costHistoryFor(a.OperationID, strings.Repeat("a", 64)), b.OperationID: costHistoryFor(b.OperationID, strings.Repeat("b", 64))})
	if len(views) != 1 || views[0].Cost.WallMS.Quality != core.CostQualityUnavailable {
		t.Fatalf("incompatible cohorts merged: %#v", views)
	}
}
