package verification

import (
	"errors"
	"fmt"
	"sort"

	environment "github.com/maemreyo/shellbeam/internal/core/environment"
	core "github.com/maemreyo/shellbeam/internal/core/verification"
)

var errInvalidAvailability = errors.New("invalid provider availability")

type Availability string

const (
	AvailabilityAvailable   Availability = "available"
	AvailabilityUnavailable Availability = "unavailable"
	AvailabilityUnknown     Availability = "unknown"
)

type ProviderAvailability struct {
	ByClass map[core.ProviderClass]Availability
	Reasons map[core.ProviderClass]string
}

func (a Availability) Validate() error {
	switch a {
	case AvailabilityAvailable, AvailabilityUnavailable, AvailabilityUnknown:
		return nil
	default:
		return errInvalidAvailability
	}
}

func EvaluateObligation(obligation core.VerificationObligation, candidates CandidateResultSet, providers ProviderAvailability, currentEnvironment *environment.Binding, quiescence map[string]core.QuiescenceObservation) core.ObligationEvaluation {
	out := core.ObligationEvaluation{ObligationID: obligation.ObligationID}
	if err := obligation.Validate(); err != nil {
		out.EvidenceStatus = core.EvidenceUnknown
		return out
	}
	for _, bound := range obligation.EvidenceRequirements {
		result := evaluateRequirement(obligation, bound, candidates, providers, currentEnvironment, quiescence)
		out.RequirementResults = append(out.RequirementResults, result)
		out.EvidenceRefs = append(out.EvidenceRefs, result.EvidenceRefs...)
	}
	out.EvidenceRefs = sortedUniqueEvidenceRefs(out.EvidenceRefs)
	out.EvidenceStatus = foldRequirementStatuses(out.RequirementResults)
	return out
}

func evaluateRequirement(obligation core.VerificationObligation, bound core.BoundEvidenceRequirement, candidates CandidateResultSet, providers ProviderAvailability, currentEnvironment *environment.Binding, quiescence map[string]core.QuiescenceObservation) core.RequirementEvaluation {
	requirement := bound.Requirement
	refs := candidateEvidenceRefs(candidates.Candidates)
	result := core.RequirementEvaluation{
		PolicyDigest: obligation.PolicyDigest, RuleID: obligation.SourceRuleID, ObligationID: obligation.ObligationID,
		RequirementID: requirement.ID, EvidenceRefs: refs,
	}
	setResult := func(status core.EvidenceStatus, reason string) core.RequirementEvaluation {
		result.Status, result.ReasonCode = status, reason
		result.EvaluationID, _ = core.EvaluationID(core.EvaluationIdentityInput{PolicyDigest: result.PolicyDigest, RuleID: result.RuleID, ObligationID: result.ObligationID, RequirementID: result.RequirementID, EvidenceRefs: result.EvidenceRefs})
		return result
	}
	if err := bound.Validate(); err != nil {
		return setResult(core.EvidenceUnknown, "invalid_evidence_requirement")
	}
	if requirement.ProjectCommandID != "" && bound.ExpectedProjectBindingDigest == "" {
		return setResult(core.EvidenceUnavailable, "project_binding_unavailable")
	}
	if len(candidates.Candidates) == 0 {
		return noEvidenceResult(setResult, requirement, bound, providers)
	}
	valid := validCandidates(candidates.Candidates)
	if len(valid) != len(candidates.Candidates) {
		return setResult(core.EvidenceUnknown, "invalid_evidence_candidate")
	}
	boundCandidates, bindingMismatch := filterProjectBinding(valid, requirement, bound.ExpectedProjectBindingDigest)
	if len(boundCandidates) == 0 {
		if bindingMismatch {
			return setResult(core.EvidenceInsufficient, "project_binding_mismatch")
		}
		return setResult(core.EvidenceInsufficient, "provider_semantics_mismatch")
	}
	providerCandidates := filterProviderSemantics(boundCandidates, requirement, bound.ExpectedProjectBindingDigest)
	if len(providerCandidates) == 0 {
		return setResult(core.EvidenceInsufficient, "provider_semantics_mismatch")
	}
	authorityCandidates := filterAuthority(providerCandidates, requirement.MinimumAuthority)
	if len(authorityCandidates) == 0 {
		return setResult(core.EvidenceInsufficient, "evidence_authority_insufficient")
	}
	freshCandidates := authorityCandidates
	if requirement.RequireCurrent {
		freshCandidates = filterCurrent(authorityCandidates)
		if len(freshCandidates) == 0 {
			return setResult(core.EvidenceInsufficient, "evidence_stale")
		}
	}
	environmentCandidates, status, reason := filterEnvironment(freshCandidates, requirement.Environment, currentEnvironment)
	if status != "" {
		return setResult(status, reason)
	}
	stability := EvaluateStability(requirement.Stability, requirement.Flake, CandidateResultSet{Candidates: environmentCandidates, Coverage: candidates.Coverage, Diagnostics: candidates.Diagnostics})
	status, reason = stabilityStatus(stability)
	if requirement.RequireQuiescence && status == core.EvidenceSatisfied {
		status, reason = evaluateQuiescence(environmentCandidates, quiescence)
	}
	return setResult(status, reason)
}

