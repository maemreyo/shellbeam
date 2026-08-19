package store

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	dp "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
)

type decisionExperimentState struct {
	experiment         dp.Experiment
	experimentEnv      dp.CanonicalRecordEnvelope
	seal               *dp.ExperimentSeal
	sealEnv            dp.CanonicalRecordEnvelope
	links              []dp.ExperimentExecutionLink
	observationBinding *dp.ExperimentObservationBinding
	closure            *dp.ExperimentClosure
	abort              *dp.ExperimentAbort
	predictions        []dp.PredictionBinding
}

func (s *DecisionProtocolStore) DefineExperiment(_ context.Context, experiment dp.Experiment) (dp.CanonicalRecordEnvelope, bool, error) {
	if s == nil || s.repository == nil {
		return dp.CanonicalRecordEnvelope{}, false, fmt.Errorf("decision protocol store unavailable")
	}
	if err := experiment.Validate(); err != nil {
		return dp.CanonicalRecordEnvelope{}, false, err
	}
	r := s.repository
	r.decisionProtocolMu.Lock()
	defer r.decisionProtocolMu.Unlock()
	if err := r.recoverDecisionProtocolLocked(); err != nil {
		return dp.CanonicalRecordEnvelope{}, false, err
	}
	if _, _, found, err := r.findDecisionEpisodeLocked(experiment.EpisodeID); err != nil {
		return dp.CanonicalRecordEnvelope{}, false, err
	} else if !found {
		return dp.CanonicalRecordEnvelope{}, false, fmt.Errorf("experiment episode unavailable")
	}
	state, found, err := r.findExperimentStateLocked(experiment.ExperimentID)
	if err != nil {
		return dp.CanonicalRecordEnvelope{}, false, err
	}
	if found {
		if !reflect.DeepEqual(state.experiment, experiment) {
			return dp.CanonicalRecordEnvelope{}, false, fmt.Errorf("experiment id conflicts with different canonical body")
		}
		return state.experimentEnv, false, nil
	}
	env, err := r.appendCanonicalRecordLocked(dp.RecordExperiment, experiment)
	return env, err == nil, err
}

func (s *DecisionProtocolStore) FindExperiment(_ context.Context, id dp.ExperimentID) (dp.Experiment, bool, error) {
	if s == nil || s.repository == nil {
		return dp.Experiment{}, false, fmt.Errorf("decision protocol store unavailable")
	}
	r := s.repository
	r.decisionProtocolMu.Lock()
	defer r.decisionProtocolMu.Unlock()
	if err := r.recoverDecisionProtocolLocked(); err != nil {
		return dp.Experiment{}, false, err
	}
	state, found, err := r.findExperimentStateLocked(id)
	return state.experiment, found, err
}

func (s *DecisionProtocolStore) BindPrediction(_ context.Context, prediction dp.PredictionBinding) (dp.CanonicalRecordEnvelope, bool, error) {
	if s == nil || s.repository == nil {
		return dp.CanonicalRecordEnvelope{}, false, fmt.Errorf("decision protocol store unavailable")
	}
	if err := prediction.Validate(); err != nil {
		return dp.CanonicalRecordEnvelope{}, false, err
	}
	r := s.repository
	r.decisionProtocolMu.Lock()
	defer r.decisionProtocolMu.Unlock()
	if err := r.recoverDecisionProtocolLocked(); err != nil {
		return dp.CanonicalRecordEnvelope{}, false, err
	}
	state, found, err := r.findExperimentStateLocked(prediction.ExperimentID)
	if err != nil {
		return dp.CanonicalRecordEnvelope{}, false, err
	}
	if !found {
		return dp.CanonicalRecordEnvelope{}, false, fmt.Errorf("prediction experiment unavailable")
	}
	if state.seal != nil {
		return dp.CanonicalRecordEnvelope{}, false, dp.NewReasonError(dp.ReasonExperimentAlreadySealed, "prediction binding after seal")
	}
	if state.abort != nil || state.closure != nil {
		return dp.CanonicalRecordEnvelope{}, false, fmt.Errorf("prediction experiment terminal")
	}
	if state.experiment.EpisodeID != prediction.EpisodeID {
		return dp.CanonicalRecordEnvelope{}, false, fmt.Errorf("prediction crosses experiment episode")
	}
	episode, _, ok, err := r.findDecisionEpisodeLocked(prediction.EpisodeID)
	if err != nil {
		return dp.CanonicalRecordEnvelope{}, false, err
	}
	if !ok || episode.Baseline.SourceGeneration != prediction.SourceGeneration {
		return dp.CanonicalRecordEnvelope{}, false, fmt.Errorf("prediction source generation mismatch")
	}
	candidate, _, ok, err := r.findDecisionCandidateLocked(prediction.CandidateID)
	if err != nil {
		return dp.CanonicalRecordEnvelope{}, false, err
	}
	if !ok || candidate.EpisodeID != prediction.EpisodeID {
		return dp.CanonicalRecordEnvelope{}, false, fmt.Errorf("prediction candidate unavailable or cross-episode")
	}
	if existing, env, found, err := r.findPredictionLocked(prediction.PredictionID); err != nil {
		return dp.CanonicalRecordEnvelope{}, false, err
	} else if found {
		if !reflect.DeepEqual(existing, prediction) {
			return dp.CanonicalRecordEnvelope{}, false, fmt.Errorf("prediction id conflicts with different canonical body")
		}
		return env, false, nil
	}
	env, err := r.appendCanonicalRecordLocked(dp.RecordPredictionBinding, prediction)
	return env, err == nil, err
}

