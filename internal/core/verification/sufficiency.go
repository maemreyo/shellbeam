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
