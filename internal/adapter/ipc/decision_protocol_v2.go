package ipc

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	dp "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
	"github.com/maemreyo/shellbeam/internal/core/failure"
)

var decisionProtocolActionsV1 = []string{
	"decision.policy.snapshot", "decision.policy.activate", "decision.create", "decision.inspect", "decision.evaluate", "decision.close_unresolved",
	"decision.candidate.create", "decision.candidate.revise", "decision.experiment.define", "decision.prediction.bind", "decision.experiment.seal", "decision.experiment.close", "decision.experiment.abort",
	"decision.assessment.record", "decision.selection.propose", "decision.override.create", "decision.selection.commit", "decision.authority.materialize",
}

type DecisionCandidateInputV1 struct {
	CandidateID   string `json:"candidate_id"`
	SemanticClaim string `json:"semantic_claim"`
	CandidateKind string `json:"candidate_kind,omitempty"`
}

type DecisionPredictionInputV1 struct {
	PredictionID string                  `json:"prediction_id"`
	CandidateID  string                  `json:"candidate_id"`
	Role         dp.PredictionRole       `json:"role"`
	Predicate    dp.ObservationPredicate `json:"predicate"`
}

type DecisionAssessmentInputV1 struct {
	AssessmentID             string          `json:"assessment_id"`
	DeclaredContextClass     dp.ContextClass `json:"declared_context_class"`
	DeclaredProviderIdentity string          `json:"declared_provider_identity,omitempty"`
	PreferredCandidates      []string        `json:"preferred_candidates"`
	SemanticRejections       []string        `json:"semantic_rejections,omitempty"`
	Rationale                string          `json:"rationale,omitempty"`
}

type DecisionAuthorityMaterializeInputV1 struct {
	RequiredAuthorityClass dp.AuthorityClass `json:"required_authority_class"`
	RequiredScope          dp.AuthorityScope `json:"required_scope"`
}

type DecisionPolicySnapshotInputV1 struct {
	Content dp.PolicyContent `json:"content"`
}

type DecisionRequestV1 struct {
	EpisodeID                    string                               `json:"episode_id,omitempty"`
	EpisodeKind                  dp.EpisodeKind                       `json:"episode_kind,omitempty"`
	PredecessorEpisodeID         string                               `json:"predecessor_episode_id,omitempty"`
	CandidateID                  string                               `json:"candidate_id,omitempty"`
	ParentCandidateID            string                               `json:"parent_candidate_id,omitempty"`
	ExperimentID                 string                               `json:"experiment_id,omitempty"`
	Policy                       *DecisionPolicySnapshotInputV1       `json:"policy,omitempty"`
	ActivationID                 string                               `json:"activation_id,omitempty"`
	PolicyDigest                 string                               `json:"policy_digest,omitempty"`
	ProposalGeneration           string                               `json:"proposal_generation,omitempty"`
	ExpectedPreviousPolicyDigest string                               `json:"expected_previous_policy_digest,omitempty"`
	Candidate                    *DecisionCandidateInputV1            `json:"candidate,omitempty"`
	Prediction                   *DecisionPredictionInputV1           `json:"prediction,omitempty"`
	Assessment                   *DecisionAssessmentInputV1           `json:"assessment,omitempty"`
	AuthorityRequest             *DecisionAuthorityMaterializeInputV1 `json:"authority_request,omitempty"`
	AuthorityAttestationRef      string                               `json:"authority_attestation_ref,omitempty"`
	ActorRef                     string                               `json:"actor_ref,omitempty"`
	ExpectedPolicyDigest         string                               `json:"expected_policy_digest,omitempty"`
	ExpectedActivationRef        string                               `json:"expected_activation_ref,omitempty"`
	ExpectedProjectionDigest     string                               `json:"expected_projection_digest,omitempty"`
	BlockingRequirementDigest    string                               `json:"blocking_requirement_digest,omitempty"`
	IdempotencyKey               string                               `json:"idempotency_key,omitempty"`
	OverrideRef                  string                               `json:"override_ref,omitempty"`
	AbortPhase                   dp.AbortPhase                        `json:"abort_phase,omitempty"`
	UnresolvedDimensions         *[]string                            `json:"unresolved_dimensions,omitempty"`
	Reason                       string                               `json:"reason,omitempty"`
}