func (s *DecisionProtocolStore) SealExperimentCAS(_ context.Context, seal dp.ExperimentSeal) (dp.CanonicalRecordEnvelope, bool, error) {
	if s == nil || s.repository == nil {
		return dp.CanonicalRecordEnvelope{}, false, fmt.Errorf("decision protocol store unavailable")
	}
	if err := seal.Validate(); err != nil {
		return dp.CanonicalRecordEnvelope{}, false, err
	}
	r := s.repository
	r.decisionProtocolMu.Lock()
	defer r.decisionProtocolMu.Unlock()
	if err := r.recoverDecisionProtocolLocked(); err != nil {
		return dp.CanonicalRecordEnvelope{}, false, err
	}
	state, found, err := r.findExperimentStateLocked(seal.ExperimentID)
	if err != nil {
		return dp.CanonicalRecordEnvelope{}, false, err
	}
	if !found {
		return dp.CanonicalRecordEnvelope{}, false, fmt.Errorf("experiment unavailable")
	}
	if state.abort != nil || state.closure != nil {
		return dp.CanonicalRecordEnvelope{}, false, fmt.Errorf("experiment terminal")
	}
	if state.seal != nil {
		if reflect.DeepEqual(*state.seal, seal) {
			return state.sealEnv, false, nil
		}
		return dp.CanonicalRecordEnvelope{}, false, dp.NewReasonError(dp.ReasonExperimentAlreadySealed, "different seal already exists")
	}
	episode, _, ok, err := r.findDecisionEpisodeLocked(state.experiment.EpisodeID)
	if err != nil {
		return dp.CanonicalRecordEnvelope{}, false, err
	}
	if !ok || episode.Baseline.SourceGeneration != seal.SourceGeneration || seal.BaseProjectionCutRef.EpisodeID != episode.EpisodeID {
		return dp.CanonicalRecordEnvelope{}, false, fmt.Errorf("seal source or episode mismatch")
	}
	digest, err := dp.PredictionSetDigest(state.predictions)
	if err != nil {
		return dp.CanonicalRecordEnvelope{}, false, err
	}
	if digest != seal.SealedPredictionDigest {
		return dp.CanonicalRecordEnvelope{}, false, fmt.Errorf("sealed prediction digest mismatch")
	}
	hw, err := r.readDecisionProtocolHighWaterLocked()
	if err != nil {
		return dp.CanonicalRecordEnvelope{}, false, err
	}
	if seal.BaseProjectionCutRef.CanonicalRecordHighWater != hw {
		return dp.CanonicalRecordEnvelope{}, false, dp.NewReasonError(dp.ReasonProjectionConflict, "seal projection cut is stale")
	}
	env, err := r.appendCanonicalRecordLocked(dp.RecordExperimentSeal, seal)
	return env, err == nil, err
}

