package store

import (
	"context"
	"fmt"
	"reflect"

	decisionprotocol "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
)

type decisionProtocolEffectiveIndex struct {
	SchemaVersion      int                        `json:"schema_version"`
	ActivationID       string                     `json:"activation_id"`
	PolicyDigest       string                     `json:"policy_digest"`
	CanonicalRecordSeq decisionprotocol.RecordSeq `json:"canonical_record_seq"`
}

type decisionProtocolActivationMaterialization struct {
	SchemaVersion                 int                               `json:"schema_version"`
	CanonicalRecordSeq            decisionprotocol.RecordSeq        `json:"canonical_record_seq"`
	IntentFingerprint             string                            `json:"intent_fingerprint"`
	PreviousEffectivePolicyDigest string                            `json:"previous_effective_policy_digest"`
	Record                        decisionprotocol.PolicyActivation `json:"record"`
}

func (s *DecisionProtocolStore) PutPolicySnapshot(_ context.Context, snapshot decisionprotocol.PolicySnapshot) (bool, error) {
	if s == nil || s.repository == nil {
		return false, fmt.Errorf("decision protocol store unavailable")
	}
	r := s.repository
	r.decisionProtocolMu.Lock()
	defer r.decisionProtocolMu.Unlock()
	if err := r.recoverDecisionProtocolLocked(); err != nil {
		return false, err
	}
	if err := snapshot.Validate(); err != nil {
		return false, err
	}
	hw, err := r.readDecisionProtocolHighWaterLocked()
	if err != nil {
		return false, err
	}
	existing, seq, found, err := r.findCanonicalDecisionPolicySnapshotLocked(snapshot.RepositoryID, snapshot.PolicyDigest, hw)
	if err != nil {
		return false, err
	}
	if found {
		if !reflect.DeepEqual(existing, snapshot) {
			return false, fmt.Errorf("conflicting immutable decision policy snapshot")
		}
		env, ok, err := r.loadDecisionProtocolRecordLocked(seq)
		if err != nil || !ok {
			return false, fmt.Errorf("canonical policy snapshot missing during replay: %w", err)
		}
		if err := r.rebuildDecisionPolicySecondaryForRecordLocked(env); err != nil {
			return false, err
		}
		return false, nil
	}
	_, err = r.appendCanonicalRecordLocked(decisionprotocol.RecordPolicySnapshot, snapshot)
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *DecisionProtocolStore) LoadPolicySnapshot(_ context.Context, repositoryID, digest string) (decisionprotocol.PolicySnapshot, bool, error) {
	if s == nil || s.repository == nil {
		return decisionprotocol.PolicySnapshot{}, false, fmt.Errorf("decision protocol store unavailable")
	}
	r := s.repository
	r.decisionProtocolMu.Lock()
	defer r.decisionProtocolMu.Unlock()
	if err := r.recoverDecisionProtocolLocked(); err != nil {
		return decisionprotocol.PolicySnapshot{}, false, err
	}
	hw, err := r.readDecisionProtocolHighWaterLocked()
	if err != nil {
		return decisionprotocol.PolicySnapshot{}, false, err
	}
	snap, _, ok, err := r.findCanonicalDecisionPolicySnapshotLocked(repositoryID, digest, hw)
	return snap, ok, err
}

