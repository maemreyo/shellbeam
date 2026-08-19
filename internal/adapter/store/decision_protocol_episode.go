package store

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	decisionprotocol "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
)

func (s *DecisionProtocolStore) CreateEpisode(_ context.Context, episode decisionprotocol.Episode) (decisionprotocol.CanonicalRecordEnvelope, bool, error) {
	if s == nil || s.repository == nil {
		return decisionprotocol.CanonicalRecordEnvelope{}, false, fmt.Errorf("decision protocol store unavailable")
	}
	if err := episode.Validate(); err != nil {
		return decisionprotocol.CanonicalRecordEnvelope{}, false, err
	}
	r := s.repository
	r.decisionProtocolMu.Lock()
	defer r.decisionProtocolMu.Unlock()
	if err := r.recoverDecisionProtocolLocked(); err != nil {
		return decisionprotocol.CanonicalRecordEnvelope{}, false, err
	}
	if existing, env, found, err := r.findDecisionEpisodeLocked(episode.EpisodeID); err != nil {
		return decisionprotocol.CanonicalRecordEnvelope{}, false, err
	} else if found {
		if !reflect.DeepEqual(existing, episode) {
			return decisionprotocol.CanonicalRecordEnvelope{}, false, fmt.Errorf("decision episode id conflicts with different canonical body")
		}
		return env, false, nil
	}
	env, err := r.appendCanonicalRecordLocked(decisionprotocol.RecordEpisode, episode)
	return env, err == nil, err
}

func (s *DecisionProtocolStore) FindEpisode(_ context.Context, id decisionprotocol.EpisodeID) (decisionprotocol.Episode, bool, error) {
	if s == nil || s.repository == nil {
		return decisionprotocol.Episode{}, false, fmt.Errorf("decision protocol store unavailable")
	}
	r := s.repository
	r.decisionProtocolMu.Lock()
	defer r.decisionProtocolMu.Unlock()
	if err := r.recoverDecisionProtocolLocked(); err != nil {
		return decisionprotocol.Episode{}, false, err
	}
	episode, _, found, err := r.findDecisionEpisodeLocked(id)
	return episode, found, err
}

func (s *DecisionProtocolStore) CreateCandidate(_ context.Context, candidate decisionprotocol.Candidate) (decisionprotocol.CanonicalRecordEnvelope, bool, error) {
	if s == nil || s.repository == nil {
		return decisionprotocol.CanonicalRecordEnvelope{}, false, fmt.Errorf("decision protocol store unavailable")
	}
	if err := candidate.Validate(); err != nil {
		return decisionprotocol.CanonicalRecordEnvelope{}, false, err
	}
	if candidate.RevisesCandidateID != "" {
		return decisionprotocol.CanonicalRecordEnvelope{}, false, fmt.Errorf("candidate.create cannot create a revision")
	}
	r := s.repository
	r.decisionProtocolMu.Lock()
	defer r.decisionProtocolMu.Unlock()
	if err := r.recoverDecisionProtocolLocked(); err != nil {
		return decisionprotocol.CanonicalRecordEnvelope{}, false, err
	}
	if _, _, found, err := r.findDecisionEpisodeLocked(candidate.EpisodeID); err != nil {
		return decisionprotocol.CanonicalRecordEnvelope{}, false, err
	} else if !found {
		return decisionprotocol.CanonicalRecordEnvelope{}, false, fmt.Errorf("candidate episode unavailable")
	}
	if existing, env, found, err := r.findDecisionCandidateLocked(candidate.CandidateID); err != nil {
		return decisionprotocol.CanonicalRecordEnvelope{}, false, err
	} else if found {
		if !reflect.DeepEqual(existing, candidate) {
			return decisionprotocol.CanonicalRecordEnvelope{}, false, fmt.Errorf("candidate id conflicts with different canonical body")
		}
		return env, false, nil
	}
	env, err := r.appendCanonicalRecordLocked(decisionprotocol.RecordCandidate, candidate)
	return env, err == nil, err
}

func (s *DecisionProtocolStore) FindCandidate(_ context.Context, id decisionprotocol.CandidateID) (decisionprotocol.Candidate, bool, error) {
	if s == nil || s.repository == nil {
		return decisionprotocol.Candidate{}, false, fmt.Errorf("decision protocol store unavailable")
	}
	r := s.repository
	r.decisionProtocolMu.Lock()
	defer r.decisionProtocolMu.Unlock()
	if err := r.recoverDecisionProtocolLocked(); err != nil {
		return decisionprotocol.Candidate{}, false, err
	}
	candidate, _, found, err := r.findDecisionCandidateLocked(id)
	return candidate, found, err
}