func (s *DecisionProtocolStore) CloseExperimentCAS(_ context.Context, closure dp.ExperimentClosure) (dp.CanonicalRecordEnvelope, bool, error) {
	if s == nil || s.repository == nil {
		return dp.CanonicalRecordEnvelope{}, false, fmt.Errorf("decision protocol store unavailable")
	}
	if err := closure.Validate(); err != nil {
		return dp.CanonicalRecordEnvelope{}, false, err
	}
	r := s.repository
	r.decisionProtocolMu.Lock()
	defer r.decisionProtocolMu.Unlock()
	if err := r.recoverDecisionProtocolLocked(); err != nil {
		return dp.CanonicalRecordEnvelope{}, false, err
	}
	state, found, err := r.findExperimentStateLocked(closure.ExperimentID)
	if err != nil {
		return dp.CanonicalRecordEnvelope{}, false, err
	}
	if !found {
		return dp.CanonicalRecordEnvelope{}, false, fmt.Errorf("experiment unavailable")
	}
	if state.abort != nil {
		return dp.CanonicalRecordEnvelope{}, false, fmt.Errorf("aborted experiment cannot close")
	}
	if state.closure != nil {
		if reflect.DeepEqual(*state.closure, closure) {
			return findExperimentRecordEnv(r, closure.ExperimentID, dp.RecordExperimentClosure)
		}
		return dp.CanonicalRecordEnvelope{}, false, fmt.Errorf("different experiment closure already exists")
	}
	if state.observationBinding == nil || state.observationBinding.BindingID != closure.ObservationBindingID {
		return dp.CanonicalRecordEnvelope{}, false, dp.NewReasonError(dp.ReasonObservationNotSettled, "closure requires unique observation binding")
	}
	env, err := r.appendCanonicalRecordLocked(dp.RecordExperimentClosure, closure)
	return env, err == nil, err
}

func (s *DecisionProtocolStore) AbortExperimentCAS(_ context.Context, abort dp.ExperimentAbort) (dp.CanonicalRecordEnvelope, bool, error) {
	if s == nil || s.repository == nil {
		return dp.CanonicalRecordEnvelope{}, false, fmt.Errorf("decision protocol store unavailable")
	}
	if err := abort.Validate(); err != nil {
		return dp.CanonicalRecordEnvelope{}, false, err
	}
	r := s.repository
	r.decisionProtocolMu.Lock()
	defer r.decisionProtocolMu.Unlock()
	if err := r.recoverDecisionProtocolLocked(); err != nil {
		return dp.CanonicalRecordEnvelope{}, false, err
	}
	state, found, err := r.findExperimentStateLocked(abort.ExperimentID)
	if err != nil {
		return dp.CanonicalRecordEnvelope{}, false, err
	}
	if !found {
		return dp.CanonicalRecordEnvelope{}, false, fmt.Errorf("experiment unavailable")
	}
	if state.closure != nil {
		return dp.CanonicalRecordEnvelope{}, false, fmt.Errorf("closed experiment cannot abort")
	}
	if state.abort != nil {
		if reflect.DeepEqual(*state.abort, abort) {
			return findExperimentRecordEnv(r, abort.ExperimentID, dp.RecordExperimentAbort)
		}
		return dp.CanonicalRecordEnvelope{}, false, fmt.Errorf("different experiment abort already exists")
	}
	if abort.Phase == dp.AbortBeforeExecution && len(state.links) != 0 {
		return dp.CanonicalRecordEnvelope{}, false, fmt.Errorf("before-execution abort after link")
	}
	if abort.Phase == dp.AbortAfterExecutionLink {
		if len(state.links) != 1 || state.links[0].LinkID != abort.ExecutionLinkID {
			return dp.CanonicalRecordEnvelope{}, false, fmt.Errorf("after-link abort requires exact execution link")
		}
	}
	env, err := r.appendCanonicalRecordLocked(dp.RecordExperimentAbort, abort)
	return env, err == nil, err
}

