package decisionprotocol

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	core "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
)

func (s *Service) DefineExperiment(ctx context.Context, experiment core.Experiment) (core.DecisionProjection, error) {
	if s == nil || s.experiments == nil {
		return core.DecisionProjection{}, fmt.Errorf("decision experiment store unavailable")
	}
	if _, _, err := s.experiments.DefineExperiment(ctx, experiment); err != nil {
		return core.DecisionProjection{}, err
	}
	return s.Inspect(ctx, experiment.EpisodeID, "")
}

func (s *Service) BindPrediction(ctx context.Context, prediction core.PredictionBinding) (core.DecisionProjection, error) {
	if s == nil || s.experiments == nil {
		return core.DecisionProjection{}, fmt.Errorf("decision experiment store unavailable")
	}
	if _, _, err := s.experiments.BindPrediction(ctx, prediction); err != nil {
		return core.DecisionProjection{}, err
	}
	return s.Inspect(ctx, prediction.EpisodeID, prediction.CandidateID)
}

func (s *Service) SealExperiment(ctx context.Context, experimentID core.ExperimentID, actor string) (core.ExperimentSeal, core.DecisionProjection, error) {
	if s == nil || s.experiments == nil || s.mutations == nil || s.ledger == nil {
		return core.ExperimentSeal{}, core.DecisionProjection{}, fmt.Errorf("decision experiment dependencies unavailable")
	}
	if actor == "" {
		return core.ExperimentSeal{}, core.DecisionProjection{}, fmt.Errorf("seal actor missing")
	}
	experiment, ok, err := s.experiments.FindExperiment(ctx, experimentID)
	if err != nil {
		return core.ExperimentSeal{}, core.DecisionProjection{}, err
	}
	if !ok {
		return core.ExperimentSeal{}, core.DecisionProjection{}, fmt.Errorf("experiment unavailable")
	}
	episode, ok, err := s.mutations.FindEpisode(ctx, experiment.EpisodeID)
	if err != nil {
		return core.ExperimentSeal{}, core.DecisionProjection{}, err
	}
	if !ok {
		return core.ExperimentSeal{}, core.DecisionProjection{}, fmt.Errorf("experiment episode unavailable")
	}
	cut, err := s.ledger.CurrentHighWater(ctx)
	if err != nil {
		return core.ExperimentSeal{}, core.DecisionProjection{}, err
	}
	records, err := s.ledger.ListEpisodeRecords(ctx, episode.EpisodeID, cut)
	if err != nil {
		return core.ExperimentSeal{}, core.DecisionProjection{}, err
	}
	if existing, found, err := existingExperimentSeal(records, experimentID); err != nil {
		return core.ExperimentSeal{}, core.DecisionProjection{}, err
	} else if found {
		projection, err := s.Inspect(ctx, episode.EpisodeID, "")
		return existing, projection, err
	}
	predictions := predictionsForExperiment(records, experimentID)
	if err := validateSealPredictions(episode.EpisodeID, episode.Baseline.SourceGeneration, predictions); err != nil {
		return core.ExperimentSeal{}, core.DecisionProjection{}, err
	}
	base, err := projectEpisodeRecords(episode, "", records)
	if err != nil {
		return core.ExperimentSeal{}, core.DecisionProjection{}, err
	}
	semantic := core.ProjectionSemanticState{EpisodeID: episode.EpisodeID, CandidateStates: candidateSemanticStates(base.Candidates), Gate: core.GateIndeterminate, SourceCompatible: true}
	baseDigest, err := core.ProjectionDigest(semantic)
	if err != nil {
		return core.ExperimentSeal{}, core.DecisionProjection{}, err
	}
	blocked, err := requiredMismatchCandidates(records)
	if err != nil {
		return core.ExperimentSeal{}, core.DecisionProjection{}, err
	}
	pairs, err := potentialDiscriminationPairs(base.Candidates, predictions, blocked)
	if err != nil {
		return core.ExperimentSeal{}, core.DecisionProjection{}, err
	}
	predictionDigest, err := core.PredictionSetDigest(predictions)
	if err != nil {
		return core.ExperimentSeal{}, core.DecisionProjection{}, err
	}
	seal := core.ExperimentSeal{ExperimentID: experimentID, SourceGeneration: episode.Baseline.SourceGeneration, SealedPredictionDigest: predictionDigest, BaseProjectionCutRef: core.DecisionProjectionCutRef{EpisodeID: episode.EpisodeID, CanonicalRecordHighWater: cut}, BaseCandidateProjectionDigest: baseDigest, PotentialDiscriminationPairs: pairs, SealedAt: s.now().UTC()}
	if _, _, err := s.experiments.SealExperimentCAS(ctx, seal); err != nil {
		return core.ExperimentSeal{}, core.DecisionProjection{}, err
	}
	projection, err := s.Inspect(ctx, episode.EpisodeID, "")
	return seal, projection, err
}

