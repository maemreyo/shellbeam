package decisionprotocol

import (
	"fmt"
	"sort"
	"time"
)

type DecisionExperiment struct {
	SchemaVersion      int          `json:"schema_version"`
	ExperimentID       ExperimentID `json:"experiment_id"`
	EpisodeID          EpisodeID    `json:"episode_id"`
	DeclaredByActorRef string       `json:"declared_by_actor_ref"`
	DeclaredAt         time.Time    `json:"declared_at"`
}

func (e DecisionExperiment) Validate() error {
	if e.SchemaVersion != 1 || !validID(e.ExperimentID) || !validID(e.EpisodeID) || !boundedToken(e.DeclaredByActorRef, 192) || !validTime(e.DeclaredAt) {
		return fmt.Errorf("invalid decision experiment")
	}
	return nil
}

type DecisionProjectionCutRef struct {
	EpisodeID                EpisodeID `json:"episode_id"`
	CanonicalRecordHighWater RecordSeq `json:"canonical_record_high_water"`
}

func (r DecisionProjectionCutRef) Validate() error {
	if !validID(r.EpisodeID) || r.CanonicalRecordHighWater == 0 {
		return fmt.Errorf("invalid projection cut ref")
	}
	return nil
}

type PotentialDiscriminationPair struct {
	TargetCandidateID     CandidateID `json:"target_candidate_id"`
	ChallengerCandidateID CandidateID `json:"challenger_candidate_id"`
	DimensionKey          string      `json:"dimension_key"`
}

type ExperimentSeal struct {
	ExperimentID                  ExperimentID                  `json:"experiment_id"`
	SourceGeneration              string                        `json:"source_generation"`
	SealedPredictionDigest        string                        `json:"sealed_prediction_digest"`
	BaseProjectionCutRef          DecisionProjectionCutRef      `json:"base_projection_cut_ref"`
	BaseCandidateProjectionDigest string                        `json:"base_candidate_projection_digest"`
	PotentialDiscriminationPairs  []PotentialDiscriminationPair `json:"potential_discrimination_pairs,omitempty"`
	SealedAt                      time.Time                     `json:"sealed_at"`
}

func (s ExperimentSeal) Validate() error {
	if !validID(s.ExperimentID) || !boundedToken(s.SourceGeneration, 192) || !validDerived(s.SealedPredictionDigest, "pred_") || s.BaseProjectionCutRef.Validate() != nil || !validDerived(s.BaseCandidateProjectionDigest, "proj_") || !validTime(s.SealedAt) {
		return fmt.Errorf("invalid experiment seal")
	}
	seen := map[string]bool{}
	for _, p := range s.PotentialDiscriminationPairs {
		if !validID(p.TargetCandidateID) || !validID(p.ChallengerCandidateID) || p.TargetCandidateID == p.ChallengerCandidateID || !validDerived(p.DimensionKey, "dim_") {
			return fmt.Errorf("invalid discrimination pair")
		}
		key := string(p.TargetCandidateID) + "\x00" + string(p.ChallengerCandidateID) + "\x00" + p.DimensionKey
		if seen[key] {
			return fmt.Errorf("duplicate discrimination pair")
		}
		seen[key] = true
	}
	return nil
}

type PredictionBinding struct {
	PredictionID     PredictionID         `json:"prediction_id"`
	EpisodeID        EpisodeID            `json:"episode_id"`
	ExperimentID     ExperimentID         `json:"experiment_id"`
	CandidateID      CandidateID          `json:"candidate_id"`
	Role             PredictionRole       `json:"role"`
	Predicate        ObservationPredicate `json:"predicate"`
	SourceGeneration string               `json:"source_generation"`
	CommittedAt      time.Time            `json:"committed_at"`
}

func (p PredictionBinding) Validate() error {
	if !validID(p.PredictionID) || !validID(p.EpisodeID) || !validID(p.ExperimentID) || !validID(p.CandidateID) || p.Role.Validate() != nil || p.Predicate.Validate() != nil || !boundedToken(p.SourceGeneration, 192) || !validTime(p.CommittedAt) {
		return fmt.Errorf("invalid prediction binding")
	}
	return nil
}

type ExperimentExecutionLink struct {
	SchemaVersion                         int          `json:"schema_version"`
	LinkID                                LinkID       `json:"link_id"`
	ExperimentID                          ExperimentID `json:"experiment_id"`
	OperationID                           string       `json:"operation_id"`
	SessionID                             string       `json:"session_id"`
	WorkspaceID                           string       `json:"workspace_id"`
	SourceGeneration                      string       `json:"source_generation"`
	AcceptedRequestFingerprint            string       `json:"accepted_request_fingerprint"`
	AcceptedExecutionFingerprint          string       `json:"accepted_execution_fingerprint"`
	AcceptedObservationBindingFingerprint string       `json:"accepted_observation_binding_fingerprint"`
	AdmittedAt                            time.Time    `json:"admitted_at"`
}