func (s *DecisionProtocolStore) ReviseCandidateCAS(_ context.Context, parentID decisionprotocol.CandidateID, child decisionprotocol.Candidate) (decisionprotocol.CanonicalRecordEnvelope, error) {
	if s == nil || s.repository == nil {
		return decisionprotocol.CanonicalRecordEnvelope{}, fmt.Errorf("decision protocol store unavailable")
	}
	if err := child.Validate(); err != nil {
		return decisionprotocol.CanonicalRecordEnvelope{}, err
	}
	if child.RevisesCandidateID != parentID {
		return decisionprotocol.CanonicalRecordEnvelope{}, fmt.Errorf("revision child does not name requested parent")
	}
	r := s.repository
	r.decisionProtocolMu.Lock()
	defer r.decisionProtocolMu.Unlock()
	if err := r.recoverDecisionProtocolLocked(); err != nil {
		return decisionprotocol.CanonicalRecordEnvelope{}, err
	}
	parent, _, found, err := r.findDecisionCandidateLocked(parentID)
	if err != nil {
		return decisionprotocol.CanonicalRecordEnvelope{}, err
	}
	if !found {
		return decisionprotocol.CanonicalRecordEnvelope{}, fmt.Errorf("revision parent unavailable")
	}
	if parent.EpisodeID != child.EpisodeID {
		return decisionprotocol.CanonicalRecordEnvelope{}, fmt.Errorf("candidate revision crosses episodes")
	}
	if existing, env, found, err := r.findDecisionCandidateLocked(child.CandidateID); err != nil {
		return decisionprotocol.CanonicalRecordEnvelope{}, err
	} else if found {
		if reflect.DeepEqual(existing, child) {
			return env, nil
		}
		return decisionprotocol.CanonicalRecordEnvelope{}, fmt.Errorf("candidate id conflicts with different canonical body")
	}
	if err := r.validateCandidateRevisionLineageLocked(parent, child); err != nil {
		return decisionprotocol.CanonicalRecordEnvelope{}, err
	}
	active, err := r.candidateActiveLocked(parentID)
	if err != nil {
		return decisionprotocol.CanonicalRecordEnvelope{}, err
	}
	if !active {
		return decisionprotocol.CanonicalRecordEnvelope{}, decisionprotocol.NewReasonError(decisionprotocol.ReasonCandidateRevisionConflict, "candidate already superseded")
	}
	return r.appendCanonicalRecordLocked(decisionprotocol.RecordCandidate, child)
}

func (r *Repository) findDecisionEpisodeLocked(id decisionprotocol.EpisodeID) (decisionprotocol.Episode, decisionprotocol.CanonicalRecordEnvelope, bool, error) {
	hw, err := r.readDecisionProtocolHighWaterLocked()
	if err != nil {
		return decisionprotocol.Episode{}, decisionprotocol.CanonicalRecordEnvelope{}, false, err
	}
	var found decisionprotocol.Episode
	var foundEnv decisionprotocol.CanonicalRecordEnvelope
	count := 0
	for seq := decisionprotocol.RecordSeq(1); seq <= hw; seq++ {
		env, ok, err := r.loadDecisionProtocolRecordLocked(seq)
		if err != nil {
			return found, foundEnv, false, err
		}
		if !ok {
			return found, foundEnv, false, fmt.Errorf("decision protocol ledger gap at %d", seq)
		}
		if env.Kind != decisionprotocol.RecordEpisode {
			continue
		}
		var episode decisionprotocol.Episode
		if err := json.Unmarshal(env.Body, &episode); err != nil {
			return found, foundEnv, false, err
		}
		if episode.EpisodeID == id {
			found, foundEnv, count = episode, env, count+1
		}
	}
	if count > 1 {
		return found, foundEnv, false, fmt.Errorf("duplicate canonical decision episode identity")
	}
	return found, foundEnv, count == 1, nil
}

func (r *Repository) findDecisionCandidateLocked(id decisionprotocol.CandidateID) (decisionprotocol.Candidate, decisionprotocol.CanonicalRecordEnvelope, bool, error) {
	hw, err := r.readDecisionProtocolHighWaterLocked()
	if err != nil {
		return decisionprotocol.Candidate{}, decisionprotocol.CanonicalRecordEnvelope{}, false, err
	}
	var found decisionprotocol.Candidate
	var foundEnv decisionprotocol.CanonicalRecordEnvelope
	count := 0
	for seq := decisionprotocol.RecordSeq(1); seq <= hw; seq++ {
		env, ok, err := r.loadDecisionProtocolRecordLocked(seq)
		if err != nil {
			return found, foundEnv, false, err
		}
		if !ok {
			return found, foundEnv, false, fmt.Errorf("decision protocol ledger gap at %d", seq)
		}
		if env.Kind != decisionprotocol.RecordCandidate {
			continue
		}
		var candidate decisionprotocol.Candidate
		if err := json.Unmarshal(env.Body, &candidate); err != nil {
			return found, foundEnv, false, err
		}
		if candidate.CandidateID == id {
			found, foundEnv, count = candidate, env, count+1
		}
	}
	if count > 1 {
		return found, foundEnv, false, fmt.Errorf("duplicate canonical decision candidate identity")
	}
	return found, foundEnv, count == 1, nil
}

func (r *Repository) candidateActiveLocked(id decisionprotocol.CandidateID) (bool, error) {
	hw, err := r.readDecisionProtocolHighWaterLocked()
	if err != nil {
		return false, err
	}
	for seq := decisionprotocol.RecordSeq(1); seq <= hw; seq++ {
		env, ok, err := r.loadDecisionProtocolRecordLocked(seq)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, fmt.Errorf("decision protocol ledger gap at %d", seq)
		}
		if env.Kind != decisionprotocol.RecordCandidate {
			continue
		}
		var candidate decisionprotocol.Candidate
		if err := json.Unmarshal(env.Body, &candidate); err != nil {
			return false, err
		}
		if candidate.RevisesCandidateID == id {
			return false, nil
		}
	}
	return true, nil
}

func (r *Repository) validateCandidateRevisionLineageLocked(parent, child decisionprotocol.Candidate) error {
	seen := map[decisionprotocol.CandidateID]struct{}{child.CandidateID: {}}
	current := parent
	for {
		if _, exists := seen[current.CandidateID]; exists {
			return fmt.Errorf("candidate revision cycle")
		}
		seen[current.CandidateID] = struct{}{}
		if current.RevisesCandidateID == "" {
			return nil
		}
		next, _, found, err := r.findDecisionCandidateLocked(current.RevisesCandidateID)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("candidate revision parent chain missing")
		}
		if next.EpisodeID != child.EpisodeID {
			return fmt.Errorf("candidate revision lineage crosses episodes")
		}
		current = next
	}
}