func (s *Service) CloseExperiment(ctx context.Context, experimentID core.ExperimentID, actor string) (core.DecisionProjection, error) {
	if s == nil || s.experiments == nil || s.ledger == nil {
		return core.DecisionProjection{}, fmt.Errorf("decision experiment dependencies unavailable")
	}
	experiment, ok, err := s.experiments.FindExperiment(ctx, experimentID)
	if err != nil {
		return core.DecisionProjection{}, err
	}
	if !ok {
		return core.DecisionProjection{}, fmt.Errorf("experiment unavailable")
	}
	hw, err := s.ledger.CurrentHighWater(ctx)
	if err != nil {
		return core.DecisionProjection{}, err
	}
	records, err := s.ledger.ListEpisodeRecords(ctx, experiment.EpisodeID, hw)
	if err != nil {
		return core.DecisionProjection{}, err
	}
	if _, found, err := existingExperimentClosure(records, experimentID); err != nil {
		return core.DecisionProjection{}, err
	} else if found {
		return s.Inspect(ctx, experiment.EpisodeID, "")
	}
	binding, found, err := uniqueObservationBinding(records, experimentID)
	if err != nil {
		return core.DecisionProjection{}, err
	}
	if !found {
		return core.DecisionProjection{}, core.NewReasonError(core.ReasonObservationNotSettled, "experiment observation binding unavailable")
	}
	closure := core.ExperimentClosure{SchemaVersion: 1, ClosureID: semanticRecordID("close", string(experimentID), string(binding.BindingID)), ExperimentID: experimentID, ObservationBindingID: binding.BindingID, ClosedByActorRef: actor, ClosedAt: s.now().UTC()}
	if _, _, err := s.experiments.CloseExperimentCAS(ctx, closure); err != nil {
		return core.DecisionProjection{}, err
	}
	return s.Inspect(ctx, experiment.EpisodeID, "")
}

