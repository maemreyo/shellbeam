package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	decisionprotocol "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
)

type decisionProtocolHighWater struct {
	SchemaVersion            int                        `json:"schema_version"`
	CanonicalRecordHighWater decisionprotocol.RecordSeq `json:"canonical_record_high_water"`
}

type decisionProtocolEpisodeIndex struct {
	SchemaVersion      int                         `json:"schema_version"`
	CanonicalRecordSeq decisionprotocol.RecordSeq  `json:"canonical_record_seq"`
	Kind               decisionprotocol.RecordKind `json:"kind"`
}

func (r *Repository) initDecisionProtocolStore() error {
	for _, path := range []string{
		r.decisionProtocolRoot(), r.decisionProtocolLedgerDir(), r.decisionProtocolRecordDir(),
		r.decisionProtocolPolicyRoot(), r.decisionProtocolActivationRoot(), r.decisionProtocolEffectiveRoot(),
		r.decisionProtocolEpisodeIndexRoot(),
	} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return err
		}
		if err := os.Chmod(path, 0o700); err != nil {
			return err
		}
	}
	r.decisionProtocolMu.Lock()
	defer r.decisionProtocolMu.Unlock()
	return r.recoverDecisionProtocolLocked()
}

func (r *Repository) AppendRecord(_ context.Context, kind decisionprotocol.RecordKind, body any) (decisionprotocol.CanonicalRecordEnvelope, error) {
	r.decisionProtocolMu.Lock()
	defer r.decisionProtocolMu.Unlock()
	if err := r.recoverDecisionProtocolLocked(); err != nil {
		return decisionprotocol.CanonicalRecordEnvelope{}, err
	}
	return r.appendCanonicalRecordLocked(kind, body)
}

func (r *Repository) LoadRecord(_ context.Context, seq decisionprotocol.RecordSeq) (decisionprotocol.CanonicalRecordEnvelope, bool, error) {
	r.decisionProtocolMu.Lock()
	defer r.decisionProtocolMu.Unlock()
	if err := r.recoverDecisionProtocolLocked(); err != nil {
		return decisionprotocol.CanonicalRecordEnvelope{}, false, err
	}
	return r.loadDecisionProtocolRecordLocked(seq)
}

func (r *Repository) CurrentHighWater(_ context.Context) (decisionprotocol.RecordSeq, error) {
	r.decisionProtocolMu.Lock()
	defer r.decisionProtocolMu.Unlock()
	if err := r.recoverDecisionProtocolLocked(); err != nil {
		return 0, err
	}
	hw, err := r.readDecisionProtocolHighWaterLocked()
	return hw, err
}

func (r *Repository) ListEpisodeRecords(_ context.Context, episode decisionprotocol.EpisodeID, cut decisionprotocol.RecordSeq) ([]decisionprotocol.CanonicalRecordEnvelope, error) {
	r.decisionProtocolMu.Lock()
	defer r.decisionProtocolMu.Unlock()
	if err := r.recoverDecisionProtocolLocked(); err != nil {
		return nil, err
	}
	hw, err := r.readDecisionProtocolHighWaterLocked()
	if err != nil {
		return nil, err
	}
	if cut > hw {
		cut = hw
	}
	out := make([]decisionprotocol.CanonicalRecordEnvelope, 0)
	for seq := decisionprotocol.RecordSeq(1); seq <= cut; seq++ {
		env, ok, err := r.loadDecisionProtocolRecordLocked(seq)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("decision protocol canonical ledger gap at %d", seq)
		}
		ep, ok, err := r.episodeForDecisionRecordLocked(env, seq)
		if err != nil {
			return nil, err
		}
		if ok && ep == episode {
			out = append(out, env)
		}
	}
	return out, nil
}

