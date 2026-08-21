package decisionprotocol

import (
	"encoding/json"
	"fmt"
)

type EpisodeID string
type CandidateID string
type ExperimentID string
type PredictionID string
type LinkID string
type BindingID string
type RecordSeq uint64

type RecordKind string

const (
	RecordPolicySnapshot               RecordKind = "decision_policy_snapshot"
	RecordPolicyActivation             RecordKind = "decision_policy_activation"
	RecordEpisode                      RecordKind = "decision_episode"
	RecordCandidate                    RecordKind = "decision_candidate"
	RecordExperiment                   RecordKind = "decision_experiment"
	RecordExperimentSeal               RecordKind = "experiment_seal"
	RecordPredictionBinding            RecordKind = "prediction_binding"
	RecordExperimentExecutionLink      RecordKind = "experiment_execution_link"
	RecordExperimentObservationBinding RecordKind = "experiment_observation_binding"
	RecordExperimentClosure            RecordKind = "experiment_closure"
	RecordExperimentAbort              RecordKind = "experiment_abort"
	RecordVerifierAssessment           RecordKind = "verifier_assessment"
	RecordSelectionProposal            RecordKind = "selection_proposal"
	RecordAuthorityAttestation         RecordKind = "decision_authority_attestation"
	RecordOverride                     RecordKind = "decision_override"
	RecordSelectionCommit              RecordKind = "selection_commit"
	RecordClosure                      RecordKind = "decision_closure"
)

var canonicalRecordKinds = []RecordKind{
	RecordPolicySnapshot, RecordPolicyActivation, RecordEpisode, RecordCandidate,
	RecordExperiment, RecordExperimentSeal, RecordPredictionBinding,
	RecordExperimentExecutionLink, RecordExperimentObservationBinding,
	RecordExperimentClosure, RecordExperimentAbort, RecordVerifierAssessment,
	RecordSelectionProposal, RecordAuthorityAttestation, RecordOverride,
	RecordSelectionCommit, RecordClosure,
}

func CanonicalRecordKinds() []RecordKind { return append([]RecordKind(nil), canonicalRecordKinds...) }

func (k RecordKind) Validate() error {
	for _, allowed := range canonicalRecordKinds {
		if k == allowed {
			return nil
		}
	}
	return fmt.Errorf("invalid decision protocol record kind %q", k)
}

type CanonicalRecordEnvelope struct {
	SchemaVersion      int             `json:"schema_version"`
	CanonicalRecordSeq RecordSeq       `json:"canonical_record_seq"`
	Kind               RecordKind      `json:"kind"`
	Body               json.RawMessage `json:"body"`
}

func (e CanonicalRecordEnvelope) Validate() error {
	if e.SchemaVersion != 1 || e.CanonicalRecordSeq == 0 || e.Kind.Validate() != nil || len(e.Body) == 0 || !json.Valid(e.Body) {
		return fmt.Errorf("invalid canonical decision protocol envelope")
	}
	return validateCanonicalBody(e.Kind, e.Body)
}

func validateCanonicalBody(kind RecordKind, body json.RawMessage) error {
	var target interface{ Validate() error }
	switch kind {
	case RecordPolicySnapshot:
		target = &DecisionPolicySnapshot{}
	case RecordPolicyActivation:
		target = &DecisionPolicyActivation{}
	case RecordEpisode:
		target = &DecisionEpisode{}
	case RecordCandidate:
		target = &DecisionCandidate{}
	case RecordExperiment:
		target = &DecisionExperiment{}
	case RecordExperimentSeal:
		target = &ExperimentSeal{}
	case RecordPredictionBinding:
		target = &PredictionBinding{}
	case RecordExperimentExecutionLink:
		target = &ExperimentExecutionLink{}
	case RecordExperimentObservationBinding:
		target = &ExperimentObservationBinding{}
	case RecordExperimentClosure:
		target = &ExperimentClosure{}
	case RecordExperimentAbort:
		target = &ExperimentAbort{}
	case RecordVerifierAssessment:
		target = &VerifierAssessment{}
	case RecordSelectionProposal:
		target = &SelectionProposal{}
	case RecordAuthorityAttestation:
		target = &DecisionAuthorityAttestation{}
	case RecordOverride:
		target = &DecisionOverride{}
	case RecordSelectionCommit:
		target = &SelectionCommit{}
	case RecordClosure:
		target = &DecisionClosure{}
	default:
		return fmt.Errorf("unsupported canonical body kind %q", kind)
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("decode %s body: %w", kind, err)
	}
	return target.Validate()
}

type ReasonCode string

const (
	ReasonCandidateRevisionConflict            ReasonCode = "CANDIDATE_REVISION_CONFLICT"
	ReasonExperimentAlreadySealed              ReasonCode = "EXPERIMENT_ALREADY_SEALED"
	ReasonExperimentExecutionLimitReached      ReasonCode = "EXPERIMENT_EXECUTION_LIMIT_REACHED"
	ReasonExperimentNotSealed                  ReasonCode = "EXPERIMENT_NOT_SEALED"
	ReasonObservationNotSettled                ReasonCode = "OBSERVATION_NOT_SETTLED"
	ReasonExperimentObservationBindingConflict ReasonCode = "EXPERIMENT_OBSERVATION_BINDING_CONFLICT"
	ReasonStaleEpisodeSourceGeneration         ReasonCode = "STALE_EPISODE_SOURCE_GENERATION"
	ReasonProjectionConflict                   ReasonCode = "PROJECTION_CONFLICT"
	ReasonPolicyConflict                       ReasonCode = "POLICY_CONFLICT"
	ReasonEpisodeTerminalConflict              ReasonCode = "EPISODE_TERMINAL_CONFLICT"
	ReasonTerminalSelectionConflict            ReasonCode = "TERMINAL_SELECTION_CONFLICT"
	ReasonIdempotencyConflict                  ReasonCode = "IDEMPOTENCY_CONFLICT"
	ReasonProtocolBlocked                      ReasonCode = "PROTOCOL_BLOCKED"
	ReasonProtocolIndeterminate                ReasonCode = "PROTOCOL_INDETERMINATE"
	ReasonOverrideScopeStale                   ReasonCode = "OVERRIDE_SCOPE_STALE"
	ReasonOverrideAuthorityNotAdmissible       ReasonCode = "OVERRIDE_AUTHORITY_NOT_ADMISSIBLE"
	ReasonAuthorityRequirementUnavailable      ReasonCode = "AUTHORITY_REQUIREMENT_UNAVAILABLE"
)

var frozenReasonCodes = []ReasonCode{
	ReasonCandidateRevisionConflict, ReasonExperimentAlreadySealed,
	ReasonExperimentExecutionLimitReached, ReasonExperimentNotSealed,
	ReasonObservationNotSettled, ReasonExperimentObservationBindingConflict,
	ReasonStaleEpisodeSourceGeneration, ReasonProjectionConflict, ReasonPolicyConflict,
	ReasonEpisodeTerminalConflict, ReasonTerminalSelectionConflict, ReasonIdempotencyConflict,
	ReasonProtocolBlocked, ReasonProtocolIndeterminate, ReasonOverrideScopeStale,
	ReasonOverrideAuthorityNotAdmissible, ReasonAuthorityRequirementUnavailable,
}

func ReasonCodes() []ReasonCode { return append([]ReasonCode(nil), frozenReasonCodes...) }