func (s *Service) AbortExperiment(ctx context.Context, experimentID core.ExperimentID, phase core.AbortPhase, reason, actor string) (core.DecisionProjection, error) {
	if s == nil || s.experiments == nil || s.ledger == nil {
		return core.DecisionProjection{}, fmt.Errorf("decision experiment dependencies unavailable")
	}
	experiment, ok, err := s.experiments.FindExperiment(ctx, experimentID)
	if err != nil {
		return core.DecisionProjection{}, err
	}
	if !ok {
		return core.DecisionProjection{}, fmt.Errorf("experiment unavailable")
	}
	hw, err := s.ledger.CurrentHighWater(ctx)
	if err != nil {
		return core.DecisionProjection{}, err
	}
	records, err := s.ledger.ListEpisodeRecords(ctx, experiment.EpisodeID, hw)
	if err != nil {
		return core.DecisionProjection{}, err
	}
	if existing, found, err := existingExperimentAbort(records, experimentID); err != nil {
		return core.DecisionProjection{}, err
	} else if found {
		if existing.Phase != phase || existing.Reason != reason || existing.AbortedByActorRef != actor {
			return core.DecisionProjection{}, fmt.Errorf("experiment already aborted with different intent")
		}
		return s.Inspect(ctx, experiment.EpisodeID, "")
	}
	var linkID core.LinkID
	if phase == core.AbortAfterExecutionLink {
		links := executionLinks(records, experimentID)
		if len(links) != 1 {
			return core.DecisionProjection{}, fmt.Errorf("after-link abort requires exactly one execution link")
		}
		linkID = links[0].LinkID
	}
	abort := core.ExperimentAbort{SchemaVersion: 1, AbortID: semanticRecordID("abort", string(experimentID), string(phase), reason, actor), ExperimentID: experimentID, Phase: phase, ExecutionLinkID: linkID, Reason: reason, AbortedByActorRef: actor, AbortedAt: s.now().UTC()}
	if _, _, err := s.experiments.AbortExperimentCAS(ctx, abort); err != nil {
		return core.DecisionProjection{}, err
	}
	return s.Inspect(ctx, experiment.EpisodeID, "")
}

