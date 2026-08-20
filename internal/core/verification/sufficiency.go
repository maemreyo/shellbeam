package verification

import (
	"fmt"
	"sort"
)

const evaluationSemanticsVersion = 1

type EvaluationIdentityInput struct {
	PolicyDigest  string   `json:"policy_digest"`
	RuleID        string   `json:"rule_id"`
	ObligationID  string   `json:"obligation_id"`
	RequirementID string   `json:"requirement_id"`
	EvidenceRefs  []string `json:"evidence_refs,omitempty"`
}

type RequirementEvaluation struct {
	EvaluationID  string         `json:"evaluation_id"`
	PolicyDigest  string         `json:"policy_digest"`
	RuleID        string         `json:"rule_id"`
	ObligationID  string         `json:"obligation_id"`
	RequirementID string         `json:"requirement_id"`
	Status        EvidenceStatus `json:"status"`
	EvidenceRefs  []string       `json:"evidence_refs,omitempty"`
	ReasonCode    string         `json:"reason_code,omitempty"`
}

type ObligationEvaluation struct {
	ObligationID       string                  `json:"obligation_id"`
	EvidenceStatus     EvidenceStatus          `json:"evidence_status"`
	RequirementResults []RequirementEvaluation `json:"requirement_results"`
	EvidenceRefs       []string                `json:"evidence_refs,omitempty"`
}

func EvaluationID(input EvaluationIdentityInput) (string, error) {
	if !isDerivedID(input.PolicyDigest, "pol_") || !boundedToken(input.RuleID, 128) || !isDerivedID(input.ObligationID, "obl_") || !boundedToken(input.RequirementID, 128) {
		return "", fmt.Errorf("invalid evaluation identity")
	}
	refs, err := normalizeEvaluationEvidenceRefs(input.EvidenceRefs)
	if err != nil {
		return "", err
	}
	return hashID("eval_", struct {
		Version       int      `json:"evaluation_semantics_version"`
		PolicyDigest  string   `json:"policy_digest"`
		RuleID        string   `json:"rule_id"`
		ObligationID  string   `json:"obligation_id"`
		RequirementID string   `json:"requirement_id"`
		EvidenceRefs  []string `json:"evidence_refs,omitempty"`
	}{evaluationSemanticsVersion, input.PolicyDigest, input.RuleID, input.ObligationID, input.RequirementID, refs})
}

func (r RequirementEvaluation) Validate() error {
	if r.Status.Validate() != nil || !boundedOptionalToken(r.ReasonCode, 128) {
		return fmt.Errorf("invalid requirement evaluation status")
	}
	want, err := EvaluationID(EvaluationIdentityInput{PolicyDigest: r.PolicyDigest, RuleID: r.RuleID, ObligationID: r.ObligationID, RequirementID: r.RequirementID, EvidenceRefs: r.EvidenceRefs})
	if err != nil || want != r.EvaluationID {
		return fmt.Errorf("invalid requirement evaluation identity")
	}
	return nil
}

func (o ObligationEvaluation) Validate() error {
	if !isDerivedID(o.ObligationID, "obl_") || o.EvidenceStatus.Validate() != nil || len(o.RequirementResults) > 32 {
		return fmt.Errorf("invalid obligation evaluation")
	}
	for i := range o.RequirementResults {
		if o.RequirementResults[i].ObligationID != o.ObligationID || o.RequirementResults[i].Validate() != nil {
			return fmt.Errorf("invalid requirement evaluation %d", i)
		}
	}
	_, err := normalizeEvaluationEvidenceRefs(o.EvidenceRefs)
	return err
}

func normalizeEvaluationEvidenceRefs(refs []string) ([]string, error) {
	out := append([]string(nil), refs...)
	for _, ref := range out {
		if !isDerivedID(ref, "ev_") {
			return nil, fmt.Errorf("invalid evaluation evidence ref")
		}
	}
	sort.Strings(out)
	n := 0
	for _, ref := range out {
		if n == 0 || out[n-1] != ref {
			out[n] = ref
			n++
		}
	}
	return out[:n], nil
}

func boundedOptionalToken(value string, max int) bool {
	return value == "" || boundedToken(value, max)
}

