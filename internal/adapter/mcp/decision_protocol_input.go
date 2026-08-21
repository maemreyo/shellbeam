package mcp

import (
	"encoding/json"
	"fmt"
	"strings"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	dp "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
)

type DecisionCandidateInput = bridge.DecisionCandidateInput
type DecisionPredictionInput = bridge.DecisionPredictionInput
type DecisionAssessmentInput = bridge.DecisionAssessmentInput
type DecisionAuthorityMaterializeInput = bridge.DecisionAuthorityMaterializeInput
type DecisionPolicySnapshotInput = bridge.DecisionPolicySnapshotInput
type DecisionRequest = bridge.DecisionRequest

func validateDecisionMCPInput(v input, raw []byte) error {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return err
	}
	if err := validateDecisionMCPRawFields(envelope["decision"], v.Action); err != nil {
		return err
	}
	return validateDecisionMCPRequest(v.Action, v.Decision)
}

func validateDecisionStartEnvelope(v input) error {
	if err := validateHermeticStartInput(v); err != nil {
		return err
	}
	if v.ExperimentID == "" {
		return nil
	}
	_, err := dp.ParseExperimentID(v.ExperimentID)
	return err
}

type decisionMCPActionFieldSpec struct{ required, optional []string }

var decisionMCPActionFieldsV1 = map[string]decisionMCPActionFieldSpec{
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

func isDecisionProtocolMCPAction(action string) bool {
	_, ok := decisionMCPActionFieldsV1[action]
	return ok
}

func validateDecisionMCPRawFields(raw json.RawMessage, action string) error {
	if len(raw) == 0 || string(raw) == "null" {
		return decisionMCPInputError("decision", "missing decision payload")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return decisionMCPInputError("decision", "invalid decision payload")
	}
	spec, ok := decisionMCPActionFieldsV1[action]
	if !ok {
		return decisionMCPInputError("action", "unknown decision action")
	}
	allowed := map[string]bool{}
	for _, f := range spec.required {
		allowed[f] = true
		if _, present := fields[f]; !present {
			return decisionMCPInputError(f, "missing required decision field")
		}
	}
	for _, f := range spec.optional {
		allowed[f] = true
	}
	for f := range fields {
		if !allowed[f] {
			return decisionMCPInputError(f, "unexpected decision field")
		}
	}
	return nil
}

func validateDecisionMCPRequest(action string, req *DecisionRequest) error {
	if req == nil {
		return decisionMCPInputError("decision", "missing decision payload")
	}
	switch action {
	case "decision.policy.snapshot":
		if req.Policy == nil || req.Policy.Content.Validate() != nil {
			return decisionMCPInputError("policy", "invalid decision policy content")
		}
	case "decision.policy.activate":
		if !boundedDecisionMCPText(req.ActivationID, 192) || !validVerificationPolicyDigest(req.PolicyDigest) || !validVerificationGeneration(req.ProposalGeneration) || (req.ExpectedPreviousPolicyDigest != "absent" && !validVerificationPolicyDigest(req.ExpectedPreviousPolicyDigest)) || !boundedDecisionMCPText(req.ActorRef, 192) {
			return decisionMCPInputError("decision", "invalid policy activation request")
		}
	case "decision.create":
		if !validDecisionMCPEpisodeID(req.EpisodeID) || req.EpisodeKind.Validate() != nil || !boundedDecisionMCPText(req.ActorRef, 192) || (req.PredecessorEpisodeID != "" && !validDecisionMCPEpisodeID(req.PredecessorEpisodeID)) || (req.ExpectedPolicyDigest != "" && !validVerificationPolicyDigest(req.ExpectedPolicyDigest)) || (req.ExpectedActivationRef != "" && !boundedDecisionMCPText(req.ExpectedActivationRef, 192)) {
			return decisionMCPInputError("decision", "invalid episode create request")
		}
	case "decision.inspect":
		if !validDecisionMCPEpisodeID(req.EpisodeID) || (req.CandidateID != "" && !validDecisionMCPCandidateID(req.CandidateID)) {
			return decisionMCPInputError("decision", "invalid inspect request")
		}
	case "decision.evaluate":
		if !validDecisionMCPEpisodeID(req.EpisodeID) || !validDecisionMCPCandidateID(req.CandidateID) {
			return decisionMCPInputError("decision", "invalid evaluate request")
		}
	case "decision.close_unresolved":
		if err := validateDecisionMCPCloseUnresolved(req); err != nil {
			return err
		}
	case "decision.candidate.create":
		if !validDecisionMCPEpisodeID(req.EpisodeID) || !boundedDecisionMCPText(req.ActorRef, 192) || validateDecisionMCPCandidateInput(req.Candidate) != nil {
			return decisionMCPInputError("decision", "invalid candidate create request")
		}
	case "decision.candidate.revise":
		if !validDecisionMCPEpisodeID(req.EpisodeID) || !validDecisionMCPCandidateID(req.ParentCandidateID) || !boundedDecisionMCPText(req.ActorRef, 192) || validateDecisionMCPCandidateInput(req.Candidate) != nil {
			return decisionMCPInputError("decision", "invalid candidate revise request")
		}
	case "decision.experiment.define":
		if !validDecisionMCPEpisodeID(req.EpisodeID) || !validDecisionMCPExperimentID(req.ExperimentID) || !boundedDecisionMCPText(req.ActorRef, 192) {
			return decisionMCPInputError("decision", "invalid experiment define request")
		}
	case "decision.prediction.bind":
		if !validDecisionMCPEpisodeID(req.EpisodeID) || !validDecisionMCPExperimentID(req.ExperimentID) || validateDecisionMCPPredictionInput(req.Prediction) != nil {
			return decisionMCPInputError("decision", "invalid prediction bind request")
		}
	case "decision.experiment.seal", "decision.experiment.close":
		if !validDecisionMCPExperimentID(req.ExperimentID) || !boundedDecisionMCPText(req.ActorRef, 192) {
			return decisionMCPInputError("decision", "invalid experiment request")
		}
	case "decision.experiment.abort":
		if !validDecisionMCPExperimentID(req.ExperimentID) || req.AbortPhase.Validate() != nil || !boundedDecisionMCPText(req.ActorRef, 192) || !boundedDecisionMCPText(req.Reason, 2048) {
			return decisionMCPInputError("decision", "invalid experiment abort request")
		}
	case "decision.assessment.record":
		if !validDecisionMCPEpisodeID(req.EpisodeID) || !boundedDecisionMCPText(req.ActorRef, 192) || validateDecisionMCPAssessmentInput(req.Assessment) != nil {
			return decisionMCPInputError("decision", "invalid assessment request")
		}
	case "decision.selection.propose":
		if !validDecisionMCPEpisodeID(req.EpisodeID) || !validDecisionMCPCandidateID(req.CandidateID) || !boundedDecisionMCPText(req.ActorRef, 192) || (req.Reason != "" && len(req.Reason) > 2048) {
			return decisionMCPInputError("decision", "invalid selection proposal")
		}
	case "decision.override.create":
		if !validDecisionMCPEpisodeID(req.EpisodeID) || !validDecisionMCPCandidateID(req.CandidateID) || !validVerificationPolicyDigest(req.ExpectedPolicyDigest) || !validDecisionMCPDerivedDigest(req.ExpectedProjectionDigest, "proj_") || !validDecisionMCPDerivedDigest(req.BlockingRequirementDigest, "block_") || !boundedDecisionMCPText(req.AuthorityAttestationRef, 192) || !boundedDecisionMCPText(req.Reason, 2048) {
			return decisionMCPInputError("decision", "invalid override request")
		}
	case "decision.selection.commit":
		if !validDecisionMCPEpisodeID(req.EpisodeID) || !validDecisionMCPCandidateID(req.CandidateID) || !boundedDecisionMCPText(req.ActorRef, 192) || !validVerificationPolicyDigest(req.ExpectedPolicyDigest) || !validDecisionMCPDerivedDigest(req.ExpectedProjectionDigest, "proj_") || !boundedDecisionMCPText(req.IdempotencyKey, 192) || (req.OverrideRef != "" && !boundedDecisionMCPText(req.OverrideRef, 192)) {
			return decisionMCPInputError("decision", "invalid selection commit request")
		}
	case "decision.authority.materialize":
		if req.AuthorityRequest == nil || req.AuthorityRequest.RequiredAuthorityClass.Validate() != nil || req.AuthorityRequest.RequiredScope.Validate() != nil {
			return decisionMCPInputError("authority_request", "invalid authority request")
		}
	default:
		return decisionMCPInputError("action", "unknown decision action")
	}
	return nil
}

func validateDecisionMCPCloseUnresolved(req *DecisionRequest) error {
	if !validDecisionMCPEpisodeID(req.EpisodeID) || !boundedDecisionMCPText(req.ActorRef, 192) || !validDecisionMCPDerivedDigest(req.ExpectedProjectionDigest, "proj_") || !boundedDecisionMCPText(req.Reason, 2048) || req.UnresolvedDimensions == nil || len(*req.UnresolvedDimensions) > 64 {
		return decisionMCPInputError("decision", "invalid close unresolved request")
	}
	for _, dimension := range *req.UnresolvedDimensions {
		if !boundedDecisionMCPText(dimension, 256) {
			return decisionMCPInputError("unresolved_dimensions", "invalid unresolved dimension")
		}
	}
	return nil
}

func validateDecisionMCPCandidateInput(v *DecisionCandidateInput) error {
	if v == nil || !validDecisionMCPCandidateID(v.CandidateID) || !boundedDecisionMCPText(v.SemanticClaim, 8192) || (v.CandidateKind != "" && len(v.CandidateKind) > 128) {
		return fmt.Errorf("invalid candidate")
	}
	return nil
}
func validateDecisionMCPPredictionInput(v *DecisionPredictionInput) error {
	if v == nil || !validDecisionMCPPredictionID(v.PredictionID) || !validDecisionMCPCandidateID(v.CandidateID) || v.Role.Validate() != nil || v.Predicate.Validate() != nil {
		return fmt.Errorf("invalid prediction")
	}
	return nil
}
func validateDecisionMCPAssessmentInput(v *DecisionAssessmentInput) error {
	if v == nil || !boundedDecisionMCPText(v.AssessmentID, 192) || v.DeclaredContextClass.ValidateDeclared() != nil || len(v.PreferredCandidates) == 0 || len(v.PreferredCandidates) > 64 || len(v.SemanticRejections) > 64 || len(v.DeclaredProviderIdentity) > 256 || len(v.Rationale) > 4096 {
		return fmt.Errorf("invalid assessment")
	}
	for _, id := range append(append([]string{}, v.PreferredCandidates...), v.SemanticRejections...) {
		if !validDecisionMCPCandidateID(id) {
			return fmt.Errorf("invalid assessment candidate")
		}
	}
	return nil
}
func validDecisionMCPEpisodeID(v string) bool   { _, err := dp.ParseEpisodeID(v); return err == nil }
func validDecisionMCPCandidateID(v string) bool { _, err := dp.ParseCandidateID(v); return err == nil }
func validDecisionMCPExperimentID(v string) bool {
	_, err := dp.ParseExperimentID(v)
	return err == nil
}
func validDecisionMCPPredictionID(v string) bool {
	_, err := dp.ParsePredictionID(v)
	return err == nil
}
func validDecisionMCPDerivedDigest(v, prefix string) bool {
	return strings.HasPrefix(v, prefix) && len(v) == len(prefix)+64 && validDecisionLowerHex(v[len(prefix):])
}
func boundedDecisionMCPText(v string, max int) bool { return v != "" && len(v) <= max }
func decisionMCPInputError(field, msg string) error {
	return fmt.Errorf("decision field %s: %s", field, msg)
}

func validDecisionLowerHex(v string) bool {
	if len(v) != 64 {
		return false
	}
	for _, c := range v {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func cloneDecisionMCPRequest(in *DecisionRequest) *bridge.DecisionRequest {
	if in == nil {
		return nil
	}
	out := *in
	if in.UnresolvedDimensions != nil {
		values := append([]string(nil), (*in.UnresolvedDimensions)...)
		out.UnresolvedDimensions = &values
	}
	if in.Policy != nil {
		value := *in.Policy
		out.Policy = &value
	}
	if in.Candidate != nil {
		value := *in.Candidate
		out.Candidate = &value
	}
	if in.Prediction != nil {
		value := *in.Prediction
		out.Prediction = &value
	}
	if in.Assessment != nil {
		value := *in.Assessment
		value.PreferredCandidates = append([]string(nil), in.Assessment.PreferredCandidates...)
		value.SemanticRejections = append([]string(nil), in.Assessment.SemanticRejections...)
		out.Assessment = &value
	}
	if in.AuthorityRequest != nil {
		value := *in.AuthorityRequest
		out.AuthorityRequest = &value
	}
	return &out
}