func (r *Repository) appendCanonicalRecordLocked(kind decisionprotocol.RecordKind, body any) (decisionprotocol.CanonicalRecordEnvelope, error) {
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return decisionprotocol.CanonicalRecordEnvelope{}, err
	}
	hw, err := r.readDecisionProtocolHighWaterLocked()
	if err != nil {
		return decisionprotocol.CanonicalRecordEnvelope{}, err
	}
	seq := hw + 1
	env := decisionprotocol.CanonicalRecordEnvelope{SchemaVersion: 1, CanonicalRecordSeq: seq, Kind: kind, Body: bodyJSON}
	if err := env.Validate(); err != nil {
		return decisionprotocol.CanonicalRecordEnvelope{}, err
	}
	path := r.decisionProtocolRecordPath(seq)
	if res := r.writer.Create(path, env); res.Err != nil {
		return decisionprotocol.CanonicalRecordEnvelope{}, res.Err
	}
	if err := r.rebuildDecisionSecondaryForRecordLocked(env); err != nil {
		return decisionprotocol.CanonicalRecordEnvelope{}, err
	}
	if err := r.writeDecisionProtocolHighWaterLocked(seq); err != nil {
		return decisionprotocol.CanonicalRecordEnvelope{}, err
	}
	return env, nil
}

func (r *Repository) readDecisionProtocolHighWaterLocked() (decisionprotocol.RecordSeq, error) {
	var hw decisionProtocolHighWater
	err := readStrict(r.decisionProtocolHighWaterPath(), &hw)
	if errors.Is(err, ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if hw.SchemaVersion != 1 {
		return 0, fmt.Errorf("corrupt decision protocol high-water")
	}
	return hw.CanonicalRecordHighWater, nil
}

func (r *Repository) writeDecisionProtocolHighWaterLocked(seq decisionprotocol.RecordSeq) error {
	res := r.writer.Replace(r.decisionProtocolHighWaterPath(), decisionProtocolHighWater{SchemaVersion: 1, CanonicalRecordHighWater: seq})
	return res.Err
}

func (r *Repository) loadDecisionProtocolRecordLocked(seq decisionprotocol.RecordSeq) (decisionprotocol.CanonicalRecordEnvelope, bool, error) {
	var env decisionprotocol.CanonicalRecordEnvelope
	err := readStrict(r.decisionProtocolRecordPath(seq), &env)
	if errors.Is(err, ErrNotFound) {
		return env, false, nil
	}
	if err != nil {
		return env, false, err
	}
	if env.CanonicalRecordSeq != seq || env.Validate() != nil {
		return env, false, fmt.Errorf("corrupt decision protocol canonical record %d", seq)
	}
	return env, true, nil
}

func (r *Repository) recoverDecisionProtocolLocked() error {
	hw, err := r.readDecisionProtocolHighWaterLocked()
	if err != nil {
		return err
	}
	seqs, err := r.decisionProtocolRecordSeqsLocked()
	if err != nil {
		return err
	}
	max := decisionprotocol.RecordSeq(0)
	if len(seqs) > 0 {
		max = seqs[len(seqs)-1]
	}
	for seq := decisionprotocol.RecordSeq(1); seq <= hw; seq++ {
		env, ok, err := r.loadDecisionProtocolRecordLocked(seq)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("decision protocol canonical ledger corruption: high-water record %d missing", seq)
		}
		if err := r.rebuildDecisionSecondaryForRecordLocked(env); err != nil {
			return err
		}
	}
	if max > hw {
		for seq := hw + 1; seq <= max; seq++ {
			env, ok, err := r.loadDecisionProtocolRecordLocked(seq)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("decision protocol canonical ledger corruption: gap at %d", seq)
			}
			if err := r.rebuildDecisionSecondaryForRecordLocked(env); err != nil {
				return err
			}
			if err := r.writeDecisionProtocolHighWaterLocked(seq); err != nil {
				return err
			}
			hw = seq
		}
	}
	return r.validateDecisionProtocolSecondaryAuthorityLocked(hw)
}

func (r *Repository) decisionProtocolRecordSeqsLocked() ([]decisionprotocol.RecordSeq, error) {
	entries, err := os.ReadDir(r.decisionProtocolRecordDir())
	if err != nil {
		return nil, err
	}
	seqs := make([]decisionprotocol.RecordSeq, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") || len(name) != 25 {
			return nil, fmt.Errorf("unexpected decision protocol ledger entry %q", name)
		}
		n, err := strconv.ParseUint(strings.TrimSuffix(name, ".json"), 10, 64)
		if err != nil || n == 0 {
			return nil, fmt.Errorf("invalid decision protocol record path %q", name)
		}
		seqs = append(seqs, decisionprotocol.RecordSeq(n))
	}
	sort.Slice(seqs, func(i, j int) bool { return seqs[i] < seqs[j] })
	return seqs, nil
}

