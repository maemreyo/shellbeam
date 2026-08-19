package decisionprotocol

import (
	"fmt"
	"time"
)

type SelectionProposal struct {
	ProposalID  string      `json:"proposal_id"`
	EpisodeID   EpisodeID   `json:"episode_id"`
	CandidateID CandidateID `json:"candidate_id"`
	ActorRef    string      `json:"actor_ref"`
	Rationale   string      `json:"rationale,omitempty"`
	CreatedAt   time.Time   `json:"created_at"`
}

func (p SelectionProposal) Validate() error {
	if !boundedToken(p.ProposalID, 192) || !validID(p.EpisodeID) || !validID(p.CandidateID) || !boundedToken(p.ActorRef, 192) || !validTime(p.CreatedAt) {
		return fmt.Errorf("invalid selection proposal")
	}
	if p.Rationale != "" && !boundedToken(p.Rationale, 8192) {
		return fmt.Errorf("invalid proposal rationale")
	}
	return nil
}

type DecisionOverride struct {
	OverrideID                string      `json:"override_id"`
	EpisodeID                 EpisodeID   `json:"episode_id"`
	CandidateID               CandidateID `json:"candidate_id"`
	PolicyDigest              string      `json:"policy_digest"`
	ProjectionDigest          string      `json:"projection_digest"`
	BlockingRequirementDigest string      `json:"blocking_requirement_digest"`
	BlockingRequirements      []string    `json:"blocking_requirements"`
	ActorRef                  string      `json:"actor_ref"`
	AuthorityAttestationRef   string      `json:"authority_attestation_ref"`
	Reason                    string      `json:"reason"`
	CreatedAt                 time.Time   `json:"created_at"`
}

func (o DecisionOverride) Validate() error {
	if !boundedToken(o.OverrideID, 192) || !validID(o.EpisodeID) || !validID(o.CandidateID) || !validDerived(o.PolicyDigest, "pol_") || !validDerived(o.ProjectionDigest, "proj_") || !validDerived(o.BlockingRequirementDigest, "block_") || len(o.BlockingRequirements) == 0 || !boundedToken(o.ActorRef, 192) || !boundedToken(o.AuthorityAttestationRef, 192) || !boundedToken(o.Reason, 8192) || !validTime(o.CreatedAt) {
		return fmt.Errorf("invalid decision override")
	}
	return uniqueStrings(o.BlockingRequirements, 32, 256, false)
}

type SelectionCommit struct {
	CommitID                  string                 `json:"commit_id"`
	EpisodeID                 EpisodeID              `json:"episode_id"`
	CandidateID               CandidateID            `json:"candidate_id"`
	PolicyDigest              string                 `json:"policy_digest"`
	ProjectionDigest          string                 `json:"projection_digest"`
	SourceGeneration          string                 `json:"source_generation"`
	OverrideRef               string                 `json:"override_ref,omitempty"`
	OverrideAuthorization     *OverrideAuthorization `json:"override_authorization,omitempty"`
	IdempotencyKey            string                 `json:"idempotency_key"`
	SemanticIntentFingerprint string                 `json:"semantic_intent_fingerprint"`
	CommittedByActorRef       string                 `json:"committed_by_actor_ref"`
	CommittedAt               time.Time              `json:"committed_at"`
}

func (c SelectionCommit) Validate() error {
	if !boundedToken(c.CommitID, 192) || !validID(c.EpisodeID) || !validID(c.CandidateID) || !validDerived(c.PolicyDigest, "pol_") || !validDerived(c.ProjectionDigest, "proj_") || !boundedToken(c.SourceGeneration, 192) || !boundedToken(c.IdempotencyKey, 256) || !validDerived(c.SemanticIntentFingerprint, "sel_") || !boundedToken(c.CommittedByActorRef, 192) || !validTime(c.CommittedAt) {
		return fmt.Errorf("invalid selection commit")
	}
	if (c.OverrideRef == "") != (c.OverrideAuthorization == nil) {
		return fmt.Errorf("override ref and authorization must be paired")
	}
	if c.OverrideRef != "" {
		if !boundedToken(c.OverrideRef, 192) || c.OverrideAuthorization.Validate() != nil {
			return fmt.Errorf("invalid override commit")
		}
	}
	return nil
}

type ClosureKind string

const ClosureUnresolved ClosureKind = "UNRESOLVED"

type DecisionClosure struct {
	EpisodeID            EpisodeID   `json:"episode_id"`
	Kind                 ClosureKind `json:"kind"`
	Reason               string      `json:"reason"`
	UnresolvedDimensions []string    `json:"unresolved_dimensions"`
	ActorRef             string      `json:"actor_ref"`
	ProjectionDigest     string      `json:"projection_digest"`
	ClosedAt             time.Time   `json:"closed_at"`
}

func (c DecisionClosure) Validate() error {
	if !validID(c.EpisodeID) || c.Kind != ClosureUnresolved || !boundedToken(c.Reason, 8192) || !boundedToken(c.ActorRef, 192) || !validDerived(c.ProjectionDigest, "proj_") || !validTime(c.ClosedAt) {
		return fmt.Errorf("invalid decision closure")
	}
	return uniqueStrings(c.UnresolvedDimensions, 128, 512, true)
}

type SelectionCommitIntent struct {
	EpisodeID        EpisodeID   `json:"episode_id"`
	CandidateID      CandidateID `json:"candidate_id"`
	ActorRef         string      `json:"actor_ref"`
	PolicyDigest     string      `json:"policy_digest"`
	ProjectionDigest string      `json:"projection_digest"`
	SourceGeneration string      `json:"source_generation"`
	Override         bool        `json:"override"`
	OverrideRef      string      `json:"override_ref,omitempty"`
}

func (i SelectionCommitIntent) Validate() error {
	if !validID(i.EpisodeID) || !validID(i.CandidateID) || !boundedToken(i.ActorRef, 192) || !validDerived(i.PolicyDigest, "pol_") || !validDerived(i.ProjectionDigest, "proj_") || !boundedToken(i.SourceGeneration, 192) {
		return fmt.Errorf("invalid selection commit intent")
	}
	if i.Override && !boundedToken(i.OverrideRef, 192) {
		return fmt.Errorf("override intent requires ref")
	}
	if !i.Override && i.OverrideRef != "" {
		return fmt.Errorf("normal intent must omit override ref")
	}
	return nil
}
func SelectionIntentFingerprint(i SelectionCommitIntent) (string, error) {
	if err := i.Validate(); err != nil {
		return "", err
	}
	return canonicalHash("sel_", i)
}