type decisionActionFieldSpec struct{ required, optional []string }

var decisionActionFieldsV1 = map[string]decisionActionFieldSpec{
	"decision.policy.snapshot":       {[]string{"policy"}, nil},
	"decision.policy.activate":       {[]string{"activation_id", "policy_digest", "proposal_generation", "expected_previous_policy_digest", "actor_ref"}, nil},
	"decision.create":                {[]string{"episode_id", "episode_kind", "actor_ref"}, []string{"predecessor_episode_id", "expected_policy_digest", "expected_activation_ref"}},
	"decision.inspect":               {[]string{"episode_id"}, []string{"candidate_id"}},
	"decision.evaluate":              {[]string{"episode_id", "candidate_id"}, nil},
	"decision.close_unresolved":      {[]string{"episode_id", "actor_ref", "expected_projection_digest", "reason", "unresolved_dimensions"}, nil},
	"decision.candidate.create":      {[]string{"episode_id", "candidate", "actor_ref"}, nil},
	"decision.candidate.revise":      {[]string{"episode_id", "parent_candidate_id", "candidate", "actor_ref"}, nil},
	"decision.experiment.define":     {[]string{"episode_id", "experiment_id", "actor_ref"}, nil},
	"decision.prediction.bind":       {[]string{"episode_id", "experiment_id", "prediction"}, nil},
	"decision.experiment.seal":       {[]string{"experiment_id", "actor_ref"}, nil},
	"decision.experiment.close":      {[]string{"experiment_id", "actor_ref"}, nil},
	"decision.experiment.abort":      {[]string{"experiment_id", "abort_phase", "actor_ref", "reason"}, nil},
	"decision.assessment.record":     {[]string{"episode_id", "assessment", "actor_ref"}, nil},
	"decision.selection.propose":     {[]string{"episode_id", "candidate_id", "actor_ref"}, []string{"reason"}},
	"decision.override.create":       {[]string{"episode_id", "candidate_id", "expected_policy_digest", "expected_projection_digest", "blocking_requirement_digest", "authority_attestation_ref", "reason"}, nil},
	"decision.selection.commit":      {[]string{"episode_id", "candidate_id", "actor_ref", "expected_policy_digest", "expected_projection_digest", "idempotency_key"}, []string{"override_ref"}},
	"decision.authority.materialize": {[]string{"authority_request"}, nil},
}

func isDecisionProtocolActionV2(action string) bool {
	_, ok := decisionActionFieldsV1[action]
	return ok
}

func validateDecisionRawFieldsV2(raw json.RawMessage, action string) error {
	if len(raw) == 0 || string(raw) == "null" {
		return decisionInputError("decision", "missing decision payload")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return decisionInputError("decision", "invalid decision payload")
	}
	spec, ok := decisionActionFieldsV1[action]
	if !ok {
		return decisionInputError("action", "unknown decision action")
	}
	allowed := map[string]bool{}
	for _, f := range spec.required {
		allowed[f] = true
		if _, present := fields[f]; !present {
			return decisionInputError(f, "missing required decision field")
		}
	}
	for _, f := range spec.optional {
		allowed[f] = true
	}
	for f := range fields {
		if !allowed[f] {
			return decisionInputError(f, "unexpected decision field")
		}
	}
	return nil
}