func validateSealPredictions(episode core.EpisodeID, source string, predictions []core.PredictionBinding) error {
	for _, p := range predictions {
		if p.EpisodeID != episode || p.SourceGeneration != source {
			return fmt.Errorf("prediction outside seal episode/source generation")
		}
		if err := p.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func predictionsForExperiment(records []core.CanonicalRecordEnvelope, id core.ExperimentID) []core.PredictionBinding {
	var out []core.PredictionBinding
	for _, record := range records {
		if record.Kind != core.RecordPredictionBinding {
			continue
		}
		var p core.PredictionBinding
		if json.Unmarshal(record.Body, &p) == nil && p.ExperimentID == id {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PredictionID < out[j].PredictionID })
	return out
}

func potentialDiscriminationPairs(candidates []core.CandidateProjection, predictions []core.PredictionBinding, blocked map[core.CandidateID]bool) ([]core.PotentialDiscriminationPair, error) {
	byID := map[core.CandidateID]core.CandidateProjection{}
	for _, c := range candidates {
		byID[c.CandidateID] = c
	}
	type predictionKey struct {
		candidate core.CandidateID
		dimension string
	}
	byDim := map[predictionKey]core.PredictionBinding{}
	for _, p := range predictions {
		dimension, err := core.ObservationDimensionKey(p.Predicate)
		if err != nil {
			return nil, err
		}
		byDim[predictionKey{p.CandidateID, dimension}] = p
	}
	var out []core.PotentialDiscriminationPair
	seen := map[string]bool{}
	for _, target := range candidates {
		if target.State != core.CandidateActive || blocked[target.CandidateID] {
			continue
		}
		for _, challenger := range candidates {
			if target.CandidateID == challenger.CandidateID || target.LineageRoot == challenger.LineageRoot || challenger.State != core.CandidateActive || blocked[challenger.CandidateID] {
				continue
			}
			for key, tp := range byDim {
				if key.candidate != target.CandidateID {
					continue
				}
				cp, ok := byDim[predictionKey{challenger.CandidateID, key.dimension}]
				if !ok {
					continue
				}
				left, _ := json.Marshal(tp.Predicate)
				right, _ := json.Marshal(cp.Predicate)
				if bytes.Equal(left, right) {
					continue
				}
				identity := string(target.CandidateID) + "\x00" + string(challenger.CandidateID) + "\x00" + key.dimension
				if seen[identity] {
					continue
				}
				seen[identity] = true
				out = append(out, core.PotentialDiscriminationPair{TargetCandidateID: target.CandidateID, ChallengerCandidateID: challenger.CandidateID, DimensionKey: key.dimension})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TargetCandidateID == out[j].TargetCandidateID {
			if out[i].ChallengerCandidateID == out[j].ChallengerCandidateID {
				return out[i].DimensionKey < out[j].DimensionKey
			}
			return out[i].ChallengerCandidateID < out[j].ChallengerCandidateID
		}
		return out[i].TargetCandidateID < out[j].TargetCandidateID
	})
	return out, nil
}

func requiredMismatchCandidates(records []core.CanonicalRecordEnvelope) (map[core.CandidateID]bool, error) {
	predictions := map[core.PredictionID]core.PredictionBinding{}
	for _, record := range records {
		if record.Kind == core.RecordPredictionBinding {
			var p core.PredictionBinding
			if err := json.Unmarshal(record.Body, &p); err != nil {
				return nil, err
			}
			predictions[p.PredictionID] = p
		}
	}
	blocked := map[core.CandidateID]bool{}
	for _, record := range records {
		if record.Kind != core.RecordExperimentObservationBinding {
			continue
		}
		var binding core.ExperimentObservationBinding
		if err := json.Unmarshal(record.Body, &binding); err != nil {
			return nil, err
		}
		for _, result := range binding.PredictionResults {
			p, ok := predictions[result.PredictionID]
			if ok && p.Role == core.PredictionRequired && result.Status == core.PredictionMismatch {
				blocked[p.CandidateID] = true
			}
		}
	}
	return blocked, nil
}

func semanticRecordID(prefix string, parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	return prefix + "_" + hex.EncodeToString(h.Sum(nil))
}
func existingExperimentSeal(records []core.CanonicalRecordEnvelope, id core.ExperimentID) (core.ExperimentSeal, bool, error) {
	for _, r := range records {
		if r.Kind == core.RecordExperimentSeal {
			var v core.ExperimentSeal
			if err := json.Unmarshal(r.Body, &v); err != nil {
				return v, false, err
			}
			if v.ExperimentID == id {
				return v, true, nil
			}
		}
	}
	return core.ExperimentSeal{}, false, nil
}
func existingExperimentClosure(records []core.CanonicalRecordEnvelope, id core.ExperimentID) (core.ExperimentClosure, bool, error) {
	for _, r := range records {
		if r.Kind == core.RecordExperimentClosure {
			var v core.ExperimentClosure
			if err := json.Unmarshal(r.Body, &v); err != nil {
				return v, false, err
			}
			if v.ExperimentID == id {
				return v, true, nil
			}
		}
	}
	return core.ExperimentClosure{}, false, nil
}
func existingExperimentAbort(records []core.CanonicalRecordEnvelope, id core.ExperimentID) (core.ExperimentAbort, bool, error) {
	for _, r := range records {
		if r.Kind == core.RecordExperimentAbort {
			var v core.ExperimentAbort
			if err := json.Unmarshal(r.Body, &v); err != nil {
				return v, false, err
			}
			if v.ExperimentID == id {
				return v, true, nil
			}
		}
	}
	return core.ExperimentAbort{}, false, nil
}
func uniqueObservationBinding(records []core.CanonicalRecordEnvelope, id core.ExperimentID) (core.ExperimentObservationBinding, bool, error) {
	var found core.ExperimentObservationBinding
	count := 0
	for _, r := range records {
		if r.Kind == core.RecordExperimentObservationBinding {
			var v core.ExperimentObservationBinding
			if err := json.Unmarshal(r.Body, &v); err != nil {
				return found, false, err
			}
			if v.ExperimentID == id {
				found = v
				count++
			}
		}
	}
	if count > 1 {
		return found, false, fmt.Errorf("duplicate experiment observation binding")
	}
	return found, count == 1, nil
}
func executionLinks(records []core.CanonicalRecordEnvelope, id core.ExperimentID) []core.ExperimentExecutionLink {
	var out []core.ExperimentExecutionLink
	for _, r := range records {
		if r.Kind == core.RecordExperimentExecutionLink {
			var v core.ExperimentExecutionLink
			if json.Unmarshal(r.Body, &v) == nil && v.ExperimentID == id {
				out = append(out, v)
			}
		}
	}
	return out
}
