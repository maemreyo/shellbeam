package verification

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"time"
)

var activationIDPattern = regexp.MustCompile(`^act_[A-Za-z0-9_-]{1,120}$`)
var waiverIDPattern = regexp.MustCompile(`^wv_[A-Za-z0-9_-]{1,121}$`)

func ValidateActivationID(v string) error {
	if !activationIDPattern.MatchString(v) {
		return fmt.Errorf("invalid activation id")
	}
	return nil
}
func ValidateWaiverID(v string) error {
	if !waiverIDPattern.MatchString(v) {
		return fmt.Errorf("invalid waiver id")
	}
	return nil
}

type PolicyActivationIntent struct {
	ActivationID            string `json:"activation_id"`
	RepositoryID            string `json:"repository_id"`
	PreviousEffectiveDigest string `json:"previous_effective_policy_digest"`
	ProposedPolicyDigest    string `json:"proposed_policy_digest"`
	ProposalGeneration      string `json:"proposal_generation"`
	Authority               string `json:"authority"`
	Actor                   string `json:"actor"`
}
type PolicyActivationCommit struct {
	Intent               PolicyActivationIntent `json:"intent"`
	ProposalOrigin       ProposalOrigin         `json:"proposal_origin"`
	ProfileOrigin        string                 `json:"profile_origin,omitempty"`
	ActivationGeneration string                 `json:"activation_generation"`
}
type VerificationWaiverIntent struct {
	WaiverID     string    `json:"waiver_id"`
	RepositoryID string    `json:"repository_id"`
	PolicyDigest string    `json:"policy_digest"`
	RuleID       string    `json:"rule_id"`
	Phase        Phase     `json:"phase"`
	Generation   string    `json:"generation,omitempty"`
	CheckpointID string    `json:"checkpoint_id,omitempty"`
	Authority    string    `json:"authority"`
	Actor        string    `json:"actor"`
	Reason       string    `json:"reason"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	ExpiresPhase Phase     `json:"expires_phase,omitempty"`
}
type WaiverRevocationIntent struct {
	RepositoryID string `json:"repository_id"`
	WaiverID     string `json:"waiver_id"`
	Authority    string `json:"authority"`
	Actor        string `json:"actor"`
}

type PolicyActivation struct {
	SchemaVersion           int            `json:"schema_version"`
	ActivationID            string         `json:"activation_id"`
	IntentFingerprint       string         `json:"intent_fingerprint"`
	RepositoryID            string         `json:"repository_id"`
	PreviousEffectiveDigest string         `json:"previous_effective_policy_digest"`
	ProposedPolicyDigest    string         `json:"proposed_policy_digest"`
	ProposalOrigin          ProposalOrigin `json:"proposal_origin"`
	ProfileOrigin           string         `json:"profile_origin,omitempty"`
	ProposalGeneration      string         `json:"proposal_generation"`
	ActivationGeneration    string         `json:"activation_generation"`
	Authority               string         `json:"authority"`
	Actor                   string         `json:"actor"`
	ActivatedAt             time.Time      `json:"activated_at"`
}
type VerificationWaiver struct {
	SchemaVersion     int       `json:"schema_version"`
	WaiverID          string    `json:"waiver_id"`
	IntentFingerprint string    `json:"intent_fingerprint"`
	RepositoryID      string    `json:"repository_id"`
	PolicyDigest      string    `json:"policy_digest"`
	RuleID            string    `json:"rule_id"`
	Phase             Phase     `json:"phase"`
	Generation        string    `json:"generation,omitempty"`
	CheckpointID      string    `json:"checkpoint_id,omitempty"`
	Authority         string    `json:"authority"`
	Actor             string    `json:"actor"`
	Reason            string    `json:"reason"`
	CreatedAt         time.Time `json:"created_at"`
	ExpiresAt         time.Time `json:"expires_at,omitempty"`
	ExpiresPhase      Phase     `json:"expires_phase,omitempty"`
}
type WaiverRevocation struct {
	SchemaVersion     int       `json:"schema_version"`
	WaiverID          string    `json:"waiver_id"`
	IntentFingerprint string    `json:"intent_fingerprint"`
	Authority         string    `json:"authority"`
	Actor             string    `json:"actor"`
	RevokedAt         time.Time `json:"revoked_at"`
}
type ActivationWriteResult struct {
	Record    PolicyActivation `json:"record"`
	Created   bool             `json:"created"`
	Replayed  bool             `json:"replayed"`
	Effective bool             `json:"effective"`
}
type WaiverWriteResult struct {
	Record   VerificationWaiver `json:"record"`
	Created  bool               `json:"created"`
	Replayed bool               `json:"replayed"`
	Active   bool               `json:"active"`
}
type RevocationWriteResult struct {
	Record   WaiverRevocation `json:"record"`
	Created  bool             `json:"created"`
	Replayed bool             `json:"replayed"`
}

func fingerprintIntent(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	s := sha256.Sum256(b)
	return "ifp_" + hex.EncodeToString(s[:]), nil
}
func ActivationIntentFingerprint(v PolicyActivationIntent) (string, error) {
	if err := v.Validate(); err != nil {
		return "", err
	}
	return fingerprintIntent(v)
}
func WaiverIntentFingerprint(v VerificationWaiverIntent) (string, error) {
	if err := v.Validate(); err != nil {
		return "", err
	}
	return fingerprintIntent(v)
}
func RevocationIntentFingerprint(v WaiverRevocationIntent) (string, error) {
	if err := v.Validate(); err != nil {
		return "", err
	}
	return fingerprintIntent(v)
}
func (v PolicyActivationIntent) Validate() error {
	if ValidateActivationID(v.ActivationID) != nil || !boundedToken(v.RepositoryID, 128) || (!isDerivedID(v.PreviousEffectiveDigest, "pol_") && v.PreviousEffectiveDigest != "absent") || !isDerivedID(v.ProposedPolicyDigest, "pol_") || !validGeneration(v.ProposalGeneration) || !boundedToken(v.Authority, 64) || !boundedToken(v.Actor, 128) {
		return fmt.Errorf("invalid activation intent")
	}
	return nil
}
func (v PolicyActivationCommit) Validate() error {
	if err := v.Intent.Validate(); err != nil {
		return err
	}
	switch v.ProposalOrigin {
	case ProposalRepositoryAuthored, ProposalStarterProfile, ProposalGenerated:
	default:
		return fmt.Errorf("invalid proposal origin")
	}
	if v.ProfileOrigin != "" && !boundedToken(v.ProfileOrigin, 256) {
		return fmt.Errorf("invalid profile origin")
	}
	if !validGeneration(v.ActivationGeneration) {
		return fmt.Errorf("invalid activation generation")
	}
	return nil
}
func (v VerificationWaiverIntent) Validate() error {
	if ValidateWaiverID(v.WaiverID) != nil || !boundedToken(v.RepositoryID, 128) || !isDerivedID(v.PolicyDigest, "pol_") || !boundedToken(v.RuleID, 128) || v.Phase.Validate() != nil || !boundedToken(v.Authority, 64) || !boundedToken(v.Actor, 128) || !boundedToken(v.Reason, 1024) {
		return fmt.Errorf("invalid waiver intent")
	}
	if v.Generation != "" && !validGeneration(v.Generation) {
		return fmt.Errorf("invalid waiver generation")
	}
	if v.CheckpointID != "" && !boundedToken(v.CheckpointID, 128) {
		return fmt.Errorf("invalid checkpoint id")
	}
	if v.ExpiresPhase != "" && v.ExpiresPhase.Validate() != nil {
		return fmt.Errorf("invalid expiry phase")
	}
	return nil
}
func (v WaiverRevocationIntent) Validate() error {
	if !boundedToken(v.RepositoryID, 128) || ValidateWaiverID(v.WaiverID) != nil || !boundedToken(v.Authority, 64) || !boundedToken(v.Actor, 128) {
		return fmt.Errorf("invalid waiver revocation intent")
	}
	return nil
}
