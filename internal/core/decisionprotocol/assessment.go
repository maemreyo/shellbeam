package decisionprotocol

import (
	"fmt"
	"sort"
	"time"
)

type ContextClass string

const (
	ContextSameContext        ContextClass = "SAME_CONTEXT"
	ContextIndependentContext ContextClass = "INDEPENDENT_CONTEXT"
	ContextIndependentSample  ContextClass = "INDEPENDENT_SAMPLE"
	ContextIndependentModel   ContextClass = "INDEPENDENT_MODEL"
	ContextHuman              ContextClass = "HUMAN"
	ContextUnknown            ContextClass = "UNKNOWN"
)

func (c ContextClass) ValidateDeclared() error {
	switch c {
	case ContextSameContext, ContextIndependentContext, ContextIndependentSample, ContextIndependentModel, ContextHuman, ContextUnknown:
		return nil
	}
	return fmt.Errorf("invalid declared context class %q", c)
}
func (c ContextClass) ValidateQualified() error {
	switch c {
	case ContextSameContext, ContextIndependentContext, ContextIndependentSample, ContextIndependentModel, ContextHuman:
		return nil
	}
	return fmt.Errorf("invalid qualified context class %q", c)
}

type ContextQualification struct {
	ProviderID             string    `json:"provider_id"`
	ProviderVersion        string    `json:"provider_version"`
	CapabilityVersion      string    `json:"capability_version"`
	QualificationCutDigest string    `json:"qualification_cut_digest,omitempty"`
	QualifiedAt            time.Time `json:"qualified_at"`
}

func (q ContextQualification) Validate() error {
	if !boundedToken(q.ProviderID, 128) || !boundedToken(q.ProviderVersion, 128) || !boundedToken(q.CapabilityVersion, 128) || !validTime(q.QualifiedAt) {
		return fmt.Errorf("invalid context qualification")
	}
	if q.QualificationCutDigest != "" && !validDerived(q.QualificationCutDigest, "cut_") {
		return fmt.Errorf("invalid qualification cut digest")
	}
	return nil
}

type VerifierAssessment struct {
	AssessmentID             string                `json:"assessment_id"`
	EpisodeID                EpisodeID             `json:"episode_id"`
	ActorRef                 string                `json:"actor_ref"`
	DeclaredContextClass     ContextClass          `json:"declared_context_class"`
	QualifiedContextClass    ContextClass          `json:"qualified_context_class,omitempty"`
	ContextQualification     *ContextQualification `json:"context_qualification,omitempty"`
	DeclaredProviderIdentity string                `json:"declared_provider_identity,omitempty"`
	PreferredCandidates      []CandidateID         `json:"preferred_candidates"`
	SemanticRejections       []CandidateID         `json:"semantic_rejections,omitempty"`
	Rationale                string                `json:"rationale"`
	CreatedAt                time.Time             `json:"created_at"`
}

func (a VerifierAssessment) Validate() error {
	if !boundedToken(a.AssessmentID, 192) || !validID(a.EpisodeID) || !boundedToken(a.ActorRef, 192) || a.DeclaredContextClass.ValidateDeclared() != nil || len(a.PreferredCandidates) == 0 || !boundedToken(a.Rationale, 8192) || !validTime(a.CreatedAt) {
		return fmt.Errorf("invalid verifier assessment")
	}
	if a.DeclaredProviderIdentity != "" && !boundedToken(a.DeclaredProviderIdentity, 256) {
		return fmt.Errorf("invalid declared provider identity")
	}
	if (a.QualifiedContextClass == "") != (a.ContextQualification == nil) {
		return fmt.Errorf("qualified context and qualification must be paired")
	}
	if a.QualifiedContextClass != "" && (a.QualifiedContextClass.ValidateQualified() != nil || a.ContextQualification.Validate() != nil) {
		return fmt.Errorf("invalid context qualification")
	}
	if err := validateCandidateSet(a.PreferredCandidates, 128); err != nil {
		return err
	}
	if err := validateCandidateSet(a.SemanticRejections, 128); err != nil {
		return err
	}
	return nil
}

func validateCandidateSet(values []CandidateID, max int) error {
	if len(values) > max {
		return fmt.Errorf("too many candidates")
	}
	seen := map[CandidateID]bool{}
	for _, v := range values {
		if !validID(v) || seen[v] {
			return fmt.Errorf("invalid duplicate candidate")
		}
		seen[v] = true
	}
	return nil
}

type VerifierSemanticState struct {
	ActorRef              string        `json:"actor_ref"`
	QualifiedContextClass ContextClass  `json:"qualified_context_class,omitempty"`
	PreferredCandidates   []CandidateID `json:"preferred_candidates"`
	SemanticRejections    []CandidateID `json:"semantic_rejections,omitempty"`
}

func canonicalVerifierState(values []VerifierSemanticState) []VerifierSemanticState {
	out := append([]VerifierSemanticState(nil), values...)
	for i := range out {
		out[i].PreferredCandidates = sortedCandidateIDs(out[i].PreferredCandidates)
		out[i].SemanticRejections = sortedCandidateIDs(out[i].SemanticRejections)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ActorRef == out[j].ActorRef {
			return out[i].QualifiedContextClass < out[j].QualifiedContextClass
		}
		return out[i].ActorRef < out[j].ActorRef
	})
	return out
}
