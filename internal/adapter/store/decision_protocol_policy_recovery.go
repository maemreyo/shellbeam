package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	decisionprotocol "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
)

func (r *Repository) rebuildDecisionPolicySecondaryForRecordLocked(env decisionprotocol.CanonicalRecordEnvelope) error {
	switch env.Kind {
	case decisionprotocol.RecordPolicySnapshot:
		var snapshot decisionprotocol.PolicySnapshot
		if err := json.Unmarshal(env.Body, &snapshot); err != nil {
			return err
		}
		if err := snapshot.Validate(); err != nil {
			return err
		}
		path := r.decisionProtocolPolicyPath(snapshot.RepositoryID, snapshot.PolicyDigest)
		if err := ensurePrivateParent(path); err != nil {
			return err
		}
		return r.replaceOrCreateDecisionSecondaryLocked(path, snapshot)
	case decisionprotocol.RecordPolicyActivation:
		var activation decisionprotocol.PolicyActivation
		if err := json.Unmarshal(env.Body, &activation); err != nil {
			return err
		}
		if err := activation.Validate(); err != nil {
			return err
		}
		snap, _, found, err := r.findCanonicalDecisionPolicySnapshotLocked(activation.RepositoryID, activation.PolicyDigest, env.CanonicalRecordSeq)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("activation canonical policy snapshot missing")
		}
		previous, err := r.previousDigestForActivationLocked(activation.RepositoryID, snap.Content.EpisodeKinds, env.CanonicalRecordSeq)
		if err != nil {
			return err
		}
		intent := decisionprotocol.PolicyActivationIntent{ActivationID: activation.ActivationID, RepositoryID: activation.RepositoryID, PreviousEffectivePolicyDigest: previous, ProposedPolicyDigest: activation.PolicyDigest, ProposalGeneration: activation.ProposalGeneration, Authority: activation.Authority, ActorRef: activation.ActorRef}
		fingerprint, err := decisionprotocol.PolicyActivationIntentFingerprint(intent)
		if err != nil {
			return err
		}
		materialized := decisionProtocolActivationMaterialization{SchemaVersion: 1, CanonicalRecordSeq: env.CanonicalRecordSeq, IntentFingerprint: fingerprint, PreviousEffectivePolicyDigest: previous, Record: activation}
		path := r.decisionProtocolActivationPath(activation.RepositoryID, activation.ActivationID)
		if err := ensurePrivateParent(path); err != nil {
			return err
		}
		if err := r.replaceOrCreateDecisionSecondaryLocked(path, materialized); err != nil {
			return err
		}
		for _, kind := range snap.Content.EpisodeKinds {
			idx := decisionProtocolEffectiveIndex{SchemaVersion: 1, ActivationID: activation.ActivationID, PolicyDigest: activation.PolicyDigest, CanonicalRecordSeq: env.CanonicalRecordSeq}
			epath := r.decisionProtocolEffectivePath(activation.RepositoryID, kind)
			if err := ensurePrivateParent(epath); err != nil {
				return err
			}
			if err := r.replaceOrCreateDecisionSecondaryLocked(epath, idx); err != nil {
				return err
			}
		}
	}
	return nil
}

func ensurePrivateParent(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.Chmod(dir, 0o700)
}

func (r *Repository) replaceOrCreateDecisionSecondaryLocked(path string, value any) error {
	var existing any
	err := readStrict(path, &existing)
	if err == nil {
		encoded, marshalErr := json.Marshal(value)
		if marshalErr != nil {
			return marshalErr
		}
		var expected any
		if unmarshalErr := json.Unmarshal(encoded, &expected); unmarshalErr != nil {
			return unmarshalErr
		}
		if reflect.DeepEqual(existing, expected) {
			return nil
		}
		if res := r.writer.Replace(path, value); res.Err != nil {
			return res.Err
		}
		return nil
	}
	if !errors.Is(err, ErrNotFound) {
		// Secondary state never outranks canonical truth. A malformed or
		// disagreeing secondary is deterministically rebuilt from the canonical
		// record rather than interpreted as authority.
		if res := r.writer.Replace(path, value); res.Err != nil {
			return res.Err
		}
		return nil
	}
	if res := r.writer.Create(path, value); res.Err != nil {
		return res.Err
	}
	return nil
}