func (r *Repository) rebuildDecisionSecondaryForRecordLocked(env decisionprotocol.CanonicalRecordEnvelope) error {
	ep, ok, err := r.episodeForDecisionRecordLocked(env, env.CanonicalRecordSeq)
	if err != nil {
		return err
	}
	if ok {
		dir := filepath.Dir(r.decisionProtocolEpisodeIndexPath(ep, env.CanonicalRecordSeq))
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		_ = os.Chmod(dir, 0o700)
		idx := decisionProtocolEpisodeIndex{SchemaVersion: 1, CanonicalRecordSeq: env.CanonicalRecordSeq, Kind: env.Kind}
		path := r.decisionProtocolEpisodeIndexPath(ep, env.CanonicalRecordSeq)
		var existing decisionProtocolEpisodeIndex
		if err := readStrict(path, &existing); err == nil {
			if existing != idx {
				if res := r.writer.Replace(path, idx); res.Err != nil {
					return res.Err
				}
			}
		} else if errors.Is(err, ErrNotFound) {
			if res := r.writer.Create(path, idx); res.Err != nil {
				return res.Err
			}
		} else {
			return err
		}
	}
	return r.rebuildDecisionPolicySecondaryForRecordLocked(env)
}

func (r *Repository) episodeForDecisionRecordLocked(env decisionprotocol.CanonicalRecordEnvelope, cut decisionprotocol.RecordSeq) (decisionprotocol.EpisodeID, bool, error) {
	if episode, handled, err := directEpisodeForDecisionRecord(env); handled || err != nil {
		return episode, handled, err
	}
	experiment, handled, err := experimentForDecisionRecord(env)
	if err != nil || !handled {
		return "", false, err
	}
	return r.episodeForExperimentLocked(experiment, cut)
}

func directEpisodeForDecisionRecord(env decisionprotocol.CanonicalRecordEnvelope) (decisionprotocol.EpisodeID, bool, error) {
	decode := func(v any) error { return json.Unmarshal(env.Body, v) }
	switch env.Kind {
	case decisionprotocol.RecordEpisode:
		var v decisionprotocol.DecisionEpisode
		if err := decode(&v); err != nil {
			return "", true, err
		}
		return v.EpisodeID, true, nil
	case decisionprotocol.RecordCandidate:
		var v decisionprotocol.DecisionCandidate
		if err := decode(&v); err != nil {
			return "", true, err
		}
		return v.EpisodeID, true, nil
	case decisionprotocol.RecordExperiment:
		var v decisionprotocol.DecisionExperiment
		if err := decode(&v); err != nil {
			return "", true, err
		}
		return v.EpisodeID, true, nil
	case decisionprotocol.RecordPredictionBinding:
		var v decisionprotocol.PredictionBinding
		if err := decode(&v); err != nil {
			return "", true, err
		}
		return v.EpisodeID, true, nil
	case decisionprotocol.RecordVerifierAssessment:
		var v decisionprotocol.VerifierAssessment
		if err := decode(&v); err != nil {
			return "", true, err
		}
		return v.EpisodeID, true, nil
	case decisionprotocol.RecordSelectionProposal:
		var v decisionprotocol.SelectionProposal
		if err := decode(&v); err != nil {
			return "", true, err
		}
		return v.EpisodeID, true, nil
	case decisionprotocol.RecordOverride:
		var v decisionprotocol.DecisionOverride
		if err := decode(&v); err != nil {
			return "", true, err
		}
		return v.EpisodeID, true, nil
	case decisionprotocol.RecordSelectionCommit:
		var v decisionprotocol.SelectionCommit
		if err := decode(&v); err != nil {
			return "", true, err
		}
		return v.EpisodeID, true, nil
	case decisionprotocol.RecordClosure:
		var v decisionprotocol.DecisionClosure
		if err := decode(&v); err != nil {
			return "", true, err
		}
		return v.EpisodeID, true, nil
	case decisionprotocol.RecordAuthorityAttestation:
		var v decisionprotocol.DecisionAuthorityAttestation
		if err := decode(&v); err != nil {
			return "", true, err
		}
		if v.Scope.EpisodeID == "" {
			return "", true, nil
		}
		return v.Scope.EpisodeID, true, nil
	case decisionprotocol.RecordPolicySnapshot, decisionprotocol.RecordPolicyActivation:
		return "", true, nil
	default:
		return "", false, nil
	}
}