func validateDecisionRequestV2(action string, req *DecisionRequestV1) error {
	if req == nil {
		return decisionInputError("decision", "missing decision payload")
	}
	switch action {
	case "decision.policy.snapshot":
		if req.Policy == nil || req.Policy.Content.Validate() != nil {
			return decisionInputError("policy", "invalid decision policy content")
		}
	case "decision.policy.activate":
		if !boundedDecisionText(req.ActivationID, 192) || !validPolicyDigestV2(req.PolicyDigest) || !validGenerationV2(req.ProposalGeneration) || (req.ExpectedPreviousPolicyDigest != "absent" && !validPolicyDigestV2(req.ExpectedPreviousPolicyDigest)) || !boundedDecisionText(req.ActorRef, 192) {
			return decisionInputError("decision", "invalid policy activation request")
		}
	case "decision.create":
		if !validEpisodeID(req.EpisodeID) || req.EpisodeKind.Validate() != nil || !boundedDecisionText(req.ActorRef, 192) || (req.PredecessorEpisodeID != "" && !validEpisodeID(req.PredecessorEpisodeID)) || (req.ExpectedPolicyDigest != "" && !validPolicyDigestV2(req.ExpectedPolicyDigest)) || (req.ExpectedActivationRef != "" && !boundedDecisionText(req.ExpectedActivationRef, 192)) {
			return decisionInputError("decision", "invalid episode create request")
		}
	case "decision.inspect":
		if !validEpisodeID(req.EpisodeID) || (req.CandidateID != "" && !validCandidateID(req.CandidateID)) {
			return decisionInputError("decision", "invalid inspect request")
		}
	case "decision.evaluate":
		if !validEpisodeID(req.EpisodeID) || !validCandidateID(req.CandidateID) {
			return decisionInputError("decision", "invalid evaluate request")
		}
	case "decision.close_unresolved":
		if err := validateDecisionCloseUnresolved(req); err != nil {
			return err
		}
	case "decision.candidate.create":
		if !validEpisodeID(req.EpisodeID) || !boundedDecisionText(req.ActorRef, 192) || validateDecisionCandidateInput(req.Candidate) != nil {
			return decisionInputError("decision", "invalid candidate create request")
		}
	case "decision.candidate.revise":
		if !validEpisodeID(req.EpisodeID) || !validCandidateID(req.ParentCandidateID) || !boundedDecisionText(req.ActorRef, 192) || validateDecisionCandidateInput(req.Candidate) != nil {
			return decisionInputError("decision", "invalid candidate revise request")
		}
	case "decision.experiment.define":
		if !validEpisodeID(req.EpisodeID) || !validExperimentID(req.ExperimentID) || !boundedDecisionText(req.ActorRef, 192) {
			return decisionInputError("decision", "invalid experiment define request")
		}
	case "decision.prediction.bind":
		if !validEpisodeID(req.EpisodeID) || !validExperimentID(req.ExperimentID) || validateDecisionPredictionInput(req.Prediction) != nil {
			return decisionInputError("decision", "invalid prediction bind request")
		}
	case "decision.experiment.seal", "decision.experiment.close":
		if !validExperimentID(req.ExperimentID) || !boundedDecisionText(req.ActorRef, 192) {
			return decisionInputError("decision", "invalid experiment request")
		}
	case "decision.experiment.abort":
		if !validExperimentID(req.ExperimentID) || req.AbortPhase.Validate() != nil || !boundedDecisionText(req.ActorRef, 192) || !boundedDecisionText(req.Reason, 2048) {
			return decisionInputError("decision", "invalid experiment abort request")
		}
	case "decision.assessment.record":
		if !validEpisodeID(req.EpisodeID) || !boundedDecisionText(req.ActorRef, 192) || validateDecisionAssessmentInput(req.Assessment) != nil {
			return decisionInputError("decision", "invalid assessment request")
		}
	case "decision.selection.propose":
		if !validEpisodeID(req.EpisodeID) || !validCandidateID(req.CandidateID) || !boundedDecisionText(req.ActorRef, 192) || (req.Reason != "" && len(req.Reason) > 2048) {
			return decisionInputError("decision", "invalid selection proposal")
		}
	case "decision.override.create":
		if !validEpisodeID(req.EpisodeID) || !validCandidateID(req.CandidateID) || !validPolicyDigestV2(req.ExpectedPolicyDigest) || !validDerivedDigest(req.ExpectedProjectionDigest, "proj_") || !validDerivedDigest(req.BlockingRequirementDigest, "block_") || !boundedDecisionText(req.AuthorityAttestationRef, 192) || !boundedDecisionText(req.Reason, 2048) {
			return decisionInputError("decision", "invalid override request")
		}
	case "decision.selection.commit":
		if !validEpisodeID(req.EpisodeID) || !validCandidateID(req.CandidateID) || !boundedDecisionText(req.ActorRef, 192) || !validPolicyDigestV2(req.ExpectedPolicyDigest) || !validDerivedDigest(req.ExpectedProjectionDigest, "proj_") || !boundedDecisionText(req.IdempotencyKey, 192) || (req.OverrideRef != "" && !boundedDecisionText(req.OverrideRef, 192)) {
			return decisionInputError("decision", "invalid selection commit request")
		}
	case "decision.authority.materialize":
		if req.AuthorityRequest == nil || req.AuthorityRequest.RequiredAuthorityClass.Validate() != nil || req.AuthorityRequest.RequiredScope.Validate() != nil {
			return decisionInputError("authority_request", "invalid authority request")
		}
	default:
		return decisionInputError("action", "unknown decision action")
	}
	return nil
}

