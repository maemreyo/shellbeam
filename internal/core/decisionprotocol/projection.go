package decisionprotocol

import (
	"fmt"
	"sort"
)

type DecisionRequirementStatus string

const (
	RequirementSatisfied     DecisionRequirementStatus = "SATISFIED"
	RequirementUnsatisfied   DecisionRequirementStatus = "UNSATISFIED"
	RequirementIndeterminate DecisionRequirementStatus = "INDETERMINATE"
)

func (s DecisionRequirementStatus) Validate() error {
	switch s {
	case RequirementSatisfied, RequirementUnsatisfied, RequirementIndeterminate:
		return nil
	}
	return fmt.Errorf("invalid decision requirement status %q", s)
}

type GateStatus string

const (
	GateClear         GateStatus = "CLEAR"
	GateBlocked       GateStatus = "BLOCKED"
	GateIndeterminate GateStatus = "INDETERMINATE"
)

func (s GateStatus) Validate() error {
	switch s {
	case GateClear, GateBlocked, GateIndeterminate:
		return nil
	}
	return fmt.Errorf("invalid decision gate status %q", s)
}

type DecisionRequirementEvaluation struct {
	RequirementID string                    `json:"requirement_id"`
	Kind          RequirementKind           `json:"kind"`
	Status        DecisionRequirementStatus `json:"status"`
	BasisRefs     []string                  `json:"basis_refs,omitempty"`
	ReasonCode    string                    `json:"reason_code"`
}

func (e DecisionRequirementEvaluation) Validate() error {
	if !boundedToken(e.RequirementID, 128) || e.Kind.Validate() != nil || e.Status.Validate() != nil || !boundedToken(e.ReasonCode, 256) {
		return fmt.Errorf("invalid requirement evaluation")
	}
	return uniqueStrings(e.BasisRefs, 256, 2048, false)
}

type CandidateContractBlocker struct {
	Code         string       `json:"code"`
	PredictionID PredictionID `json:"prediction_id"`
	BasisRefs    []string     `json:"basis_refs,omitempty"`
}

func (b CandidateContractBlocker) Validate() error {
	if !boundedToken(b.Code, 256) || !validID(b.PredictionID) {
		return fmt.Errorf("invalid candidate contract blocker")
	}
	return uniqueStrings(b.BasisRefs, 256, 2048, false)
}

type DecisionProtocolEvaluation struct {
	EpisodeID                 EpisodeID                       `json:"episode_id"`
	CandidateID               CandidateID                     `json:"candidate_id"`
	RequirementEvaluations    []DecisionRequirementEvaluation `json:"requirement_evaluations"`
	CandidateContractBlockers []CandidateContractBlocker      `json:"candidate_contract_blockers"`
	Gate                      GateStatus                      `json:"gate"`
	BlockingRequirementDigest string                          `json:"blocking_requirement_digest"`
}

func (e DecisionProtocolEvaluation) Validate() error {
	if !validID(e.EpisodeID) || !validID(e.CandidateID) || e.Gate.Validate() != nil || !validDerived(e.BlockingRequirementDigest, "block_") {
		return fmt.Errorf("invalid protocol evaluation")
	}
	for _, r := range e.RequirementEvaluations {
		if r.Validate() != nil {
			return fmt.Errorf("invalid requirement evaluation")
		}
	}
	for _, b := range e.CandidateContractBlockers {
		if b.Validate() != nil {
			return fmt.Errorf("invalid contract blocker")
		}
	}
	return nil
}

type CandidateSemanticState struct {
	CandidateID         CandidateID                  `json:"candidate_id"`
	Active              bool                         `json:"active"`
	Superseded          bool                         `json:"superseded"`
	ExpectationOutcomes []PredictionEvaluationStatus `json:"expectation_outcomes,omitempty"`
	Eligible            bool                         `json:"eligible"`
}
type RequirementSemanticState struct {
	RequirementID string                    `json:"requirement_id"`
	Kind          RequirementKind           `json:"kind"`
	Status        DecisionRequirementStatus `json:"status"`
}