func (s *DecisionProtocolStore) ActivatePolicyCAS(_ context.Context, commit decisionprotocol.PolicyActivationCommit) (decisionprotocol.PolicyActivationWriteResult, error) {
	if s == nil || s.repository == nil {
		return decisionprotocol.PolicyActivationWriteResult{}, fmt.Errorf("decision protocol store unavailable")
	}
	r := s.repository
	r.decisionProtocolMu.Lock()
	defer r.decisionProtocolMu.Unlock()
	if err := r.recoverDecisionProtocolLocked(); err != nil {
		return decisionprotocol.PolicyActivationWriteResult{}, err
	}
	if err := commit.Validate(); err != nil {
		return decisionprotocol.PolicyActivationWriteResult{}, err
	}
	hw, err := r.readDecisionProtocolHighWaterLocked()
	if err != nil {
		return decisionprotocol.PolicyActivationWriteResult{}, err
	}
	snap, _, found, err := r.findCanonicalDecisionPolicySnapshotLocked(commit.Intent.RepositoryID, commit.Intent.ProposedPolicyDigest, hw)
	if err != nil {
		return decisionprotocol.PolicyActivationWriteResult{}, err
	}
	if !found {
		return decisionprotocol.PolicyActivationWriteResult{}, fmt.Errorf("decision policy snapshot unavailable")
	}

	existing, seq, found, err := r.findCanonicalDecisionPolicyActivationLocked(commit.Intent.RepositoryID, commit.Intent.ActivationID, hw)
	if err != nil {
		return decisionprotocol.PolicyActivationWriteResult{}, err
	}
	if found {
		previous, err := r.previousDigestForActivationLocked(commit.Intent.RepositoryID, snap.Content.EpisodeKinds, seq)
		if err != nil {
			return decisionprotocol.PolicyActivationWriteResult{}, err
		}
		if existing.PolicyDigest != commit.Intent.ProposedPolicyDigest || existing.ProposalGeneration != commit.Intent.ProposalGeneration || existing.ActivationGeneration != commit.ActivationGeneration || existing.Authority != commit.Intent.Authority || existing.ActorRef != commit.Intent.ActorRef || previous != commit.Intent.PreviousEffectivePolicyDigest {
			return decisionprotocol.PolicyActivationWriteResult{}, fmt.Errorf("decision policy activation id conflicts with different intent")
		}
		effective, err := r.activationCurrentlyEffectiveLocked(existing, snap.Content.EpisodeKinds)
		if err != nil {
			return decisionprotocol.PolicyActivationWriteResult{}, err
		}
		return decisionprotocol.PolicyActivationWriteResult{Record: existing, Replayed: true, Effective: effective}, nil
	}

	current, err := r.currentCommonEffectiveDigestLocked(commit.Intent.RepositoryID, snap.Content.EpisodeKinds)
	if err != nil {
		return decisionprotocol.PolicyActivationWriteResult{}, err
	}
	if current != commit.Intent.PreviousEffectivePolicyDigest {
		return decisionprotocol.PolicyActivationWriteResult{}, fmt.Errorf("decision effective policy compare-and-swap mismatch")
	}
	record := decisionprotocol.PolicyActivation{ActivationID: commit.Intent.ActivationID, RepositoryID: commit.Intent.RepositoryID, PolicyDigest: commit.Intent.ProposedPolicyDigest, ProposalGeneration: commit.Intent.ProposalGeneration, ActivationGeneration: commit.ActivationGeneration, Authority: commit.Intent.Authority, ActorRef: commit.Intent.ActorRef, ActivatedAt: r.now()}
	if err := record.Validate(); err != nil {
		return decisionprotocol.PolicyActivationWriteResult{}, err
	}
	if _, err := r.appendCanonicalRecordLocked(decisionprotocol.RecordPolicyActivation, record); err != nil {
		return decisionprotocol.PolicyActivationWriteResult{}, err
	}
	return decisionprotocol.PolicyActivationWriteResult{Record: record, Created: true, Effective: true}, nil
}

func (s *DecisionProtocolStore) CurrentEffectivePolicy(_ context.Context, repositoryID string, kind decisionprotocol.EpisodeKind) (decisionprotocol.PolicySnapshot, decisionprotocol.PolicyActivation, bool, error) {
	if s == nil || s.repository == nil {
		return decisionprotocol.PolicySnapshot{}, decisionprotocol.PolicyActivation{}, false, fmt.Errorf("decision protocol store unavailable")
	}
	if kind.Validate() != nil {
		return decisionprotocol.PolicySnapshot{}, decisionprotocol.PolicyActivation{}, false, fmt.Errorf("invalid episode kind")
	}
	r := s.repository
	r.decisionProtocolMu.Lock()
	defer r.decisionProtocolMu.Unlock()
	if err := r.recoverDecisionProtocolLocked(); err != nil {
		return decisionprotocol.PolicySnapshot{}, decisionprotocol.PolicyActivation{}, false, err
	}
	idx, ok, err := r.readDecisionProtocolEffectiveIndexLocked(repositoryID, kind)
	if err != nil || !ok {
		return decisionprotocol.PolicySnapshot{}, decisionprotocol.PolicyActivation{}, false, err
	}
	hw, err := r.readDecisionProtocolHighWaterLocked()
	if err != nil {
		return decisionprotocol.PolicySnapshot{}, decisionprotocol.PolicyActivation{}, false, err
	}
	activation, _, found, err := r.findCanonicalDecisionPolicyActivationLocked(repositoryID, idx.ActivationID, hw)
	if err != nil || !found {
		return decisionprotocol.PolicySnapshot{}, decisionprotocol.PolicyActivation{}, false, fmt.Errorf("decision effective activation target missing: %w", err)
	}
	snap, _, found, err := r.findCanonicalDecisionPolicySnapshotLocked(repositoryID, idx.PolicyDigest, hw)
	if err != nil || !found {
		return decisionprotocol.PolicySnapshot{}, decisionprotocol.PolicyActivation{}, false, fmt.Errorf("decision effective policy target missing: %w", err)
	}
	if activation.PolicyDigest != snap.PolicyDigest {
		return decisionprotocol.PolicySnapshot{}, decisionprotocol.PolicyActivation{}, false, fmt.Errorf("decision effective policy digest mismatch")
	}
	if !episodeKindIncluded(snap.Content.EpisodeKinds, kind) {
		return decisionprotocol.PolicySnapshot{}, decisionprotocol.PolicyActivation{}, false, nil
	}
	return snap, activation, true, nil
}
