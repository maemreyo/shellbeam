package verification

import (
	"sort"

	evidence "github.com/maemreyo/shellbeam/internal/core/evidence"
	core "github.com/maemreyo/shellbeam/internal/core/verification"
)

func EvaluateStability(requirement core.StabilityRequirement, protocol *core.FlakeProtocol, candidates CandidateResultSet) core.StabilityEvaluation {
	out := core.StabilityEvaluation{Status: core.EvidenceNotEvaluated, EvidenceRefs: core.SortedEvidenceRefs(candidates.Candidates)}
	if requirement.Validate() != nil || (requirement == core.StabilityFlakeProtocol) != (protocol != nil) {
		out.Status, out.ReasonCode = core.EvidenceUnknown, "invalid_stability_requirement"
		return out
	}
	groups, unknown := groupCompatibleCandidates(candidates.Candidates, &out)
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		cohort := foldBaseCohort(key, groups[key])
		if requirement == core.StabilityFlakeProtocol {
			cohort, out.Flake = foldFlakeCohort(cohort, groups[key], *protocol, out.Flake)
		}
		out.Cohorts = append(out.Cohorts, cohort)
	}
	out.Status = foldCohortStatuses(out.Cohorts)
	if len(unknown) > 0 && out.Status != core.EvidenceFailed && out.Status != core.EvidenceInconsistent {
		out.Status, out.ReasonCode = core.EvidenceUnknown, "compatibility_unknown"
	}
	if candidates.Coverage != core.CoverageComplete && out.Status == core.EvidenceSatisfied {
		out.Status, out.ReasonCode = core.EvidenceUnknown, "bounded_evidence_history"
	}
	if out.Status == core.EvidenceNotEvaluated && len(candidates.Candidates) > 0 {
		out.Status, out.ReasonCode = core.EvidenceUnknown, "compatibility_unknown"
	}
	return out
}

func groupCompatibleCandidates(candidates []core.EvidenceCandidate, out *core.StabilityEvaluation) (map[string][]core.EvidenceCandidate, []core.EvidenceCandidate) {
	groups := map[string][]core.EvidenceCandidate{}
	unknown := []core.EvidenceCandidate{}
	for _, candidate := range candidates {
		if candidate.Attempt != nil && candidate.Attempt.RerunReason == evidence.RerunDiagnoseFlake {
			out.DiagnosticReruns++
		}
		key, ok := core.CompatibilityKey(candidate)
		if !ok {
			unknown = append(unknown, candidate)
			continue
		}
		groups[key] = append(groups[key], candidate)
	}
	return groups, unknown
}

func foldBaseCohort(key string, candidates []core.EvidenceCandidate) core.StabilityCohort {
	cohort := core.StabilityCohort{CompatibilityKey: key, EvidenceRefs: core.SortedEvidenceRefs(candidates)}
	for _, candidate := range candidates {
		cohort.Counts.Runs++
		switch candidate.Result {
		case core.CandidatePass:
			cohort.Counts.Passes++
		case core.CandidateFail:
			cohort.Counts.Failures++
		case core.CandidateIncomplete:
			cohort.Counts.Incomplete++
		case core.CandidateAmbiguous:
			cohort.Counts.Ambiguous++
		}
	}
	switch {
	case cohort.Counts.Passes > 0 && cohort.Counts.Failures > 0:
		cohort.Status = core.EvidenceInconsistent
	case cohort.Counts.Failures > 0:
		cohort.Status = core.EvidenceFailed
	case cohort.Counts.Ambiguous > 0:
		cohort.Status = core.EvidenceUnknown
	case cohort.Counts.Incomplete > 0:
		cohort.Status = core.EvidenceInsufficient
	case cohort.Counts.Passes > 0:
		cohort.Status = core.EvidenceSatisfied
	default:
		cohort.Status = core.EvidenceNotEvaluated
	}
	return cohort
}

func foldFlakeCohort(base core.StabilityCohort, candidates []core.EvidenceCandidate, protocol core.FlakeProtocol, previous *core.FlakeEvaluation) (core.StabilityCohort, *core.FlakeEvaluation) {
	protocolID, ok := core.FlakeProtocolID(protocol)
	if !ok {
		base.Status = core.EvidenceUnknown
		return base, previous
	}
	retained := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		retained[candidate.EvidenceID] = true
	}
	qualified := make([]core.EvidenceCandidate, 0, protocol.Runs)
	for _, candidate := range candidates {
		attempt := candidate.Attempt
		if attempt == nil || attempt.RerunReason != evidence.RerunFlakeQualification || !retained[attempt.RerunOfEvidenceID] {
			continue
		}
		qualified = append(qualified, candidate)
	}
	flake := core.FlakeEvaluation{ProtocolID: protocolID, CompatibilityKey: base.CompatibilityKey, QualifiedEvidenceRefs: core.SortedEvidenceRefs(qualified)}
	for _, candidate := range qualified {
		flake.Counts.Runs++
		switch candidate.Result {
		case core.CandidatePass:
			flake.Counts.Passes++
		case core.CandidateFail:
			flake.Counts.Failures++
		case core.CandidateIncomplete:
			flake.Counts.Incomplete++
		case core.CandidateAmbiguous:
			flake.Counts.Ambiguous++
		}
	}
	if previous != nil && previous.CompatibilityKey != base.CompatibilityKey {
		base.Status = foldStatus(base.Status, core.EvidenceInsufficient)
		return base, previous
	}
	switch {
	case flake.Counts.Runs != protocol.Runs:
		if base.Status != core.EvidenceInconsistent && base.Status != core.EvidenceFailed {
			base.Status = core.EvidenceInsufficient
		}
	case flake.Counts.Incomplete > 0:
		base.Status = core.EvidenceInsufficient
	case flake.Counts.Ambiguous > 0:
		base.Status = core.EvidenceUnknown
	case flake.Counts.Passes >= protocol.MinPasses && flake.Counts.Failures <= protocol.MaxFailures:
		base.Status = core.EvidenceSatisfied
	default:
		base.Status = core.EvidenceFailed
	}
	return base, &flake
}

func foldCohortStatuses(cohorts []core.StabilityCohort) core.EvidenceStatus {
	status := core.EvidenceNotEvaluated
	for _, cohort := range cohorts {
		status = foldStatus(status, cohort.Status)
	}
	return status
}

func foldStatus(left, right core.EvidenceStatus) core.EvidenceStatus {
	rank := map[core.EvidenceStatus]int{
		core.EvidenceNotEvaluated: 0,
		core.EvidenceSatisfied:    1,
		core.EvidenceUnknown:      2,
		core.EvidenceInsufficient: 3,
		core.EvidenceFailed:       4,
		core.EvidenceInconsistent: 5,
	}
	if rank[right] > rank[left] {
		return right
	}
	return left
}
