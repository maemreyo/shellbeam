package decisionprotocol

import (
	"fmt"
	"time"
)

type EpisodeBaseline struct {
	SourceGeneration string `json:"source_generation"`
}
type EpisodePolicyBinding struct {
	PolicyID      string `json:"policy_id"`
	PolicyDigest  string `json:"policy_digest"`
	ActivationRef string `json:"activation_ref"`
}

type DecisionEpisode struct {
	SchemaVersion        int                  `json:"schema_version"`
	EpisodeID            EpisodeID            `json:"episode_id"`
	EpisodeKind          EpisodeKind          `json:"episode_kind"`
	RepositoryID         string               `json:"repository_id"`
	WorkspaceID          string               `json:"workspace_id"`
	PredecessorEpisodeID EpisodeID            `json:"predecessor_episode_id,omitempty"`
	Baseline             EpisodeBaseline      `json:"baseline"`
	PolicyBinding        EpisodePolicyBinding `json:"policy_binding"`
	CreatedByActorRef    string               `json:"created_by_actor_ref"`
	CreatedAt            time.Time            `json:"created_at"`
}

func (e DecisionEpisode) Validate() error {
	if e.SchemaVersion != 1 || !validID(e.EpisodeID) || e.EpisodeKind.Validate() != nil || !boundedToken(e.RepositoryID, 128) || !boundedToken(e.WorkspaceID, 192) || !boundedToken(e.Baseline.SourceGeneration, 192) || !boundedToken(e.PolicyBinding.PolicyID, 128) || !validDerived(e.PolicyBinding.PolicyDigest, "pol_") || !boundedToken(e.PolicyBinding.ActivationRef, 192) || !boundedToken(e.CreatedByActorRef, 192) || !validTime(e.CreatedAt) {
		return fmt.Errorf("invalid decision episode")
	}
	if e.PredecessorEpisodeID != "" && !validID(e.PredecessorEpisodeID) {
		return fmt.Errorf("invalid predecessor episode")
	}
	return nil
}

type DecisionCandidate struct {
	CandidateID        CandidateID `json:"candidate_id"`
	EpisodeID          EpisodeID   `json:"episode_id"`
	SemanticClaim      string      `json:"semantic_claim"`
	CandidateKind      string      `json:"candidate_kind,omitempty"`
	RevisesCandidateID CandidateID `json:"revises_candidate_id,omitempty"`
	DeclaredByActorRef string      `json:"declared_by_actor_ref"`
	DeclaredAt         time.Time   `json:"declared_at"`
}

func (c DecisionCandidate) Validate() error {
	if !validID(c.CandidateID) || !validID(c.EpisodeID) || !boundedToken(c.SemanticClaim, 8192) || !boundedToken(c.DeclaredByActorRef, 192) || !validTime(c.DeclaredAt) {
		return fmt.Errorf("invalid decision candidate")
	}
	if c.CandidateKind != "" && !boundedToken(c.CandidateKind, 128) {
		return fmt.Errorf("invalid candidate kind")
	}
	if c.RevisesCandidateID != "" && (!validID(c.RevisesCandidateID) || c.RevisesCandidateID == c.CandidateID) {
		return fmt.Errorf("invalid candidate revision parent")
	}
	return nil
}
