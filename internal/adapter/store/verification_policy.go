package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"

	core "github.com/maemreyo/shellbeam/internal/core/verification"
	"github.com/maemreyo/shellbeam/internal/core/workspace"
)

type verificationEffectiveIndex struct {
	SchemaVersion int    `json:"schema_version"`
	ActivationID  string `json:"activation_id"`
	PolicyDigest  string `json:"policy_digest"`
}

func (r *Repository) initVerificationStore() error {
	for _, d := range []string{"policies", "activations", "effective", "waivers", "waiver_revocations"} {
		p := filepath.Join(r.root, "verification", d)
		if err := os.MkdirAll(p, 0o700); err != nil {
			return err
		}
		if err := os.Chmod(p, 0o700); err != nil {
			return err
		}
	}
	return nil
}
func (r *Repository) verificationRepoDir(kind string, repo workspace.RepositoryID) string {
	return filepath.Join(r.root, "verification", kind, string(repo))
}
func (r *Repository) ensureVerificationRepoDir(kind string, repo workspace.RepositoryID) (string, error) {
	d := r.verificationRepoDir(kind, repo)
	if err := os.MkdirAll(d, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(d, 0o700); err != nil {
		return "", err
	}
	return d, nil
}
func policyPath(r *Repository, repo workspace.RepositoryID, digest string) string {
	return filepath.Join(r.verificationRepoDir("policies", repo), digest+".json")
}
func activationPath(r *Repository, repo workspace.RepositoryID, id string) string {
	return filepath.Join(r.verificationRepoDir("activations", repo), id+".json")
}
func waiverPath(r *Repository, repo workspace.RepositoryID, id string) string {
	return filepath.Join(r.verificationRepoDir("waivers", repo), id+".json")
}
func revocationPath(r *Repository, repo workspace.RepositoryID, id string) string {
	return filepath.Join(r.verificationRepoDir("waiver_revocations", repo), id+".json")
}
func effectivePath(r *Repository, repo workspace.RepositoryID) string {
	return filepath.Join(r.root, "verification", "effective", string(repo)+".json")
}

func (r *Repository) PutPolicySnapshot(_ context.Context, s core.PolicySnapshot) (bool, error) {
	r.verificationMu.Lock()
	defer r.verificationMu.Unlock()
	repo := workspace.RepositoryID(s.RepositoryID)
	if _, err := workspace.ParseRepositoryID(s.RepositoryID); err != nil {
		return false, err
	}
	digest, err := core.PolicyDigest(s.Content)
	if err != nil || digest != s.Digest {
		return false, fmt.Errorf("policy snapshot digest mismatch")
	}
	if _, err := r.ensureVerificationRepoDir("policies", repo); err != nil {
		return false, err
	}
	path := policyPath(r, repo, s.Digest)
	var existing core.PolicySnapshot
	if err := readStrict(path, &existing); err == nil {
		existingDigest, e := core.PolicyDigest(existing.Content)
		if e != nil || existingDigest != existing.Digest || !reflect.DeepEqual(existing, s) {
			return false, fmt.Errorf("conflicting immutable policy snapshot")
		}
		return false, nil
	} else if !errors.Is(err, ErrNotFound) {
		return false, err
	}
	res := r.writer.Create(path, s)
	if res.Err != nil {
		return false, res.Err
	}
	return true, nil
}
func (r *Repository) LoadPolicySnapshot(_ context.Context, repo workspace.RepositoryID, digest string) (core.PolicySnapshot, bool, error) {
	r.verificationMu.Lock()
	defer r.verificationMu.Unlock()
	var s core.PolicySnapshot
	err := readStrict(policyPath(r, repo, digest), &s)
	if errors.Is(err, ErrNotFound) {
		return s, false, nil
	}
	if err != nil {
		return s, false, err
	}
	computed, e := core.PolicyDigest(s.Content)
	if e != nil || computed != s.Digest || s.RepositoryID != string(repo) {
		return s, false, fmt.Errorf("corrupt policy snapshot")
	}
	return s, true, nil
}
func (r *Repository) FindActivation(_ context.Context, repo workspace.RepositoryID, id string) (core.PolicyActivation, bool, error) {
	r.verificationMu.Lock()
	defer r.verificationMu.Unlock()
	return r.findActivationUnlocked(repo, id)
}
func (r *Repository) findActivationUnlocked(repo workspace.RepositoryID, id string) (core.PolicyActivation, bool, error) {
	var a core.PolicyActivation
	err := readStrict(activationPath(r, repo, id), &a)
	if errors.Is(err, ErrNotFound) {
		return a, false, nil
	}
	if err != nil {
		return a, false, err
	}
	if a.SchemaVersion != 1 || a.ActivationID != id || a.RepositoryID != string(repo) || a.IntentFingerprint == "" || a.ActivatedAt.IsZero() {
		return a, false, fmt.Errorf("corrupt policy activation")
	}
	return a, true, nil
}
func (r *Repository) readEffectiveUnlocked(repo workspace.RepositoryID) (verificationEffectiveIndex, bool, error) {
	var idx verificationEffectiveIndex
	err := readStrict(effectivePath(r, repo), &idx)
	if errors.Is(err, ErrNotFound) {
		return idx, false, nil
	}
	if err != nil {
		return idx, false, err
	}
	if idx.SchemaVersion != 1 || idx.ActivationID == "" || idx.PolicyDigest == "" {
		return idx, false, fmt.Errorf("corrupt verification effective index")
	}
	return idx, true, nil
}
func currentDigest(idx verificationEffectiveIndex, ok bool) string {
	if !ok {
		return "absent"
	}
	return idx.PolicyDigest
}
func (r *Repository) ActivatePolicyCAS(_ context.Context, c core.PolicyActivationCommit) (core.ActivationWriteResult, error) {
	r.verificationMu.Lock()
	defer r.verificationMu.Unlock()
	if err := c.Validate(); err != nil {
		return core.ActivationWriteResult{}, err
	}
	repo := workspace.RepositoryID(c.Intent.RepositoryID)
	fp, err := core.ActivationIntentFingerprint(c.Intent)
	if err != nil {
		return core.ActivationWriteResult{}, err
	}
	existing, found, err := r.findActivationUnlocked(repo, c.Intent.ActivationID)
	if err != nil {
		return core.ActivationWriteResult{}, err
	}
	idx, idxOK, err := r.readEffectiveUnlocked(repo)
	if err != nil {
		return core.ActivationWriteResult{}, err
	}
	if found {
		if existing.IntentFingerprint != fp {
			return core.ActivationWriteResult{}, fmt.Errorf("activation id conflicts with different intent")
		}
		if idxOK && idx.ActivationID == existing.ActivationID && idx.PolicyDigest == existing.ProposedPolicyDigest {
			return core.ActivationWriteResult{Record: existing, Replayed: true, Effective: true}, nil
		}
		if currentDigest(idx, idxOK) == existing.PreviousEffectiveDigest {
			next := verificationEffectiveIndex{SchemaVersion: 1, ActivationID: existing.ActivationID, PolicyDigest: existing.ProposedPolicyDigest}
			if res := r.writer.Replace(effectivePath(r, repo), next); res.Err != nil {
				return core.ActivationWriteResult{}, res.Err
			}
			return core.ActivationWriteResult{Record: existing, Replayed: true, Effective: true}, nil
		}
		return core.ActivationWriteResult{Record: existing, Replayed: true, Effective: false}, nil
	}
	if currentDigest(idx, idxOK) != c.Intent.PreviousEffectiveDigest {
		return core.ActivationWriteResult{}, fmt.Errorf("effective policy compare-and-swap mismatch")
	}
	var snap core.PolicySnapshot
	if err := readStrict(policyPath(r, repo, c.Intent.ProposedPolicyDigest), &snap); err != nil {
		return core.ActivationWriteResult{}, fmt.Errorf("activation policy snapshot unavailable: %w", err)
	}
	record := core.PolicyActivation{SchemaVersion: 1, ActivationID: c.Intent.ActivationID, IntentFingerprint: fp, RepositoryID: c.Intent.RepositoryID, PreviousEffectiveDigest: c.Intent.PreviousEffectiveDigest, ProposedPolicyDigest: c.Intent.ProposedPolicyDigest, ProposalOrigin: c.ProposalOrigin, ProfileOrigin: c.ProfileOrigin, ProposalGeneration: c.Intent.ProposalGeneration, ActivationGeneration: c.ActivationGeneration, Authority: c.Intent.Authority, Actor: c.Intent.Actor, ActivatedAt: r.now()}
	if _, err := r.ensureVerificationRepoDir("activations", repo); err != nil {
		return core.ActivationWriteResult{}, err
	}
	if res := r.writer.Create(activationPath(r, repo, record.ActivationID), record); res.Err != nil {
		return core.ActivationWriteResult{}, res.Err
	}
	next := verificationEffectiveIndex{SchemaVersion: 1, ActivationID: record.ActivationID, PolicyDigest: record.ProposedPolicyDigest}
	if res := r.writer.Replace(effectivePath(r, repo), next); res.Err != nil {
		return core.ActivationWriteResult{}, res.Err
	}
	return core.ActivationWriteResult{Record: record, Created: true, Effective: true}, nil
}
func (r *Repository) CurrentActivation(_ context.Context, repo workspace.RepositoryID) (core.PolicyActivation, bool, error) {
	r.verificationMu.Lock()
	defer r.verificationMu.Unlock()
	idx, ok, err := r.readEffectiveUnlocked(repo)
	if err != nil || !ok {
		return core.PolicyActivation{}, false, err
	}
	a, found, err := r.findActivationUnlocked(repo, idx.ActivationID)
	if err != nil || !found {
		return core.PolicyActivation{}, false, fmt.Errorf("effective index target missing or corrupt")
	}
	if a.ProposedPolicyDigest != idx.PolicyDigest {
		return core.PolicyActivation{}, false, fmt.Errorf("effective index digest mismatch")
	}
	return a, true, nil
}
func (r *Repository) FindWaiver(_ context.Context, repo workspace.RepositoryID, id string) (core.VerificationWaiver, bool, error) {
	r.verificationMu.Lock()
	defer r.verificationMu.Unlock()
	return r.findWaiverUnlocked(repo, id)
}
func (r *Repository) findWaiverUnlocked(repo workspace.RepositoryID, id string) (core.VerificationWaiver, bool, error) {
	var w core.VerificationWaiver
	err := readStrict(waiverPath(r, repo, id), &w)
	if errors.Is(err, ErrNotFound) {
		return w, false, nil
	}
	if err != nil {
		return w, false, err
	}
	if w.SchemaVersion != 1 || w.WaiverID != id || w.RepositoryID != string(repo) || w.IntentFingerprint == "" || w.CreatedAt.IsZero() {
		return w, false, fmt.Errorf("corrupt waiver")
	}
	return w, true, nil
}
func (r *Repository) FindWaiverRevocation(_ context.Context, repo workspace.RepositoryID, id string) (core.WaiverRevocation, bool, error) {
	r.verificationMu.Lock()
	defer r.verificationMu.Unlock()
	return r.findRevocationUnlocked(repo, id)
}
func (r *Repository) findRevocationUnlocked(repo workspace.RepositoryID, id string) (core.WaiverRevocation, bool, error) {
	var w core.WaiverRevocation
	err := readStrict(revocationPath(r, repo, id), &w)
	if errors.Is(err, ErrNotFound) {
		return w, false, nil
	}
	if err != nil {
		return w, false, err
	}
	if w.SchemaVersion != 1 || w.WaiverID != id || w.IntentFingerprint == "" || w.RevokedAt.IsZero() {
		return w, false, fmt.Errorf("corrupt revocation")
	}
	return w, true, nil
}
func (r *Repository) PutWaiver(_ context.Context, in core.VerificationWaiverIntent) (core.WaiverWriteResult, error) {
	r.verificationMu.Lock()
	defer r.verificationMu.Unlock()
	if err := in.Validate(); err != nil {
		return core.WaiverWriteResult{}, err
	}
	repo := workspace.RepositoryID(in.RepositoryID)
	fp, err := core.WaiverIntentFingerprint(in)
	if err != nil {
		return core.WaiverWriteResult{}, err
	}
	existing, found, err := r.findWaiverUnlocked(repo, in.WaiverID)
	if err != nil {
		return core.WaiverWriteResult{}, err
	}
	if found {
		if existing.IntentFingerprint != fp {
			return core.WaiverWriteResult{}, fmt.Errorf("waiver id conflicts with different intent")
		}
		_, revoked, err := r.findRevocationUnlocked(repo, in.WaiverID)
		if err != nil {
			return core.WaiverWriteResult{}, err
		}
		return core.WaiverWriteResult{Record: existing, Replayed: true, Active: !revoked}, nil
	}
	if _, err := r.ensureVerificationRepoDir("waivers", repo); err != nil {
		return core.WaiverWriteResult{}, err
	}
	record := core.VerificationWaiver{SchemaVersion: 1, WaiverID: in.WaiverID, IntentFingerprint: fp, RepositoryID: in.RepositoryID, PolicyDigest: in.PolicyDigest, RuleID: in.RuleID, Phase: in.Phase, Generation: in.Generation, CheckpointID: in.CheckpointID, Authority: in.Authority, Actor: in.Actor, Reason: in.Reason, CreatedAt: r.now(), ExpiresAt: in.ExpiresAt, ExpiresPhase: in.ExpiresPhase}
	if res := r.writer.Create(waiverPath(r, repo, in.WaiverID), record); res.Err != nil {
		return core.WaiverWriteResult{}, res.Err
	}
	return core.WaiverWriteResult{Record: record, Created: true, Active: true}, nil
}
func (r *Repository) PutWaiverRevocation(_ context.Context, in core.WaiverRevocationIntent) (core.RevocationWriteResult, error) {
	r.verificationMu.Lock()
	defer r.verificationMu.Unlock()
	if err := in.Validate(); err != nil {
		return core.RevocationWriteResult{}, err
	}
	repo, err := workspace.ParseRepositoryID(in.RepositoryID)
	if err != nil {
		return core.RevocationWriteResult{}, err
	}
	if _, found, err := r.findWaiverUnlocked(repo, in.WaiverID); err != nil {
		return core.RevocationWriteResult{}, err
	} else if !found {
		return core.RevocationWriteResult{}, fmt.Errorf("waiver not found for revocation")
	}
	fp, err := core.RevocationIntentFingerprint(in)
	if err != nil {
		return core.RevocationWriteResult{}, err
	}
	existing, found, err := r.findRevocationUnlocked(repo, in.WaiverID)
	if err != nil {
		return core.RevocationWriteResult{}, err
	}
	if found {
		if existing.IntentFingerprint != fp {
			return core.RevocationWriteResult{}, fmt.Errorf("revocation conflicts")
		}
		return core.RevocationWriteResult{Record: existing, Replayed: true}, nil
	}
	if _, err := r.ensureVerificationRepoDir("waiver_revocations", repo); err != nil {
		return core.RevocationWriteResult{}, err
	}
	record := core.WaiverRevocation{SchemaVersion: 1, WaiverID: in.WaiverID, IntentFingerprint: fp, Authority: in.Authority, Actor: in.Actor, RevokedAt: r.now()}
	if res := r.writer.Create(revocationPath(r, repo, in.WaiverID), record); res.Err != nil {
		return core.RevocationWriteResult{}, res.Err
	}
	return core.RevocationWriteResult{Record: record, Created: true}, nil
}

func (r *Repository) ListWaivers(_ context.Context, repo workspace.RepositoryID) ([]core.VerificationWaiver, []core.WaiverRevocation, error) {
	r.verificationMu.Lock()
	defer r.verificationMu.Unlock()
	waivers := []core.VerificationWaiver{}
	revs := []core.WaiverRevocation{}
	for _, spec := range []struct {
		kind    string
		waivers bool
	}{{"waivers", true}, {"waiver_revocations", false}} {
		dir := r.verificationRepoDir(spec.kind, repo)
		entries, err := os.ReadDir(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
				continue
			}
			if spec.waivers {
				var v core.VerificationWaiver
				if err := readStrict(filepath.Join(dir, e.Name()), &v); err != nil {
					return nil, nil, err
				}
				waivers = append(waivers, v)
			} else {
				var v core.WaiverRevocation
				if err := readStrict(filepath.Join(dir, e.Name()), &v); err != nil {
					return nil, nil, err
				}
				revs = append(revs, v)
			}
		}
	}
	sort.Slice(waivers, func(i, j int) bool { return waivers[i].WaiverID < waivers[j].WaiverID })
	sort.Slice(revs, func(i, j int) bool { return revs[i].WaiverID < revs[j].WaiverID })
	return waivers, revs, nil
}
