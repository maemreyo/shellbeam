package bridge

import dp "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"

type DecisionCandidateInput struct {
	CandidateID   string `json:"candidate_id"`
	SemanticClaim string `json:"semantic_claim"`
	CandidateKind string `json:"candidate_kind,omitempty"`
}
type DecisionPredictionInput struct {
	PredictionID string                  `json:"prediction_id"`
	CandidateID  string                  `json:"candidate_id"`
	Role         dp.PredictionRole       `json:"role"`
	Predicate    dp.ObservationPredicate `json:"predicate"`
}
type DecisionAssessmentInput struct {
	AssessmentID             string          `json:"assessment_id"`
	DeclaredContextClass     dp.ContextClass `json:"declared_context_class"`
	DeclaredProviderIdentity string          `json:"declared_provider_identity,omitempty"`
	PreferredCandidates      []string        `json:"preferred_candidates"`
	SemanticRejections       []string        `json:"semantic_rejections,omitempty"`
	Rationale                string          `json:"rationale,omitempty"`
}
type DecisionAuthorityMaterializeInput struct {
	RequiredAuthorityClass dp.AuthorityClass `json:"required_authority_class"`
	RequiredScope          dp.AuthorityScope `json:"required_scope"`
}
type DecisionPolicySnapshotInput struct {
	Content dp.PolicyContent `json:"content"`
}

type DecisionRequest struct {
	EpisodeID                    string                             `json:"episode_id,omitempty"`
	EpisodeKind                  dp.EpisodeKind                     `json:"episode_kind,omitempty"`
	PredecessorEpisodeID         string                             `json:"predecessor_episode_id,omitempty"`
	CandidateID                  string                             `json:"candidate_id,omitempty"`
	ParentCandidateID            string                             `json:"parent_candidate_id,omitempty"`
	ExperimentID                 string                             `json:"experiment_id,omitempty"`
	Policy                       *DecisionPolicySnapshotInput       `json:"policy,omitempty"`
	ActivationID                 string                             `json:"activation_id,omitempty"`
	PolicyDigest                 string                             `json:"policy_digest,omitempty"`
	ProposalGeneration           string                             `json:"proposal_generation,omitempty"`
	ExpectedPreviousPolicyDigest string                             `json:"expected_previous_policy_digest,omitempty"`
	Candidate                    *DecisionCandidateInput            `json:"candidate,omitempty"`
	Prediction                   *DecisionPredictionInput           `json:"prediction,omitempty"`
	Assessment                   *DecisionAssessmentInput           `json:"assessment,omitempty"`
	AuthorityRequest             *DecisionAuthorityMaterializeInput `json:"authority_request,omitempty"`
	AuthorityAttestationRef      string                             `json:"authority_attestation_ref,omitempty"`
	ActorRef                     string                             `json:"actor_ref,omitempty"`
	ExpectedPolicyDigest         string                             `json:"expected_policy_digest,omitempty"`
	ExpectedActivationRef        string                             `json:"expected_activation_ref,omitempty"`
	ExpectedProjectionDigest     string                             `json:"expected_projection_digest,omitempty"`
	BlockingRequirementDigest    string                             `json:"blocking_requirement_digest,omitempty"`
	IdempotencyKey               string                             `json:"idempotency_key,omitempty"`
	OverrideRef                  string                             `json:"override_ref,omitempty"`
	AbortPhase                   dp.AbortPhase                      `json:"abort_phase,omitempty"`
	UnresolvedDimensions         *[]string                          `json:"unresolved_dimensions,omitempty"`
	Reason                       string                             `json:"reason,omitempty"`
}

type DecisionResponse struct {
	Policy          *dp.PolicySnapshot               `json:"policy,omitempty"`
	Activation      *dp.PolicyActivation             `json:"activation,omitempty"`
	Episode         *dp.Episode                      `json:"episode,omitempty"`
	Projection      *dp.DecisionProjection           `json:"projection,omitempty"`
	Evaluation      *dp.DecisionProtocolEvaluation   `json:"evaluation,omitempty"`
	Candidate       *dp.Candidate                    `json:"candidate,omitempty"`
	Experiment      *dp.Experiment                   `json:"experiment,omitempty"`
	Prediction      *dp.PredictionBinding            `json:"prediction,omitempty"`
	Seal            *dp.ExperimentSeal               `json:"seal,omitempty"`
	Assessment      *dp.VerifierAssessment           `json:"assessment,omitempty"`
	Proposal        *dp.SelectionProposal            `json:"proposal,omitempty"`
	Override        *dp.DecisionOverride             `json:"override,omitempty"`
	Selection       *dp.SelectionCommit              `json:"selection,omitempty"`
	Closure         *dp.DecisionClosure              `json:"closure,omitempty"`
	Attestation     *dp.DecisionAuthorityAttestation `json:"attestation,omitempty"`
	AuthorityStatus dp.QualificationStatus           `json:"authority_status,omitempty"`
}

func isDecisionProtocolAction(action string) bool {
	switch action {
	case "decision.policy.snapshot",
		"decision.policy.activate",
		"decision.create",
		"decision.inspect",
		"decision.evaluate",
		"decision.close_unresolved",
		"decision.candidate.create",
		"decision.candidate.revise",
		"decision.experiment.define",
		"decision.prediction.bind",
		"decision.experiment.seal",
		"decision.experiment.close",
		"decision.experiment.abort",
		"decision.assessment.record",
		"decision.selection.propose",
		"decision.override.create",
		"decision.selection.commit",
		"decision.authority.materialize":
		return true
	default:
		return false
	}
}