func noEvidenceResult(setResult func(core.EvidenceStatus, string) core.RequirementEvaluation, requirement core.EvidenceRequirement, bound core.BoundEvidenceRequirement, providers ProviderAvailability) core.RequirementEvaluation {
	if requirement.ProjectCommandID != "" && bound.ExpectedProjectBindingDigest != "" {
		return setResult(core.EvidenceNotEvaluated, "no_evidence")
	}
	availability, ok := providers.ByClass[requirement.ProviderClass]
	if !ok {
		availability = AvailabilityUnknown
	}
	switch availability {
	case AvailabilityAvailable:
		return setResult(core.EvidenceNotEvaluated, "no_evidence")
	case AvailabilityUnavailable:
		return setResult(core.EvidenceUnavailable, "provider_unavailable")
	default:
		return setResult(core.EvidenceUnknown, "provider_availability_unknown")
	}
}

func validCandidates(candidates []core.EvidenceCandidate) []core.EvidenceCandidate {
	out := make([]core.EvidenceCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Validate() == nil {
			out = append(out, candidate)
		}
	}
	return out
}

func filterProjectBinding(candidates []core.EvidenceCandidate, requirement core.EvidenceRequirement, expected string) ([]core.EvidenceCandidate, bool) {
	if requirement.ProjectCommandID == "" {
		return candidates, false
	}
	out := make([]core.EvidenceCandidate, 0, len(candidates))
	mismatch := false
	for _, candidate := range candidates {
		if candidate.ProviderClass != core.ProviderProjectCommand || candidate.ProjectCommandID != requirement.ProjectCommandID || candidate.ProjectBindingDigest != expected {
			mismatch = true
			continue
		}
		out = append(out, candidate)
	}
	return out, mismatch
}

func filterProviderSemantics(candidates []core.EvidenceCandidate, requirement core.EvidenceRequirement, expectedBinding string) []core.EvidenceCandidate {
	out := make([]core.EvidenceCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if requirement.ProjectCommandID != "" && expectedBinding != "" {
			if candidate.ProviderClassKnown && candidate.ProviderClass == core.ProviderProjectCommand {
				out = append(out, candidate)
			}
			continue
		}
		if candidate.ProviderClassKnown && candidate.ProviderClass == requirement.ProviderClass {
			out = append(out, candidate)
		}
	}
	return out
}

func filterAuthority(candidates []core.EvidenceCandidate, minimum core.DerivationAuthority) []core.EvidenceCandidate {
	out := make([]core.EvidenceCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.AuthorityKnown && core.MeetsMinimumAuthority(candidate.Authority, minimum) {
			out = append(out, candidate)
		}
	}
	return out
}

func filterCurrent(candidates []core.EvidenceCandidate) []core.EvidenceCandidate {
	out := make([]core.EvidenceCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Freshness == core.CandidateCurrent {
			out = append(out, candidate)
		}
	}
	return out
}

func filterEnvironment(candidates []core.EvidenceCandidate, requirement core.EnvironmentRequirement, current *environment.Binding) ([]core.EvidenceCandidate, core.EvidenceStatus, string) {
	if requirement == core.EnvironmentNone {
		return candidates, "", ""
	}
	if current == nil || current.Validate() != nil {
		return nil, core.EvidenceUnavailable, "current_environment_unavailable"
	}
	out := make([]core.EvidenceCandidate, 0, len(candidates))
	unbound, envMismatch, toolMismatch := false, false, false
	for _, candidate := range candidates {
		if candidate.EnvironmentFingerprint == "" {
			unbound = true
			continue
		}
		if candidate.EnvironmentFingerprint != current.EnvironmentFingerprint || candidate.EnvironmentFingerprintVersion != current.EnvironmentFingerprintVersion {
			envMismatch = true
			continue
		}
		if requirement == core.EnvironmentSameCurrentToolchain {
			if candidate.ToolchainFingerprint == "" || current.ToolchainFingerprint == "" || candidate.ToolchainFingerprint != current.ToolchainFingerprint || candidate.ToolchainFingerprintVersion != current.ToolchainFingerprintVersion {
				toolMismatch = true
				continue
			}
		}
		out = append(out, candidate)
	}
	if len(out) > 0 {
		return out, "", ""
	}
	if unbound {
		return nil, core.EvidenceInsufficient, "evidence_environment_unbound"
	}
	if envMismatch {
		return nil, core.EvidenceInsufficient, "environment_mismatch"
	}
	if toolMismatch {
		return nil, core.EvidenceInsufficient, "toolchain_mismatch"
	}
	return nil, core.EvidenceInsufficient, "environment_mismatch"
}