type GateBreakdown struct {
	EvidenceSatisfied int `json:"evidence_satisfied"`
	Waived            int `json:"waived"`
	Blocking          int `json:"blocking"`
	Indeterminate     int `json:"indeterminate"`
}

type GateEvaluation struct {
	Status      GateStatus    `json:"status"`
	Breakdown   GateBreakdown `json:"breakdown"`
	ReasonCodes []string      `json:"reason_codes,omitempty"`
}

func (g GateBreakdown) Validate() error {
	if g.EvidenceSatisfied < 0 || g.Waived < 0 || g.Blocking < 0 || g.Indeterminate < 0 {
		return fmt.Errorf("invalid gate breakdown")
	}
	return nil
}

func (g GateEvaluation) Validate() error {
	if g.Status.Validate() != nil || g.Breakdown.Validate() != nil {
		return fmt.Errorf("invalid gate evaluation")
	}
	if len(g.ReasonCodes) > 32 {
		return fmt.Errorf("too many gate reasons")
	}
	for i, reason := range g.ReasonCodes {
		if !boundedToken(reason, 128) || (i > 0 && g.ReasonCodes[i-1] >= reason) {
			return fmt.Errorf("invalid gate reason codes")
		}
	}
	return nil
}

func FoldGate(obligations []VerificationObligation, evaluations map[string]ObligationEvaluation) (GateEvaluation, error) {
	out := GateEvaluation{Status: GateClear}
	seen := make(map[string]bool, len(obligations))
	reasons := map[string]bool{}
	for _, obligation := range obligations {
		if err := obligation.Validate(); err != nil {
			return GateEvaluation{}, err
		}
		if seen[obligation.ObligationID] {
			return GateEvaluation{}, fmt.Errorf("duplicate obligation id")
		}
		seen[obligation.ObligationID] = true
		switch obligation.Disposition {
		case DispositionDeferred, DispositionOptional, DispositionNotTriggered:
			continue
		case DispositionRequiredNow, DispositionWaived:
		default:
			return GateEvaluation{}, fmt.Errorf("invalid gate obligation disposition")
		}
		evaluation, ok := evaluations[obligation.ObligationID]
		if !ok || evaluation.ObligationID != obligation.ObligationID || evaluation.Validate() != nil {
			return GateEvaluation{}, fmt.Errorf("missing or invalid obligation evaluation")
		}
		if obligation.Disposition == DispositionWaived {
			out.Breakdown.Waived++
			continue
		}
		switch evaluation.EvidenceStatus {
		case EvidenceSatisfied:
			out.Breakdown.EvidenceSatisfied++
		case EvidenceFailed, EvidenceInsufficient, EvidenceInconsistent:
			out.Breakdown.Blocking++
			reasons[gateEvidenceReason(evaluation.EvidenceStatus)] = true
		case EvidenceNotEvaluated, EvidenceUnknown, EvidenceUnavailable:
			out.Breakdown.Indeterminate++
			reasons[gateEvidenceReason(evaluation.EvidenceStatus)] = true
		default:
			return GateEvaluation{}, fmt.Errorf("invalid gate evidence status")
		}
	}
	if out.Breakdown.Blocking > 0 {
		out.Status = GateBlocked
	} else if out.Breakdown.Indeterminate > 0 {
		out.Status = GateIndeterminate
	}
	out.ReasonCodes = make([]string, 0, len(reasons))
	for reason := range reasons {
		out.ReasonCodes = append(out.ReasonCodes, reason)
	}
	sort.Strings(out.ReasonCodes)
	if err := out.Validate(); err != nil {
		return GateEvaluation{}, err
	}
	return out, nil
}

func gateEvidenceReason(status EvidenceStatus) string {
	switch status {
	case EvidenceFailed:
		return "evidence_failed"
	case EvidenceInsufficient:
		return "evidence_insufficient"
	case EvidenceInconsistent:
		return "evidence_inconsistent"
	case EvidenceNotEvaluated:
		return "evidence_not_evaluated"
	case EvidenceUnknown:
		return "evidence_unknown"
	case EvidenceUnavailable:
		return "evidence_unavailable"
	default:
		return "evidence_status_invalid"
	}
}
