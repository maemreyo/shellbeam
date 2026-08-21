package store

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	dp "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
)

func (s *DecisionProtocolStore) PutAuthorityAttestation(_ context.Context, attestation dp.DecisionAuthorityAttestation) (dp.CanonicalRecordEnvelope, bool, error) {
	if s == nil || s.repository == nil {
		return dp.CanonicalRecordEnvelope{}, false, fmt.Errorf("decision protocol store unavailable")
	}
	if err := attestation.Validate(); err != nil {
		return dp.CanonicalRecordEnvelope{}, false, err
	}
	r := s.repository
	r.decisionProtocolMu.Lock()
	defer r.decisionProtocolMu.Unlock()
	if err := r.recoverDecisionProtocolLocked(); err != nil {
		return dp.CanonicalRecordEnvelope{}, false, err
	}
	if attestation.Scope.EpisodeID != "" {
		if _, _, found, err := r.findDecisionEpisodeLocked(attestation.Scope.EpisodeID); err != nil {
			return dp.CanonicalRecordEnvelope{}, false, err
		} else if !found {
			return dp.CanonicalRecordEnvelope{}, false, fmt.Errorf("authority episode unavailable")
		}
	}
	if existing, env, found, err := r.findAuthorityAttestationLocked(attestation.AttestationID); err != nil {
		return dp.CanonicalRecordEnvelope{}, false, err
	} else if found {
		if !reflect.DeepEqual(existing, attestation) {
			return dp.CanonicalRecordEnvelope{}, false, fmt.Errorf("authority attestation identity conflict")
		}
		return env, false, nil
	}
	env, err := r.appendCanonicalRecordLocked(dp.RecordAuthorityAttestation, attestation)
	return env, err == nil, err
}
func (s *DecisionProtocolStore) FindAuthorityAttestation(_ context.Context, id string) (dp.DecisionAuthorityAttestation, bool, error) {
	if s == nil || s.repository == nil {
		return dp.DecisionAuthorityAttestation{}, false, fmt.Errorf("decision protocol store unavailable")
	}
	r := s.repository
	r.decisionProtocolMu.Lock()
	defer r.decisionProtocolMu.Unlock()
	if err := r.recoverDecisionProtocolLocked(); err != nil {
		return dp.DecisionAuthorityAttestation{}, false, err
	}
	a, _, found, err := r.findAuthorityAttestationLocked(id)
	return a, found, err
}
func (s *DecisionProtocolStore) RecordOverride(_ context.Context, o dp.DecisionOverride) (dp.CanonicalRecordEnvelope, bool, error) {
	if s == nil || s.repository == nil {
		return dp.CanonicalRecordEnvelope{}, false, fmt.Errorf("decision protocol store unavailable")
	}
	if err := o.Validate(); err != nil {
		return dp.CanonicalRecordEnvelope{}, false, err
	}
	r := s.repository
	r.decisionProtocolMu.Lock()
	defer r.decisionProtocolMu.Unlock()
	if err := r.recoverDecisionProtocolLocked(); err != nil {
		return dp.CanonicalRecordEnvelope{}, false, err
	}
	if err := r.requireDecisionCandidateInEpisodeLocked(o.EpisodeID, o.CandidateID); err != nil {
		return dp.CanonicalRecordEnvelope{}, false, err
	}
	att, _, found, err := r.findAuthorityAttestationLocked(o.AuthorityAttestationRef)
	if err != nil {
		return dp.CanonicalRecordEnvelope{}, false, err
	}
	if !found || att.ActorRef != o.ActorRef {
		return dp.CanonicalRecordEnvelope{}, false, fmt.Errorf("override authority attestation unavailable")
	}
	if existing, env, found, err := r.findOverrideLocked(o.OverrideID); err != nil {
		return dp.CanonicalRecordEnvelope{}, false, err
	} else if found {
		if !reflect.DeepEqual(existing, o) {
			return dp.CanonicalRecordEnvelope{}, false, fmt.Errorf("override identity conflict")
		}
		return env, false, nil
	}
	env, err := r.appendCanonicalRecordLocked(dp.RecordOverride, o)
	return env, err == nil, err
}
func (s *DecisionProtocolStore) FindOverride(_ context.Context, id string) (dp.DecisionOverride, bool, error) {
	if s == nil || s.repository == nil {
		return dp.DecisionOverride{}, false, fmt.Errorf("decision protocol store unavailable")
	}
	r := s.repository
	r.decisionProtocolMu.Lock()
	defer r.decisionProtocolMu.Unlock()
	if err := r.recoverDecisionProtocolLocked(); err != nil {
		return dp.DecisionOverride{}, false, err
	}
	o, _, found, err := r.findOverrideLocked(id)
	return o, found, err
}
func (r *Repository) findAuthorityAttestationLocked(id string) (dp.DecisionAuthorityAttestation, dp.CanonicalRecordEnvelope, bool, error) {
	hw, err := r.readDecisionProtocolHighWaterLocked()
	if err != nil {
		return dp.DecisionAuthorityAttestation{}, dp.CanonicalRecordEnvelope{}, false, err
	}
	var found dp.DecisionAuthorityAttestation
	var envFound dp.CanonicalRecordEnvelope
	count := 0
	for seq := dp.RecordSeq(1); seq <= hw; seq++ {
		env, ok, err := r.loadDecisionProtocolRecordLocked(seq)
		if err != nil {
			return found, envFound, false, err
		}
		if !ok {
			return found, envFound, false, fmt.Errorf("decision protocol ledger gap at %d", seq)
		}
		if env.Kind != dp.RecordAuthorityAttestation {
			continue
		}
		var a dp.DecisionAuthorityAttestation
		if err := json.Unmarshal(env.Body, &a); err != nil {
			return found, envFound, false, err
		}
		if a.AttestationID == id {
			found, envFound, count = a, env, count+1
		}
	}
	if count > 1 {
		return found, envFound, false, fmt.Errorf("duplicate authority attestation identity")
	}
	return found, envFound, count == 1, nil
}
func (r *Repository) findOverrideLocked(id string) (dp.DecisionOverride, dp.CanonicalRecordEnvelope, bool, error) {
	hw, err := r.readDecisionProtocolHighWaterLocked()
	if err != nil {
		return dp.DecisionOverride{}, dp.CanonicalRecordEnvelope{}, false, err
	}
	var found dp.DecisionOverride
	var envFound dp.CanonicalRecordEnvelope
	count := 0
	for seq := dp.RecordSeq(1); seq <= hw; seq++ {
		env, ok, err := r.loadDecisionProtocolRecordLocked(seq)
		if err != nil {
			return found, envFound, false, err
		}
		if !ok {
			return found, envFound, false, fmt.Errorf("decision protocol ledger gap at %d", seq)
		}
		if env.Kind != dp.RecordOverride {
			continue
		}
		var o dp.DecisionOverride
		if err := json.Unmarshal(env.Body, &o); err != nil {
			return found, envFound, false, err
		}
		if o.OverrideID == id {
			found, envFound, count = o, env, count+1
		}
	}
	if count > 1 {
		return found, envFound, false, fmt.Errorf("duplicate override identity")
	}
	return found, envFound, count == 1, nil
}