func stabilityStatus(stability core.StabilityEvaluation) (core.EvidenceStatus, string) {
	if stability.ReasonCode != "" {
		return stability.Status, stability.ReasonCode
	}
	switch stability.Status {
	case core.EvidenceFailed:
		return stability.Status, "evidence_failed"
	case core.EvidenceInconsistent:
		return stability.Status, "contradictory_evidence"
	case core.EvidenceInsufficient:
		return stability.Status, "evidence_incomplete"
	case core.EvidenceUnknown:
		return stability.Status, "evidence_ambiguous"
	default:
		return stability.Status, ""
	}
}

func foldRequirementStatuses(results []core.RequirementEvaluation) core.EvidenceStatus {
	for _, status := range []core.EvidenceStatus{core.EvidenceFailed, core.EvidenceInconsistent, core.EvidenceInsufficient, core.EvidenceUnavailable, core.EvidenceUnknown, core.EvidenceNotEvaluated} {
		for _, result := range results {
			if result.Status == status {
				return status
			}
		}
	}
	if len(results) > 0 {
		return core.EvidenceSatisfied
	}
	return core.EvidenceNotEvaluated
}

func candidateEvidenceRefs(candidates []core.EvidenceCandidate) []string {
	refs := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		refs = append(refs, candidate.EvidenceID)
	}
	return sortedUniqueEvidenceRefs(refs)
}

func sortedUniqueEvidenceRefs(refs []string) []string {
	out := append([]string(nil), refs...)
	sort.Strings(out)
	n := 0
	for _, ref := range out {
		if n == 0 || out[n-1] != ref {
			out[n] = ref
			n++
		}
	}
	return out[:n]
}

func evaluateQuiescence(candidates []core.EvidenceCandidate, observations map[string]core.QuiescenceObservation) (core.EvidenceStatus, string) {
	if len(candidates) == 0 {
		return core.EvidenceUnavailable, "quiescence_unavailable"
	}
	for _, candidate := range candidates {
		observation, ok := observations[candidate.OperationID]
		if !ok {
			return core.EvidenceUnavailable, "quiescence_unavailable"
		}
		if observation.Validate() != nil {
			return core.EvidenceUnknown, "quiescence_unknown"
		}
		switch observation.Status {
		case core.QuiescenceComplete:
			continue
		case core.QuiescenceIncomplete:
			return core.EvidenceInsufficient, "undeclared_live_resources"
		case core.QuiescenceUnknown:
			return core.EvidenceUnknown, "quiescence_unknown"
		default:
			return core.EvidenceUnavailable, "quiescence_unavailable"
		}
	}
	return core.EvidenceSatisfied, ""
}

// GateForPolicyState prevents an absent/invalid policy from becoming an
// accidental clear zero-obligation gate. When an older durable policy remains
// effective, its obligations continue to govern regardless of proposal state.
func GateForPolicyState(state PolicyState, hasEffective bool, obligations []core.VerificationObligation, evaluations map[string]core.ObligationEvaluation) (core.GateEvaluation, error) {
	if hasEffective {
		return core.FoldGate(obligations, evaluations)
	}
	reason := ""
	switch state {
	case PolicyStateAbsent:
		reason = "policy_absent"
	case PolicyStateInvalid:
		reason = "policy_invalid"
	case PolicyStateUnsupported:
		reason = "policy_unsupported"
	case PolicyStateProposalPending:
		reason = "policy_proposal_pending"
	case PolicyStateEffective:
		reason = "effective_policy_unavailable"
	default:
		return core.GateEvaluation{}, fmt.Errorf("invalid verification policy state")
	}
	return core.GateEvaluation{Status: core.GateIndeterminate, ReasonCodes: []string{reason}}, nil
}