func (r *Repository) findExperimentStateLocked(id dp.ExperimentID) (decisionExperimentState, bool, error) {
	hw, err := r.readDecisionProtocolHighWaterLocked()
	if err != nil {
		return decisionExperimentState{}, false, err
	}
	state := decisionExperimentState{}
	count := 0
	for seq := dp.RecordSeq(1); seq <= hw; seq++ {
		env, ok, err := r.loadDecisionProtocolRecordLocked(seq)
		if err != nil {
			return state, false, err
		}
		if !ok {
			return state, false, fmt.Errorf("decision protocol ledger gap at %d", seq)
		}
		switch env.Kind {
		case dp.RecordExperiment:
			var v dp.Experiment
			if json.Unmarshal(env.Body, &v) == nil && v.ExperimentID == id {
				state.experiment = v
				state.experimentEnv = env
				count++
			}
		case dp.RecordPredictionBinding:
			var v dp.PredictionBinding
			if json.Unmarshal(env.Body, &v) == nil && v.ExperimentID == id {
				state.predictions = append(state.predictions, v)
			}
		case dp.RecordExperimentSeal:
			var v dp.ExperimentSeal
			if json.Unmarshal(env.Body, &v) == nil && v.ExperimentID == id {
				if state.seal != nil {
					return state, false, fmt.Errorf("duplicate experiment seal")
				}
				state.seal = &v
				state.sealEnv = env
			}
		case dp.RecordExperimentExecutionLink:
			var v dp.ExperimentExecutionLink
			if json.Unmarshal(env.Body, &v) == nil && v.ExperimentID == id {
				state.links = append(state.links, v)
			}
		case dp.RecordExperimentObservationBinding:
			var v dp.ExperimentObservationBinding
			if json.Unmarshal(env.Body, &v) == nil && v.ExperimentID == id {
				if state.observationBinding != nil {
					return state, false, fmt.Errorf("duplicate experiment observation binding")
				}
				state.observationBinding = &v
			}
		case dp.RecordExperimentClosure:
			var v dp.ExperimentClosure
			if json.Unmarshal(env.Body, &v) == nil && v.ExperimentID == id {
				if state.closure != nil {
					return state, false, fmt.Errorf("duplicate experiment closure")
				}
				state.closure = &v
			}
		case dp.RecordExperimentAbort:
			var v dp.ExperimentAbort
			if json.Unmarshal(env.Body, &v) == nil && v.ExperimentID == id {
				if state.abort != nil {
					return state, false, fmt.Errorf("duplicate experiment abort")
				}
				state.abort = &v
			}
		}
	}
	if count > 1 {
		return state, false, fmt.Errorf("duplicate canonical experiment identity")
	}
	if state.closure != nil && state.abort != nil {
		return state, false, fmt.Errorf("experiment has both closure and abort")
	}
	return state, count == 1, nil
}

func (r *Repository) findPredictionLocked(id dp.PredictionID) (dp.PredictionBinding, dp.CanonicalRecordEnvelope, bool, error) {
	hw, err := r.readDecisionProtocolHighWaterLocked()
	if err != nil {
		return dp.PredictionBinding{}, dp.CanonicalRecordEnvelope{}, false, err
	}
	var found dp.PredictionBinding
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
		if env.Kind != dp.RecordPredictionBinding {
			continue
		}
		var v dp.PredictionBinding
		if err := json.Unmarshal(env.Body, &v); err != nil {
			return found, envFound, false, err
		}
		if v.PredictionID == id {
			found = v
			envFound = env
			count++
		}
	}
	if count > 1 {
		return found, envFound, false, fmt.Errorf("duplicate prediction identity")
	}
	return found, envFound, count == 1, nil
}

func findExperimentRecordEnv(r *Repository, id dp.ExperimentID, kind dp.RecordKind) (dp.CanonicalRecordEnvelope, bool, error) {
	hw, err := r.readDecisionProtocolHighWaterLocked()
	if err != nil {
		return dp.CanonicalRecordEnvelope{}, false, err
	}
	for seq := hw; seq >= 1; seq-- {
		env, ok, err := r.loadDecisionProtocolRecordLocked(seq)
		if err != nil {
			return dp.CanonicalRecordEnvelope{}, false, err
		}
		if ok && env.Kind == kind {
			episode, belongs, err := r.episodeForDecisionRecordLocked(env, seq)
			_ = episode
			if err == nil && belongs {
				switch kind {
				case dp.RecordExperimentClosure:
					var v dp.ExperimentClosure
					if json.Unmarshal(env.Body, &v) == nil && v.ExperimentID == id {
						return env, true, nil
					}
				case dp.RecordExperimentAbort:
					var v dp.ExperimentAbort
					if json.Unmarshal(env.Body, &v) == nil && v.ExperimentID == id {
						return env, true, nil
					}
				}
			}
		}
		if seq == 1 {
			break
		}
	}
	return dp.CanonicalRecordEnvelope{}, false, fmt.Errorf("experiment record unavailable")
}