func (l ExperimentExecutionLink) Validate() error {
	if l.SchemaVersion != 1 || !validID(l.LinkID) || !validID(l.ExperimentID) || !boundedToken(l.OperationID, 192) || !boundedToken(l.SessionID, 192) || !boundedToken(l.WorkspaceID, 192) || !boundedToken(l.SourceGeneration, 192) || !validFingerprint(l.AcceptedRequestFingerprint) || !validFingerprint(l.AcceptedExecutionFingerprint) || !validFingerprint(l.AcceptedObservationBindingFingerprint) || !validTime(l.AdmittedAt) {
		return fmt.Errorf("invalid experiment execution link")
	}
	return nil
}

type PredictionResult struct {
	PredictionID PredictionID               `json:"prediction_id"`
	Status       PredictionEvaluationStatus `json:"status"`
	ReasonCode   string                     `json:"reason_code,omitempty"`
	BasisRefs    []string                   `json:"basis_refs,omitempty"`
}

func (r PredictionResult) Validate() error {
	if !validID(r.PredictionID) || r.Status.Validate() != nil {
		return fmt.Errorf("invalid prediction result")
	}
	if r.ReasonCode != "" && !boundedToken(r.ReasonCode, 256) {
		return fmt.Errorf("invalid prediction result reason")
	}
	return uniqueStrings(r.BasisRefs, 256, 2048, false)
}

type ExperimentObservationBinding struct {
	SchemaVersion               int                `json:"schema_version"`
	BindingID                   BindingID          `json:"binding_id"`
	ExperimentID                ExperimentID       `json:"experiment_id"`
	OperationID                 string             `json:"operation_id"`
	SourceGeneration            string             `json:"source_generation"`
	ObservationSemanticsVersion int                `json:"observation_semantics_version"`
	DerivationCutDigest         string             `json:"derivation_cut_digest"`
	PredictionResults           []PredictionResult `json:"prediction_results"`
	MaterializedAt              time.Time          `json:"materialized_at"`
}

func (b ExperimentObservationBinding) Validate() error {
	if b.SchemaVersion != 1 || !validID(b.BindingID) || !validID(b.ExperimentID) || !boundedToken(b.OperationID, 192) || !boundedToken(b.SourceGeneration, 192) || b.ObservationSemanticsVersion != 1 || !validDerived(b.DerivationCutDigest, "cut_") || len(b.PredictionResults) == 0 || !validTime(b.MaterializedAt) {
		return fmt.Errorf("invalid observation binding")
	}
	seen := map[PredictionID]bool{}
	for _, r := range b.PredictionResults {
		if r.Validate() != nil || seen[r.PredictionID] {
			return fmt.Errorf("invalid duplicate prediction result")
		}
		seen[r.PredictionID] = true
	}
	return nil
}

type ExperimentClosure struct {
	SchemaVersion        int          `json:"schema_version"`
	ClosureID            string       `json:"closure_id"`
	ExperimentID         ExperimentID `json:"experiment_id"`
	ObservationBindingID BindingID    `json:"observation_binding_id"`
	ClosedByActorRef     string       `json:"closed_by_actor_ref"`
	ClosedAt             time.Time    `json:"closed_at"`
}

func (c ExperimentClosure) Validate() error {
	if c.SchemaVersion != 1 || !boundedToken(c.ClosureID, 192) || !validID(c.ExperimentID) || !validID(c.ObservationBindingID) || !boundedToken(c.ClosedByActorRef, 192) || !validTime(c.ClosedAt) {
		return fmt.Errorf("invalid experiment closure")
	}
	return nil
}

type AbortPhase string

const (
	AbortBeforeExecution    AbortPhase = "BEFORE_EXECUTION"
	AbortAfterExecutionLink AbortPhase = "AFTER_EXECUTION_LINK"
)

func (p AbortPhase) Validate() error {
	if p == AbortBeforeExecution || p == AbortAfterExecutionLink {
		return nil
	}
	return fmt.Errorf("invalid abort phase")
}

type ExperimentAbort struct {
	SchemaVersion     int          `json:"schema_version"`
	AbortID           string       `json:"abort_id"`
	ExperimentID      ExperimentID `json:"experiment_id"`
	Phase             AbortPhase   `json:"phase"`
	ExecutionLinkID   LinkID       `json:"execution_link_id,omitempty"`
	Reason            string       `json:"reason"`
	AbortedByActorRef string       `json:"aborted_by_actor_ref"`
	AbortedAt         time.Time    `json:"aborted_at"`
}

func (a ExperimentAbort) Validate() error {
	if a.SchemaVersion != 1 || !boundedToken(a.AbortID, 192) || !validID(a.ExperimentID) || a.Phase.Validate() != nil || !boundedToken(a.Reason, 2048) || !boundedToken(a.AbortedByActorRef, 192) || !validTime(a.AbortedAt) {
		return fmt.Errorf("invalid experiment abort")
	}
	if a.Phase == AbortBeforeExecution && a.ExecutionLinkID != "" {
		return fmt.Errorf("before execution abort must omit link")
	}
	if a.Phase == AbortAfterExecutionLink && !validID(a.ExecutionLinkID) {
		return fmt.Errorf("after link abort requires link")
	}
	return nil
}

func sealedPredictionDigest(bindings []PredictionBinding) (string, error) {
	out := append([]PredictionBinding(nil), bindings...)
	for _, b := range out {
		if err := b.Validate(); err != nil {
			return "", err
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PredictionID < out[j].PredictionID })
	return canonicalHash("pred_", out)
}
