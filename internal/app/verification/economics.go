package verification

import (
	"sort"

	telemetry "github.com/maemreyo/shellbeam/internal/core/telemetry"
	core "github.com/maemreyo/shellbeam/internal/core/verification"
)

func ProjectBoundRequirementCosts(obligations []core.VerificationObligation, candidates CandidateResultSet, histories map[string]CostHistory) []core.BoundRequirementCost {
	out := make([]core.BoundRequirementCost, 0)
	for _, obligation := range obligations {
		for _, bound := range obligation.EvidenceRequirements {
			req := bound.Requirement
			view := core.BoundRequirementCost{ObligationID: obligation.ObligationID, RequirementID: req.ID, ProviderClass: req.ProviderClass, ProjectCommandID: req.ProjectCommandID, Execution: req.Execution, Cost: core.UnavailableVerificationCost()}
			view.Cost.ProjectCommandID = req.ProjectCommandID
			matched := relevantHistories(req, bound.ExpectedProjectBindingDigest, candidates.Candidates, histories)
			if len(matched) == 1 {
				view.Cost = projectHistoryCost(req.ProjectCommandID, matched[0])
			}
			out = append(out, view)
		}
	}
	return out
}

func relevantHistories(req core.EvidenceRequirement, expectedBinding string, candidates []core.EvidenceCandidate, histories map[string]CostHistory) []CostHistory {
	byKey := map[string]CostHistory{}
	for _, candidate := range candidates {
		if !candidateRelevantToRequirement(candidate, req, expectedBinding) {
			continue
		}
		h, ok := histories[candidate.OperationID]
		if !ok || h.CompatibilityKey == "" || h.Latest == nil || h.Summary == nil {
			continue
		}
		byKey[h.CompatibilityKey] = h
	}
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]CostHistory, 0, len(keys))
	for _, key := range keys {
		out = append(out, byKey[key])
	}
	return out
}
func candidateRelevantToRequirement(c core.EvidenceCandidate, req core.EvidenceRequirement, expectedBinding string) bool {
	if req.ProjectCommandID != "" {
		return c.ProviderClassKnown && c.ProviderClass == core.ProviderProjectCommand && c.ProjectCommandID == req.ProjectCommandID && expectedBinding != "" && c.ProjectBindingDigest == expectedBinding
	}
	return c.ProviderClassKnown && c.ProviderClass == req.ProviderClass
}
func projectHistoryCost(projectCommandID string, h CostHistory) core.VerificationCost {
	cost := core.UnavailableVerificationCost()
	cost.ProjectCommandID = projectCommandID
	if h.Summary != nil && h.Summary.Samples > 0 {
		cost.WallMS = percentileMetric(h.Summary.WallMS, h.Summary.Samples)
		cost.OutputBytes = percentileMetric(h.Summary.OutputBytes, h.Summary.Samples)
	}
	if h.Latest != nil {
		cost.CPUUserMS = latestMetric(h.Latest.Resources.CPUUserMS)
		cost.CPUSystemMS = latestMetric(h.Latest.Resources.CPUSystemMS)
		cost.MaxRSSBytes = latestMetric(h.Latest.Resources.MaxRSSBytes)
		cost.ProcessPeak = latestMetric(h.Latest.Resources.ProcessCountPeak)
	}
	return cost
}
func percentileMetric(p telemetry.Percentiles, samples int) core.CostMetric {
	p50, p95 := p.P50, p.P95
	return core.CostMetric{Quality: core.CostQualityExact, P50: &p50, P95: &p95, Samples: samples}
}
func latestMetric(m telemetry.IntMetric) core.CostMetric {
	if m.Quality == telemetry.MetricUnavailable || m.Value == nil {
		return core.CostMetric{Quality: core.CostQualityUnavailable}
	}
	quality := core.CostQuality(m.Quality)
	value := *m.Value
	return core.CostMetric{Quality: quality, Latest: &value, Samples: 1}
}
