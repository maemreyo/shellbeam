package decisionprotocol

import (
	"fmt"
	"sort"
	"time"
)

type EpisodeKind string

const (
	EpisodeDiagnosis       EpisodeKind = "DIAGNOSIS"
	EpisodeOptionSelection EpisodeKind = "OPTION_SELECTION"
	EpisodeClaimEvaluation EpisodeKind = "CLAIM_EVALUATION"
	EpisodePlanSelection   EpisodeKind = "PLAN_SELECTION"
)

func (k EpisodeKind) Validate() error {
	switch k {
	case EpisodeDiagnosis, EpisodeOptionSelection, EpisodeClaimEvaluation, EpisodePlanSelection:
		return nil
	}
	return fmt.Errorf("invalid episode kind %q", k)
}

type DecisionBudget struct {
	MaxExperimentsStarted *uint64 `json:"max_experiments_started,omitempty"`
	MaxLinkedOperations   *uint64 `json:"max_linked_operations,omitempty"`
	MaxMachineWallMS      *uint64 `json:"max_machine_wall_ms,omitempty"`
}

func (b DecisionBudget) Validate() error {
	for _, v := range []*uint64{b.MaxExperimentsStarted, b.MaxLinkedOperations, b.MaxMachineWallMS} {
		if v != nil && *v == 0 {
			return fmt.Errorf("decision budget limit must be positive")
		}
	}
	return nil
}

type RequirementKind string

const (
	RequirementCandidateChallenge   RequirementKind = "CANDIDATE_CHALLENGE"
	RequirementPredictionEvaluation RequirementKind = "PREDICTION_EVALUATION"
	RequirementDiscrimination       RequirementKind = "DISCRIMINATION"
	RequirementVerifierAssessment   RequirementKind = "VERIFIER_ASSESSMENT"
)

func (k RequirementKind) Validate() error {
	switch k {
	case RequirementCandidateChallenge, RequirementPredictionEvaluation, RequirementDiscrimination, RequirementVerifierAssessment:
		return nil
	}
	return fmt.Errorf("invalid requirement kind %q", k)
}

type CandidateChallengeRequirement struct {
	MinimumDistinctLineages uint64 `json:"minimum_distinct_lineages"`
}
type PredictionEvaluationRequirement struct {
	MinimumEvaluatedPredictions uint64           `json:"minimum_evaluated_predictions"`
	Roles                       []PredictionRole `json:"roles"`
}
type DiscriminationOutcome string

const (
	DiscriminationAttempted DiscriminationOutcome = "ATTEMPTED"
	DiscriminationRealized  DiscriminationOutcome = "REALIZED"
)

type DiscriminationRequirement struct {
	MinimumQualifyingExperiments uint64                `json:"minimum_qualifying_experiments"`
	RequiredOutcome              DiscriminationOutcome `json:"required_outcome"`
}
type VerifierAssessmentRequirement struct {
	MinimumSupportingAssessments uint64       `json:"minimum_supporting_assessments"`
	RequiredContextClass         ContextClass `json:"required_context_class,omitempty"`
	DistinctActorRefs            bool         `json:"distinct_actor_refs"`
}

type DecisionRequirement struct {
	RequirementID        string                           `json:"requirement_id"`
	Kind                 RequirementKind                  `json:"kind"`
	CandidateChallenge   *CandidateChallengeRequirement   `json:"candidate_challenge,omitempty"`
	PredictionEvaluation *PredictionEvaluationRequirement `json:"prediction_evaluation,omitempty"`
	Discrimination       *DiscriminationRequirement       `json:"discrimination,omitempty"`
	VerifierAssessment   *VerifierAssessmentRequirement   `json:"verifier_assessment,omitempty"`
}

func (r DecisionRequirement) Validate() error {
	if !boundedToken(r.RequirementID, 128) || r.Kind.Validate() != nil {
		return fmt.Errorf("invalid decision requirement identity")
	}
	branches := 0
	if r.CandidateChallenge != nil {
		branches++
	}
	if r.PredictionEvaluation != nil {
		branches++
	}
	if r.Discrimination != nil {
		branches++
	}
	if r.VerifierAssessment != nil {
		branches++
	}
	if branches != 1 {
		return fmt.Errorf("decision requirement requires exactly one payload")
	}
	switch r.Kind {
	case RequirementCandidateChallenge:
		if r.CandidateChallenge == nil || r.CandidateChallenge.MinimumDistinctLineages < 2 {
			return fmt.Errorf("invalid candidate challenge")
		}
	case RequirementPredictionEvaluation:
		if r.PredictionEvaluation == nil || r.PredictionEvaluation.MinimumEvaluatedPredictions == 0 || len(r.PredictionEvaluation.Roles) == 0 {
			return fmt.Errorf("invalid prediction evaluation")
		}
		seen := map[PredictionRole]bool{}
		for _, role := range r.PredictionEvaluation.Roles {
			if role != PredictionRequired && role != PredictionDiscriminator {
				return fmt.Errorf("unsupported V1 prediction evaluation role")
			}
			if seen[role] {
				return fmt.Errorf("invalid duplicate prediction role")
			}
			seen[role] = true
		}
	case RequirementDiscrimination:
		if r.Discrimination == nil || r.Discrimination.MinimumQualifyingExperiments == 0 || (r.Discrimination.RequiredOutcome != DiscriminationAttempted && r.Discrimination.RequiredOutcome != DiscriminationRealized) {
			return fmt.Errorf("invalid discrimination requirement")
		}
	case RequirementVerifierAssessment:
		if r.VerifierAssessment == nil || r.VerifierAssessment.MinimumSupportingAssessments == 0 || r.VerifierAssessment.DistinctActorRefs {
			return fmt.Errorf("invalid verifier assessment requirement")
		}
		if r.VerifierAssessment.RequiredContextClass != "" && r.VerifierAssessment.RequiredContextClass.ValidateQualified() != nil {
			return fmt.Errorf("invalid required context class")
		}
	}
	return nil
}