func validateDecisionCloseUnresolved(req *DecisionRequestV1) error {
	if !validEpisodeID(req.EpisodeID) || !boundedDecisionText(req.ActorRef, 192) || !validDerivedDigest(req.ExpectedProjectionDigest, "proj_") || !boundedDecisionText(req.Reason, 2048) || req.UnresolvedDimensions == nil || len(*req.UnresolvedDimensions) > 64 {
		return decisionInputError("decision", "invalid close unresolved request")
	}
	for _, dimension := range *req.UnresolvedDimensions {
		if !boundedDecisionText(dimension, 256) {
			return decisionInputError("unresolved_dimensions", "invalid unresolved dimension")
		}
	}
	return nil
}

func validateDecisionCandidateInput(v *DecisionCandidateInputV1) error {
	if v == nil || !validCandidateID(v.CandidateID) || !boundedDecisionText(v.SemanticClaim, 8192) || (v.CandidateKind != "" && len(v.CandidateKind) > 128) {
		return fmt.Errorf("invalid candidate")
	}
	return nil
}
func validateDecisionPredictionInput(v *DecisionPredictionInputV1) error {
	if v == nil || !validPredictionID(v.PredictionID) || !validCandidateID(v.CandidateID) || v.Role.Validate() != nil || v.Predicate.Validate() != nil {
		return fmt.Errorf("invalid prediction")
	}
	return nil
}
func validateDecisionAssessmentInput(v *DecisionAssessmentInputV1) error {
	if v == nil || !boundedDecisionText(v.AssessmentID, 192) || v.DeclaredContextClass.ValidateDeclared() != nil || len(v.PreferredCandidates) == 0 || len(v.PreferredCandidates) > 64 || len(v.SemanticRejections) > 64 || len(v.DeclaredProviderIdentity) > 256 || len(v.Rationale) > 4096 {
		return fmt.Errorf("invalid assessment")
	}
	for _, id := range append(append([]string{}, v.PreferredCandidates...), v.SemanticRejections...) {
		if !validCandidateID(id) {
			return fmt.Errorf("invalid assessment candidate")
		}
	}
	return nil
}
func validEpisodeID(v string) bool    { _, err := dp.ParseEpisodeID(v); return err == nil }
func validCandidateID(v string) bool  { _, err := dp.ParseCandidateID(v); return err == nil }
func validExperimentID(v string) bool { _, err := dp.ParseExperimentID(v); return err == nil }
func validPredictionID(v string) bool { _, err := dp.ParsePredictionID(v); return err == nil }
func validDerivedDigest(v, prefix string) bool {
	return strings.HasPrefix(v, prefix) && len(v) == len(prefix)+64 && validLowerHexV2(v[len(prefix):])
}
func boundedDecisionText(v string, max int) bool { return v != "" && len(v) <= max }
func decisionInputError(field, msg string) error {
	return failure.New(failure.InvalidInput, map[string]string{"field": field}, fmt.Errorf("%s", msg))
}

func decisionActionNamesV2() []string {
	out := append([]string(nil), decisionProtocolActionsV1...)
	sort.Strings(out)
	return out
}

type DecisionResponseV1 struct {
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

type DecisionProtocolActions interface {
	DecisionProtocol(context.Context, string, DecisionRequestV1) (DecisionResponseV1, error)
}

func (s *Server) decisionProtocolV2(ctx context.Context, req RequestV2, resp *ResponseV2) error {
	actions, ok := s.actions.(DecisionProtocolActions)
	if !ok {
		return failure.New(failure.FeatureUnavailable, map[string]string{"action": req.Action}, fmt.Errorf("feature unavailable"))
	}
	if req.Decision == nil {
		return failure.New(failure.InvalidInput, map[string]string{"field": "decision"}, fmt.Errorf("missing decision payload"))
	}
	out, err := actions.DecisionProtocol(ctx, req.Action, *req.Decision)
	if err != nil {
		return err
	}
	resp.Decision = &out
	return nil
}