func (r *Repository) findCanonicalDecisionPolicySnapshotLocked(repo, digest string, cut decisionprotocol.RecordSeq) (decisionprotocol.PolicySnapshot, decisionprotocol.RecordSeq, bool, error) {
	var found decisionprotocol.PolicySnapshot
	var foundSeq decisionprotocol.RecordSeq
	count := 0
	for seq := decisionprotocol.RecordSeq(1); seq <= cut; seq++ {
		env, ok, err := r.loadDecisionProtocolRecordLocked(seq)
		if err != nil {
			return found, 0, false, err
		}
		if !ok {
			return found, 0, false, fmt.Errorf("decision protocol ledger gap at %d", seq)
		}
		if env.Kind != decisionprotocol.RecordPolicySnapshot {
			continue
		}
		var s decisionprotocol.PolicySnapshot
		if err := json.Unmarshal(env.Body, &s); err != nil {
			return found, 0, false, err
		}
		if s.RepositoryID == repo && s.PolicyDigest == digest {
			found = s
			foundSeq = seq
			count++
		}
	}
	if count > 1 {
		return found, 0, false, fmt.Errorf("duplicate canonical decision policy snapshot identity")
	}
	return found, foundSeq, count == 1, nil
}

func (r *Repository) findCanonicalDecisionPolicyActivationLocked(repo, id string, cut decisionprotocol.RecordSeq) (decisionprotocol.PolicyActivation, decisionprotocol.RecordSeq, bool, error) {
	var found decisionprotocol.PolicyActivation
	var foundSeq decisionprotocol.RecordSeq
	count := 0
	for seq := decisionprotocol.RecordSeq(1); seq <= cut; seq++ {
		env, ok, err := r.loadDecisionProtocolRecordLocked(seq)
		if err != nil {
			return found, 0, false, err
		}
		if !ok {
			return found, 0, false, fmt.Errorf("decision protocol ledger gap at %d", seq)
		}
		if env.Kind != decisionprotocol.RecordPolicyActivation {
			continue
		}
		var a decisionprotocol.PolicyActivation
		if err := json.Unmarshal(env.Body, &a); err != nil {
			return found, 0, false, err
		}
		if a.RepositoryID == repo && a.ActivationID == id {
			found = a
			foundSeq = seq
			count++
		}
	}
	if count > 1 {
		return found, 0, false, fmt.Errorf("duplicate canonical decision policy activation identity")
	}
	return found, foundSeq, count == 1, nil
}

func episodeKindIncluded(kinds []decisionprotocol.EpisodeKind, want decisionprotocol.EpisodeKind) bool {
	for _, kind := range kinds {
		if kind == want {
			return true
		}
	}
	return false
}

func (r *Repository) previousDigestForActivationLocked(repo string, kinds []decisionprotocol.EpisodeKind, activationSeq decisionprotocol.RecordSeq) (string, error) {
	if activationSeq <= 1 {
		return "absent", nil
	}
	return r.commonEffectiveDigestAtCutLocked(repo, kinds, activationSeq-1)
}
func (r *Repository) currentCommonEffectiveDigestLocked(repo string, kinds []decisionprotocol.EpisodeKind) (string, error) {
	hw, err := r.readDecisionProtocolHighWaterLocked()
	if err != nil {
		return "", err
	}
	return r.commonEffectiveDigestAtCutLocked(repo, kinds, hw)
}
func (r *Repository) commonEffectiveDigestAtCutLocked(repo string, kinds []decisionprotocol.EpisodeKind, cut decisionprotocol.RecordSeq) (string, error) {
	if len(kinds) == 0 {
		return "", fmt.Errorf("policy has no episode kinds")
	}
	var common string
	for i, kind := range kinds {
		digest, err := r.effectiveDigestForKindAtCutLocked(repo, kind, cut)
		if err != nil {
			return "", err
		}
		if i == 0 {
			common = digest
		} else if digest != common {
			return "", fmt.Errorf("decision policy activation previous effective digests differ across episode kinds")
		}
	}
	return common, nil
}
func (r *Repository) effectiveDigestForKindAtCutLocked(repo string, kind decisionprotocol.EpisodeKind, cut decisionprotocol.RecordSeq) (string, error) {
	current := "absent"
	for seq := decisionprotocol.RecordSeq(1); seq <= cut; seq++ {
		env, ok, err := r.loadDecisionProtocolRecordLocked(seq)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", fmt.Errorf("decision protocol ledger gap at %d", seq)
		}
		if env.Kind != decisionprotocol.RecordPolicyActivation {
			continue
		}
		var a decisionprotocol.PolicyActivation
		if json.Unmarshal(env.Body, &a) != nil || a.RepositoryID != repo {
			continue
		}
		snap, _, found, err := r.findCanonicalDecisionPolicySnapshotLocked(repo, a.PolicyDigest, seq)
		if err != nil {
			return "", err
		}
		if !found {
			return "", fmt.Errorf("activation policy snapshot unavailable")
		}
		if episodeKindIncluded(snap.Content.EpisodeKinds, kind) {
			current = a.PolicyDigest
		}
	}
	return current, nil
}