func experimentForDecisionRecord(env decisionprotocol.CanonicalRecordEnvelope) (decisionprotocol.ExperimentID, bool, error) {
	decode := func(v any) error { return json.Unmarshal(env.Body, v) }
	switch env.Kind {
	case decisionprotocol.RecordExperimentSeal:
		var v decisionprotocol.ExperimentSeal
		if err := decode(&v); err != nil {
			return "", true, err
		}
		return v.ExperimentID, true, nil
	case decisionprotocol.RecordExperimentExecutionLink:
		var v decisionprotocol.ExperimentExecutionLink
		if err := decode(&v); err != nil {
			return "", true, err
		}
		return v.ExperimentID, true, nil
	case decisionprotocol.RecordExperimentObservationBinding:
		var v decisionprotocol.ExperimentObservationBinding
		if err := decode(&v); err != nil {
			return "", true, err
		}
		return v.ExperimentID, true, nil
	case decisionprotocol.RecordExperimentClosure:
		var v decisionprotocol.ExperimentClosure
		if err := decode(&v); err != nil {
			return "", true, err
		}
		return v.ExperimentID, true, nil
	case decisionprotocol.RecordExperimentAbort:
		var v decisionprotocol.ExperimentAbort
		if err := decode(&v); err != nil {
			return "", true, err
		}
		return v.ExperimentID, true, nil
	default:
		return "", false, nil
	}
}

func (r *Repository) episodeForExperimentLocked(id decisionprotocol.ExperimentID, cut decisionprotocol.RecordSeq) (decisionprotocol.EpisodeID, bool, error) {
	if cut > 0 {
		cut--
	}
	for seq := cut; seq >= 1; seq-- {
		env, ok, err := r.loadDecisionProtocolRecordLocked(seq)
		if err != nil {
			return "", false, err
		}
		if !ok {
			continue
		}
		if env.Kind == decisionprotocol.RecordExperiment {
			var e decisionprotocol.DecisionExperiment
			if json.Unmarshal(env.Body, &e) == nil && e.ExperimentID == id {
				return e.EpisodeID, true, nil
			}
		}
		if seq == 1 {
			break
		}
	}
	return "", false, fmt.Errorf("decision protocol experiment %s has no canonical parent", id)
}

func (s *DecisionProtocolStore) AppendRecord(ctx context.Context, kind decisionprotocol.RecordKind, body any) (decisionprotocol.CanonicalRecordEnvelope, error) {
	if s == nil || s.repository == nil {
		return decisionprotocol.CanonicalRecordEnvelope{}, fmt.Errorf("decision protocol store unavailable")
	}
	return s.repository.AppendRecord(ctx, kind, body)
}
func (s *DecisionProtocolStore) LoadRecord(ctx context.Context, seq decisionprotocol.RecordSeq) (decisionprotocol.CanonicalRecordEnvelope, bool, error) {
	if s == nil || s.repository == nil {
		return decisionprotocol.CanonicalRecordEnvelope{}, false, fmt.Errorf("decision protocol store unavailable")
	}
	return s.repository.LoadRecord(ctx, seq)
}
func (s *DecisionProtocolStore) ListEpisodeRecords(ctx context.Context, episode decisionprotocol.EpisodeID, cut decisionprotocol.RecordSeq) ([]decisionprotocol.CanonicalRecordEnvelope, error) {
	if s == nil || s.repository == nil {
		return nil, fmt.Errorf("decision protocol store unavailable")
	}
	return s.repository.ListEpisodeRecords(ctx, episode, cut)
}
func (s *DecisionProtocolStore) CurrentHighWater(ctx context.Context) (decisionprotocol.RecordSeq, error) {
	if s == nil || s.repository == nil {
		return 0, fmt.Errorf("decision protocol store unavailable")
	}
	return s.repository.CurrentHighWater(ctx)
}
