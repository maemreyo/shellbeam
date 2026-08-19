package decisionprotocol

import (
	"fmt"
	"time"
)

type AuthorityClass struct {
	Domain  string `json:"domain"`
	ClassID string `json:"class_id"`
	Version int    `json:"version"`
}

func (a AuthorityClass) Validate() error {
	if !boundedToken(a.Domain, 128) || !boundedToken(a.ClassID, 128) || a.Version <= 0 {
		return fmt.Errorf("invalid authority class")
	}
	return nil
}
func (a AuthorityClass) Equal(b AuthorityClass) bool {
	return a.Domain == b.Domain && a.ClassID == b.ClassID && a.Version == b.Version
}

type AuthorityActionKind string

const AuthorityActionCommitSelectionOverride AuthorityActionKind = "COMMIT_SELECTION_OVERRIDE"

func (a AuthorityActionKind) Validate() error {
	if a == AuthorityActionCommitSelectionOverride {
		return nil
	}
	return fmt.Errorf("invalid authority action kind %q", a)
}

type AuthorityScope struct {
	RepositoryID string              `json:"repository_id"`
	EpisodeID    EpisodeID           `json:"episode_id,omitempty"`
	ActionKind   AuthorityActionKind `json:"action_kind"`
}

func (s AuthorityScope) Validate() error {
	if !boundedToken(s.RepositoryID, 128) || s.ActionKind.Validate() != nil {
		return fmt.Errorf("invalid authority scope")
	}
	if s.EpisodeID != "" && !validID(s.EpisodeID) {
		return fmt.Errorf("invalid authority scope episode")
	}
	return nil
}

type ResolverRef struct {
	ProviderID        string `json:"provider_id"`
	ProviderVersion   string `json:"provider_version"`
	CapabilityVersion string `json:"capability_version"`
}

func (r ResolverRef) Validate() error {
	if !boundedToken(r.ProviderID, 128) || !boundedToken(r.ProviderVersion, 128) || !boundedToken(r.CapabilityVersion, 128) {
		return fmt.Errorf("invalid resolver ref")
	}
	return nil
}

type DecisionAuthorityAttestation struct {
	SchemaVersion  int            `json:"schema_version"`
	AttestationID  string         `json:"attestation_id"`
	ActorRef       string         `json:"actor_ref"`
	AuthorityClass AuthorityClass `json:"authority_class"`
	Scope          AuthorityScope `json:"scope"`
	Resolver       ResolverRef    `json:"resolver"`
	IssuedAt       time.Time      `json:"issued_at"`
	ExpiresAt      *time.Time     `json:"expires_at,omitempty"`
	ProvenanceRef  string         `json:"provenance_ref"`
}

func (a DecisionAuthorityAttestation) Validate() error {
	if a.SchemaVersion != 1 || !boundedToken(a.AttestationID, 192) || !boundedToken(a.ActorRef, 192) || a.AuthorityClass.Validate() != nil || a.Scope.Validate() != nil || a.Resolver.Validate() != nil || !validTime(a.IssuedAt) || !boundedToken(a.ProvenanceRef, 512) {
		return fmt.Errorf("invalid decision authority attestation")
	}
	if a.ExpiresAt != nil && (!validTime(*a.ExpiresAt) || !a.ExpiresAt.After(a.IssuedAt)) {
		return fmt.Errorf("invalid authority expiry")
	}
	return nil
}

type QualificationStatus string

const (
	QualificationQualified     QualificationStatus = "QUALIFIED"
	QualificationExpired       QualificationStatus = "EXPIRED"
	QualificationRevoked       QualificationStatus = "REVOKED"
	QualificationScopeMismatch QualificationStatus = "SCOPE_MISMATCH"
	QualificationClassMismatch QualificationStatus = "CLASS_MISMATCH"
	QualificationUnknown       QualificationStatus = "UNKNOWN"
	QualificationUnavailable   QualificationStatus = "UNAVAILABLE"
)

func (s QualificationStatus) Validate() error {
	switch s {
	case QualificationQualified, QualificationExpired, QualificationRevoked, QualificationScopeMismatch, QualificationClassMismatch, QualificationUnknown, QualificationUnavailable:
		return nil
	}
	return fmt.Errorf("invalid qualification status")
}

type OverrideAuthorization struct {
	AuthorityAttestationRef string         `json:"authority_attestation_ref"`
	AuthorityClass          AuthorityClass `json:"authority_class"`
	ActorRef                string         `json:"actor_ref"`
	Resolver                ResolverRef    `json:"resolver"`
	ValidatedAt             time.Time      `json:"validated_at"`
	QualificationCutDigest  string         `json:"qualification_cut_digest,omitempty"`
}

func (a OverrideAuthorization) Validate() error {
	if !boundedToken(a.AuthorityAttestationRef, 192) || a.AuthorityClass.Validate() != nil || !boundedToken(a.ActorRef, 192) || a.Resolver.Validate() != nil || !validTime(a.ValidatedAt) {
		return fmt.Errorf("invalid override authorization")
	}
	if a.QualificationCutDigest != "" && !validDerived(a.QualificationCutDigest, "cut_") {
		return fmt.Errorf("invalid override qualification cut")
	}
	return nil
}