func (r *Repository) activationCurrentlyEffectiveLocked(a decisionprotocol.PolicyActivation, kinds []decisionprotocol.EpisodeKind) (bool, error) {
	for _, kind := range kinds {
		idx, ok, err := r.readDecisionProtocolEffectiveIndexLocked(a.RepositoryID, kind)
		if err != nil {
			return false, err
		}
		if !ok || idx.ActivationID != a.ActivationID || idx.PolicyDigest != a.PolicyDigest {
			return false, nil
		}
	}
	return true, nil
}
func (r *Repository) readDecisionProtocolEffectiveIndexLocked(repo string, kind decisionprotocol.EpisodeKind) (decisionProtocolEffectiveIndex, bool, error) {
	var idx decisionProtocolEffectiveIndex
	err := readStrict(r.decisionProtocolEffectivePath(repo, kind), &idx)
	if errors.Is(err, ErrNotFound) {
		return idx, false, nil
	}
	if err != nil {
		return idx, false, err
	}
	if idx.SchemaVersion != 1 || !boundedStoreToken(idx.ActivationID) || !strings.HasPrefix(idx.PolicyDigest, "pol_") || idx.CanonicalRecordSeq == 0 {
		return idx, false, fmt.Errorf("corrupt decision protocol effective index")
	}
	return idx, true, nil
}
func boundedStoreToken(s string) bool { return s != "" && len(s) <= 256 }

func (r *Repository) validateDecisionProtocolSecondaryAuthorityLocked(hw decisionprotocol.RecordSeq) error {
	canonicalPolicies := map[string]struct{}{}
	canonicalActivations := map[string]struct{}{}
	for seq := decisionprotocol.RecordSeq(1); seq <= hw; seq++ {
		env, ok, err := r.loadDecisionProtocolRecordLocked(seq)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("decision protocol ledger gap at %d", seq)
		}
		switch env.Kind {
		case decisionprotocol.RecordPolicySnapshot:
			var s decisionprotocol.PolicySnapshot
			if json.Unmarshal(env.Body, &s) != nil {
				return fmt.Errorf("corrupt canonical policy snapshot")
			}
			canonicalPolicies[s.RepositoryID+"\x00"+s.PolicyDigest] = struct{}{}
		case decisionprotocol.RecordPolicyActivation:
			var a decisionprotocol.PolicyActivation
			if json.Unmarshal(env.Body, &a) != nil {
				return fmt.Errorf("corrupt canonical policy activation")
			}
			canonicalActivations[a.RepositoryID+"\x00"+a.ActivationID] = struct{}{}
		}
	}
	if err := rejectOrphanDecisionSecondaries(r.decisionProtocolPolicyRoot(), canonicalPolicies); err != nil {
		return err
	}
	if err := rejectOrphanDecisionSecondaries(r.decisionProtocolActivationRoot(), canonicalActivations); err != nil {
		return err
	}
	return nil
}

func rejectOrphanDecisionSecondaries(root string, allowed map[string]struct{}) error {
	repos, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, repoEntry := range repos {
		if !repoEntry.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(root, repoEntry.Name()))
		if err != nil {
			return err
		}
		for _, file := range files {
			if file.IsDir() || !strings.HasSuffix(file.Name(), ".json") {
				continue
			}
			id := strings.TrimSuffix(file.Name(), ".json")
			if _, ok := allowed[repoEntry.Name()+"\x00"+id]; !ok {
				return fmt.Errorf("decision protocol secondary %s/%s has no canonical authority", repoEntry.Name(), id)
			}
		}
	}
	return nil
}
