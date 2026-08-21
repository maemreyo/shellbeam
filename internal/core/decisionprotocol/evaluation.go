package decisionprotocol

import (
	"fmt"
	"sort"
)

const CandidateBlockerRequiredPredictionMismatch = "DECLARED_REQUIRED_PREDICTION_MISMATCH"

type CandidateEvaluationFact struct {
	CandidateID CandidateID `json:"candidate_id"`
	LineageRoot CandidateID `json:"lineage_root"`
}

type PredictionEvaluationFact struct {
	PredictionID PredictionID               `json:"prediction_id"`
	CandidateID  CandidateID                `json:"candidate_id"`
	Role         PredictionRole             `json:"role"`
	Sealed       bool                       `json:"sealed"`
	Linked       bool                       `json:"linked"`
	Status       PredictionEvaluationStatus `json:"status"`
	BasisRefs    []string                   `json:"basis_refs,omitempty"`
}

type DiscriminationResult string

const (
	DiscriminationResultUnavailable DiscriminationResult = "UNAVAILABLE"
	DiscriminationResultNoPartition DiscriminationResult = "NO_PARTITION"
	DiscriminationResultPartitioned DiscriminationResult = "PARTITIONED"
)

type DiscriminationEvaluationFact struct {
	ExperimentID       ExperimentID         `json:"experiment_id"`
	CandidateID        CandidateID          `json:"candidate_id"`
	Potential          bool                 `json:"potential"`
	Linked             bool                 `json:"linked"`
	ObservationSettled bool                 `json:"observation_settled"`
	Closed             bool                 `json:"closed"`
	Aborted            bool                 `json:"aborted"`
	Realized           DiscriminationResult `json:"realized"`
}

type EvaluationFacts struct {
	EpisodeID           EpisodeID                      `json:"episode_id"`
	CandidateID         CandidateID                    `json:"candidate_id"`
	Candidates          []CandidateEvaluationFact      `json:"candidates,omitempty"`
	Predictions         []PredictionEvaluationFact     `json:"predictions,omitempty"`
	Experiments         []DiscriminationEvaluationFact `json:"experiments,omitempty"`
	VerifierAssessments []VerifierAssessment           `json:"verifier_assessments,omitempty"`
}

type MachineWallQuality string

const (
	MachineWallNotObserved     MachineWallQuality = "NOT_OBSERVED"
	MachineWallObservedNotHard MachineWallQuality = "OBSERVED_NOT_HARD"
	MachineWallHardEnforced    MachineWallQuality = "HARD_ENFORCED"
)

type BudgetUsage struct {
	ExperimentsStarted uint64             `json:"experiments_started"`
	LinkedOperations   uint64             `json:"linked_operations"`
	MachineWallMS      uint64             `json:"machine_wall_ms"`
	MachineWallQuality MachineWallQuality `json:"machine_wall_quality"`
}

type BudgetAdmission struct {
	MayStartExperiment   bool               `json:"may_start_experiment"`
	MayLinkOperation     bool               `json:"may_link_operation"`
	ExperimentsExhausted bool               `json:"experiments_exhausted"`
	LinksExhausted       bool               `json:"links_exhausted"`
	MachineWallExhausted bool               `json:"machine_wall_exhausted"`
	MachineWallQuality   MachineWallQuality `json:"machine_wall_quality"`
}

type DecisionProtocolGate struct {
	Status GateStatus `json:"status"`
}

type EpistemicProgress string

const (
	EpistemicProgressNone    EpistemicProgress = "NONE"
	EpistemicProgressChanged EpistemicProgress = "CHANGED"
)

func CompareEpistemicProgress(previous, current string) EpistemicProgress {
	if previous != "" && previous == current {
		return EpistemicProgressNone
	}
	return EpistemicProgressChanged
}

func BlockingRequirementDigest(reqs []DecisionRequirementEvaluation, blockers []CandidateContractBlocker) (string, error) {
	type requirementState struct {
		RequirementID string                    `json:"requirement_id"`
		Kind          RequirementKind           `json:"kind"`
		Status        DecisionRequirementStatus `json:"status"`
		ReasonCode    string                    `json:"reason_code"`
	}
	type blockerState struct {
		Code         string       `json:"code"`
		PredictionID PredictionID `json:"prediction_id"`
	}
	state := struct {
		Requirements []requirementState `json:"requirements,omitempty"`
		Blockers     []blockerState     `json:"blockers,omitempty"`
	}{}
	for _, r := range reqs {
		if r.Status == RequirementSatisfied {
			continue
		}
		state.Requirements = append(state.Requirements, requirementState{r.RequirementID, r.Kind, r.Status, r.ReasonCode})
	}
	for _, b := range blockers {
		state.Blockers = append(state.Blockers, blockerState{b.Code, b.PredictionID})
	}
	sort.Slice(state.Requirements, func(i, j int) bool { return state.Requirements[i].RequirementID < state.Requirements[j].RequirementID })
	sort.Slice(state.Blockers, func(i, j int) bool {
		if state.Blockers[i].Code == state.Blockers[j].Code {
			return state.Blockers[i].PredictionID < state.Blockers[j].PredictionID
		}
		return state.Blockers[i].Code < state.Blockers[j].Code
	})
	for _, r := range state.Requirements {
		if !boundedToken(r.RequirementID, 128) || r.Kind.Validate() != nil || r.Status.Validate() != nil || !boundedToken(r.ReasonCode, 256) {
			return "", fmt.Errorf("invalid blocking requirement state")
		}
	}
	return canonicalHash("block_", state)
}