type OverridePolicy struct {
	Allowed                bool            `json:"allowed"`
	RequiredAuthorityClass *AuthorityClass `json:"required_authority_class,omitempty"`
}

func (p OverridePolicy) Validate() error {
	if p.Allowed {
		if p.RequiredAuthorityClass == nil || p.RequiredAuthorityClass.Validate() != nil {
			return fmt.Errorf("allowed override requires authority class")
		}
		return nil
	}
	if p.RequiredAuthorityClass != nil {
		return fmt.Errorf("disabled override must omit authority class")
	}
	return nil
}

type PolicyContent struct {
	PolicyID       string                `json:"policy_id"`
	EpisodeKinds   []EpisodeKind         `json:"episode_kinds"`
	Requirements   []DecisionRequirement `json:"requirements"`
	Budget         DecisionBudget        `json:"budget,omitempty"`
	OverridePolicy OverridePolicy        `json:"override_policy"`
}

func (p PolicyContent) Validate() error {
	if !boundedToken(p.PolicyID, 128) || len(p.EpisodeKinds) == 0 || len(p.EpisodeKinds) > 4 || len(p.Requirements) > 8 || p.Budget.Validate() != nil || p.OverridePolicy.Validate() != nil {
		return fmt.Errorf("invalid decision policy header")
	}
	seenKinds := map[EpisodeKind]bool{}
	for _, kind := range p.EpisodeKinds {
		if kind.Validate() != nil || seenKinds[kind] {
			return fmt.Errorf("invalid duplicate episode kind")
		}
		seenKinds[kind] = true
	}
	seenIDs := map[string]bool{}
	seenReqKinds := map[RequirementKind]bool{}
	for i := range p.Requirements {
		r := p.Requirements[i]
		if err := r.Validate(); err != nil {
			return fmt.Errorf("requirement %d: %w", i, err)
		}
		if seenIDs[r.RequirementID] || seenReqKinds[r.Kind] {
			return fmt.Errorf("duplicate requirement id or kind")
		}
		seenIDs[r.RequirementID], seenReqKinds[r.Kind] = true, true
	}
	return nil
}

func canonicalPolicy(p PolicyContent) PolicyContent {
	out := p
	out.EpisodeKinds = append([]EpisodeKind(nil), p.EpisodeKinds...)
	sort.Slice(out.EpisodeKinds, func(i, j int) bool { return out.EpisodeKinds[i] < out.EpisodeKinds[j] })
	out.Requirements = append([]DecisionRequirement(nil), p.Requirements...)
	sort.Slice(out.Requirements, func(i, j int) bool {
		if out.Requirements[i].Kind == out.Requirements[j].Kind {
			return out.Requirements[i].RequirementID < out.Requirements[j].RequirementID
		}
		return out.Requirements[i].Kind < out.Requirements[j].Kind
	})
	for i := range out.Requirements {
		if pe := out.Requirements[i].PredictionEvaluation; pe != nil {
			copyPE := *pe
			copyPE.Roles = append([]PredictionRole(nil), pe.Roles...)
			sort.Slice(copyPE.Roles, func(a, b int) bool { return copyPE.Roles[a] < copyPE.Roles[b] })
			out.Requirements[i].PredictionEvaluation = &copyPE
		}
	}
	return out
}

func PolicyDigest(p PolicyContent) (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	return canonicalHash("pol_", canonicalPolicy(p))
}

type DecisionPolicySnapshot struct {
	SchemaVersion int           `json:"schema_version"`
	RepositoryID  string        `json:"repository_id"`
	PolicyDigest  string        `json:"policy_digest"`
	Content       PolicyContent `json:"content"`
}

func (s DecisionPolicySnapshot) Validate() error {
	if s.SchemaVersion != 1 || !boundedToken(s.RepositoryID, 128) || !validDerived(s.PolicyDigest, "pol_") {
		return fmt.Errorf("invalid decision policy snapshot")
	}
	want, err := PolicyDigest(s.Content)
	if err != nil || want != s.PolicyDigest {
		return fmt.Errorf("decision policy digest mismatch")
	}
	return nil
}

const AuthorityExplicitCaller = "explicit_caller"

type DecisionPolicyActivation struct {
	ActivationID         string    `json:"activation_id"`
	RepositoryID         string    `json:"repository_id"`
	PolicyDigest         string    `json:"policy_digest"`
	ProposalGeneration   string    `json:"proposal_generation"`
	ActivationGeneration string    `json:"activation_generation"`
	Authority            string    `json:"authority"`
	ActorRef             string    `json:"actor_ref"`
	ActivatedAt          time.Time `json:"activated_at"`
}

func (a DecisionPolicyActivation) Validate() error {
	if !boundedToken(a.ActivationID, 128) || !boundedToken(a.RepositoryID, 128) || !validDerived(a.PolicyDigest, "pol_") || !validGeneration(a.ProposalGeneration) || !validGeneration(a.ActivationGeneration) || a.Authority != AuthorityExplicitCaller || !boundedToken(a.ActorRef, 192) || !validTime(a.ActivatedAt) {
		return fmt.Errorf("invalid decision policy activation")
	}
	return nil
}