type ProjectionSemanticState struct {
	EpisodeID            EpisodeID                  `json:"episode_id"`
	CandidateID          CandidateID                `json:"candidate_id"`
	CandidateStates      []CandidateSemanticState   `json:"candidate_states,omitempty"`
	RequirementStates    []RequirementSemanticState `json:"requirement_states,omitempty"`
	Gate                 GateStatus                 `json:"gate"`
	UnresolvedDimensions []string                   `json:"unresolved_dimensions,omitempty"`
	VerifierState        []VerifierSemanticState    `json:"verifier_state,omitempty"`
	SourceCompatible     bool                       `json:"source_compatible"`
	BasisRefs            []string                   `json:"-"`
}

func canonicalProjectionState(s ProjectionSemanticState) (ProjectionSemanticState, error) {
	if !validID(s.EpisodeID) || !validID(s.CandidateID) || s.Gate.Validate() != nil {
		return s, fmt.Errorf("invalid projection semantic state")
	}
	out := s
	out.BasisRefs = nil
	out.CandidateStates = append([]CandidateSemanticState(nil), s.CandidateStates...)
	sort.Slice(out.CandidateStates, func(i, j int) bool { return out.CandidateStates[i].CandidateID < out.CandidateStates[j].CandidateID })
	for i := range out.CandidateStates {
		outcomes := append([]PredictionEvaluationStatus(nil), out.CandidateStates[i].ExpectationOutcomes...)
		sort.Slice(outcomes, func(a, b int) bool { return outcomes[a] < outcomes[b] })
		for _, status := range outcomes {
			if status.Validate() != nil {
				return s, fmt.Errorf("invalid candidate expectation outcome")
			}
		}
		out.CandidateStates[i].ExpectationOutcomes = outcomes
	}
	out.RequirementStates = append([]RequirementSemanticState(nil), s.RequirementStates...)
	sort.Slice(out.RequirementStates, func(i, j int) bool {
		return out.RequirementStates[i].RequirementID < out.RequirementStates[j].RequirementID
	})
	for _, r := range out.RequirementStates {
		if !boundedToken(r.RequirementID, 128) || r.Kind.Validate() != nil || r.Status.Validate() != nil {
			return s, fmt.Errorf("invalid requirement semantic state")
		}
	}
	out.UnresolvedDimensions = append([]string(nil), s.UnresolvedDimensions...)
	sort.Strings(out.UnresolvedDimensions)
	if err := uniqueStrings(out.UnresolvedDimensions, 128, 512, true); err != nil {
		return s, err
	}
	out.VerifierState = canonicalVerifierState(s.VerifierState)
	for _, v := range out.VerifierState {
		if !boundedToken(v.ActorRef, 192) || (v.QualifiedContextClass != "" && v.QualifiedContextClass.ValidateQualified() != nil) || validateCandidateSet(v.PreferredCandidates, 128) != nil || validateCandidateSet(v.SemanticRejections, 128) != nil {
			return s, fmt.Errorf("invalid verifier semantic state")
		}
	}
	return out, nil
}
func ProjectionDigest(s ProjectionSemanticState) (string, error) {
	out, err := canonicalProjectionState(s)
	if err != nil {
		return "", err
	}
	return canonicalHash("proj_", out)
}

type AuditState struct {
	EpisodeID           EpisodeID   `json:"episode_id"`
	CanonicalRecordSeqs []RecordSeq `json:"canonical_record_seqs"`
	BasisRefs           []string    `json:"basis_refs"`
}

func AuditDigest(s AuditState) (string, error) {
	if !validID(s.EpisodeID) {
		return "", fmt.Errorf("invalid audit episode")
	}
	for _, seq := range s.CanonicalRecordSeqs {
		if seq == 0 {
			return "", fmt.Errorf("invalid audit record seq")
		}
	}
	if err := uniqueStrings(s.BasisRefs, 1024, 2048, false); err != nil {
		return "", err
	}
	return canonicalHash("audit_", s)
}

type DecisionProjection struct {
	EpisodeID            EpisodeID                  `json:"episode_id"`
	CandidateID          CandidateID                `json:"candidate_id"`
	ProjectionDigest     string                     `json:"projection_digest"`
	AuditDigest          string                     `json:"audit_digest"`
	Protocol             DecisionProtocolEvaluation `json:"protocol"`
	SourceCompatible     bool                       `json:"source_compatible"`
	UnresolvedDimensions []string                   `json:"unresolved_dimensions,omitempty"`
}
