package decisionprotocol

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	core "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
	"github.com/maemreyo/shellbeam/internal/core/workspace"
)

func (s *Service) Inspect(ctx context.Context, episodeID core.EpisodeID, candidateID core.CandidateID) (core.DecisionProjection, error) {
	if s == nil || s.mutations == nil || s.ledger == nil || s.workspaces == nil || s.snapshots == nil {
		return core.DecisionProjection{}, fmt.Errorf("decision projection dependencies unavailable")
	}
	episode, found, err := s.mutations.FindEpisode(ctx, episodeID)
	if err != nil {
		return core.DecisionProjection{}, err
	}
	if !found {
		return core.DecisionProjection{}, ErrEpisodeNotFound
	}
	hw, err := s.ledger.CurrentHighWater(ctx)
	if err != nil {
		return core.DecisionProjection{}, err
	}
	records, err := s.ledger.ListEpisodeRecords(ctx, episodeID, hw)
	if err != nil {
		return core.DecisionProjection{}, err
	}
	view, err := projectEpisodeRecords(episode, candidateID, records)
	if err != nil {
		return core.DecisionProjection{}, err
	}
	compatibility := s.sourceCompatibility(ctx, episode)
	view.SourceGenerationCompatibility = compatibility
	view.SourceCompatible = compatibility == core.SourceGenerationCurrent

	semantic := core.ProjectionSemanticState{
		EpisodeID:        episodeID,
		CandidateID:      candidateID,
		CandidateStates:  candidateSemanticStates(view.Candidates),
		Gate:             core.GateIndeterminate,
		SourceCompatible: view.SourceCompatible,
	}
	projectionDigest, err := core.ProjectionDigest(semantic)
	if err != nil {
		return core.DecisionProjection{}, err
	}
	seqs := make([]core.RecordSeq, 0, len(records))
	for _, record := range records {
		seqs = append(seqs, record.CanonicalRecordSeq)
	}
	auditDigest, err := core.AuditDigest(core.AuditState{EpisodeID: episodeID, CanonicalRecordSeqs: seqs})
	if err != nil {
		return core.DecisionProjection{}, err
	}
	view.ProjectionDigest = projectionDigest
	view.AuditDigest = auditDigest
	return view, nil
}

func projectEpisodeRecords(episode core.Episode, requested core.CandidateID, records []core.CanonicalRecordEnvelope) (core.DecisionProjection, error) {
	candidates := map[core.CandidateID]core.Candidate{}
	replaced := map[core.CandidateID]bool{}
	commits, closures := 0, 0
	for _, record := range records {
		switch record.Kind {
		case core.RecordCandidate:
			var candidate core.Candidate
			if err := json.Unmarshal(record.Body, &candidate); err != nil {
				return core.DecisionProjection{}, err
			}
			if candidate.EpisodeID != episode.EpisodeID {
				return core.DecisionProjection{}, fmt.Errorf("candidate indexed into wrong episode")
			}
			if _, exists := candidates[candidate.CandidateID]; exists {
				return core.DecisionProjection{}, fmt.Errorf("duplicate candidate identity in projection")
			}
			candidates[candidate.CandidateID] = candidate
			if candidate.RevisesCandidateID != "" {
				replaced[candidate.RevisesCandidateID] = true
			}
		case core.RecordSelectionCommit:
			commits++
		case core.RecordClosure:
			closures++
		}
	}
	if commits > 1 || closures > 1 || (commits > 0 && closures > 0) {
		return core.DecisionProjection{}, fmt.Errorf("corrupt decision episode terminal state")
	}
	state := core.EpisodeOpen
	if commits == 1 {
		state = core.EpisodeCommitted
	} else if closures == 1 {
		state = core.EpisodeClosedUnresolved
	}
	if requested != "" {
		if _, ok := candidates[requested]; !ok {
			return core.DecisionProjection{}, ErrCandidateNotFound
		}
	}
	ids := make([]core.CandidateID, 0, len(candidates))
	for id := range candidates {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	projections := make([]core.CandidateProjection, 0, len(ids))
	for _, id := range ids {
		root, err := candidateLineageRoot(id, candidates)
		if err != nil {
			return core.DecisionProjection{}, err
		}
		candidateState := core.CandidateActive
		if replaced[id] {
			candidateState = core.CandidateSuperseded
		}
		projections = append(projections, core.CandidateProjection{CandidateID: id, LineageRoot: root, State: candidateState})
	}
	experiments, err := experimentLifecycleProjections(records)
	if err != nil {
		return core.DecisionProjection{}, err
	}
	return core.DecisionProjection{EpisodeID: episode.EpisodeID, EpisodeState: state, EpisodeKind: episode.EpisodeKind, PolicyBinding: episode.PolicyBinding, CandidateID: requested, Candidates: projections, Experiments: experiments}, nil
}

type experimentLifecycleFacts struct {
	defined                                      bool
	seals, links, observations, closures, aborts int
	seal                                         *core.ExperimentSeal
	binding                                      *core.ExperimentObservationBinding
}

func experimentLifecycleProjections(records []core.CanonicalRecordEnvelope) ([]core.ExperimentProjection, error) {
	facts, err := collectExperimentLifecycleFacts(records)
	if err != nil {
		return nil, err
	}
	ids := make([]core.ExperimentID, 0, len(facts))
	for id := range facts {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out := make([]core.ExperimentProjection, 0, len(ids))
	for _, id := range ids {
		fact := facts[id]
		state, err := projectExperimentLifecycle(id, fact)
		if err != nil {
			return nil, err
		}
		projection := core.ExperimentProjection{ExperimentID: id, State: state}
		if fact.links == 1 {
			projection.ObservationState = core.ObservationSettling
		}
		if fact.observations == 1 {
			projection.ObservationState = core.ObservationSettled
		}
		if fact.seal != nil {
			projection.PotentialDiscrimination = append([]core.PotentialDiscriminationPair(nil), fact.seal.PotentialDiscriminationPairs...)
		}
		if fact.seal != nil && fact.binding != nil {
			projection.RealizedDiscrimination = realizedDiscriminationAtSealCut(records, *fact.seal, *fact.binding)
		}
		out = append(out, projection)
	}
	return out, nil
}

func collectExperimentLifecycleFacts(records []core.CanonicalRecordEnvelope) (map[core.ExperimentID]*experimentLifecycleFacts, error) {
	facts := map[core.ExperimentID]*experimentLifecycleFacts{}
	for _, record := range records {
		id, kind, handled, err := experimentLifecycleRecordIdentity(record)
		if err != nil {
			return nil, err
		}
		if !handled {
			continue
		}
		if facts[id] == nil {
			facts[id] = &experimentLifecycleFacts{}
		}
		switch kind {
		case core.RecordExperiment:
			facts[id].defined = true
		case core.RecordExperimentSeal:
			facts[id].seals++
			var seal core.ExperimentSeal
			if err := json.Unmarshal(record.Body, &seal); err != nil {
				return nil, err
			}
			facts[id].seal = &seal
		case core.RecordExperimentExecutionLink:
			facts[id].links++
		case core.RecordExperimentObservationBinding:
			facts[id].observations++
			var binding core.ExperimentObservationBinding
			if err := json.Unmarshal(record.Body, &binding); err != nil {
				return nil, err
			}
			facts[id].binding = &binding
		case core.RecordExperimentClosure:
			facts[id].closures++
		case core.RecordExperimentAbort:
			facts[id].aborts++
		}
	}
	return facts, nil
}

func experimentLifecycleRecordIdentity(record core.CanonicalRecordEnvelope) (core.ExperimentID, core.RecordKind, bool, error) {
	switch record.Kind {
	case core.RecordExperiment:
		var v core.Experiment
		if err := json.Unmarshal(record.Body, &v); err != nil {
			return "", record.Kind, true, err
		}
		return v.ExperimentID, record.Kind, true, nil
	case core.RecordExperimentSeal:
		var v core.ExperimentSeal
		if err := json.Unmarshal(record.Body, &v); err != nil {
			return "", record.Kind, true, err
		}
		return v.ExperimentID, record.Kind, true, nil
	case core.RecordExperimentExecutionLink:
		var v core.ExperimentExecutionLink
		if err := json.Unmarshal(record.Body, &v); err != nil {
			return "", record.Kind, true, err
		}
		return v.ExperimentID, record.Kind, true, nil
	case core.RecordExperimentObservationBinding:
		var v core.ExperimentObservationBinding
		if err := json.Unmarshal(record.Body, &v); err != nil {
			return "", record.Kind, true, err
		}
		return v.ExperimentID, record.Kind, true, nil
	case core.RecordExperimentClosure:
		var v core.ExperimentClosure
		if err := json.Unmarshal(record.Body, &v); err != nil {
			return "", record.Kind, true, err
		}
		return v.ExperimentID, record.Kind, true, nil
	case core.RecordExperimentAbort:
		var v core.ExperimentAbort
		if err := json.Unmarshal(record.Body, &v); err != nil {
			return "", record.Kind, true, err
		}
		return v.ExperimentID, record.Kind, true, nil
	default:
		return "", record.Kind, false, nil
	}
}

func projectExperimentLifecycle(id core.ExperimentID, facts *experimentLifecycleFacts) (core.ExperimentLifecycleState, error) {
	if facts == nil || !facts.defined || facts.seals > 1 || facts.links > 1 || facts.observations > 1 || facts.closures > 1 || facts.aborts > 1 || (facts.closures > 0 && facts.aborts > 0) {
		return "", fmt.Errorf("corrupt experiment lifecycle for %s", id)
	}
	switch {
	case facts.aborts == 1:
		return core.ExperimentAborted, nil
	case facts.closures == 1:
		return core.ExperimentClosed, nil
	case facts.seals == 0 && facts.links == 0:
		return core.ExperimentDefined, nil
	case facts.seals == 1 && facts.links == 0:
		return core.ExperimentSealed, nil
	case facts.seals == 1 && facts.links == 1:
		return core.ExperimentObserving, nil
	default:
		return "", fmt.Errorf("invalid experiment lifecycle for %s", id)
	}
}

func realizedDiscriminationAtSealCut(records []core.CanonicalRecordEnvelope, seal core.ExperimentSeal, binding core.ExperimentObservationBinding) bool {
	if len(seal.PotentialDiscriminationPairs) == 0 {
		return false
	}
	predictions := map[core.CandidateID]map[string]core.PredictionID{}
	for _, record := range records {
		if record.CanonicalRecordSeq > seal.BaseProjectionCutRef.CanonicalRecordHighWater || record.Kind != core.RecordPredictionBinding {
			continue
		}
		var prediction core.PredictionBinding
		if json.Unmarshal(record.Body, &prediction) != nil || prediction.ExperimentID != seal.ExperimentID {
			continue
		}
		dimension, err := core.ObservationDimensionKey(prediction.Predicate)
		if err != nil {
			continue
		}
		if predictions[prediction.CandidateID] == nil {
			predictions[prediction.CandidateID] = map[string]core.PredictionID{}
		}
		predictions[prediction.CandidateID][dimension] = prediction.PredictionID
	}
	results := map[core.PredictionID]core.PredictionEvaluationStatus{}
	for _, result := range binding.PredictionResults {
		results[result.PredictionID] = result.Status
	}
	for _, pair := range seal.PotentialDiscriminationPairs {
		targetID := predictions[pair.TargetCandidateID][pair.DimensionKey]
		challengerID := predictions[pair.ChallengerCandidateID][pair.DimensionKey]
		target, tok := results[targetID]
		challenger, cok := results[challengerID]
		if !tok || !cok {
			continue
		}
		if (target == core.PredictionMatch && challenger == core.PredictionMismatch) || (target == core.PredictionMismatch && challenger == core.PredictionMatch) {
			return true
		}
	}
	return false
}

func candidateLineageRoot(id core.CandidateID, candidates map[core.CandidateID]core.Candidate) (core.CandidateID, error) {
	seen := map[core.CandidateID]bool{}
	current := id
	for {
		if seen[current] {
			return "", fmt.Errorf("candidate revision cycle")
		}
		seen[current] = true
		candidate, ok := candidates[current]
		if !ok {
			return "", fmt.Errorf("candidate revision parent missing")
		}
		if candidate.RevisesCandidateID == "" {
			return current, nil
		}
		current = candidate.RevisesCandidateID
	}
}

func candidateSemanticStates(candidates []core.CandidateProjection) []core.CandidateSemanticState {
	out := make([]core.CandidateSemanticState, 0, len(candidates))
	for _, candidate := range candidates {
		active := candidate.State == core.CandidateActive
		out = append(out, core.CandidateSemanticState{CandidateID: candidate.CandidateID, LineageRoot: candidate.LineageRoot, Active: active, Superseded: !active, Eligible: active})
	}
	return out
}

func (s *Service) sourceCompatibility(ctx context.Context, episode core.Episode) core.SourceGenerationCompatibility {
	ws, err := s.workspaces.Inspect(ctx, episode.WorkspaceID)
	if err != nil || ws.Validate() != nil || string(ws.RepositoryID) != episode.RepositoryID || string(ws.ID) != episode.WorkspaceID {
		return core.SourceGenerationStale
	}
	snap := s.snapshots.ObserveFresh(ctx, ws.Root)
	if snap.Quality != workspace.QualityFresh && snap.DiagnosticCode == "observation_budget_exceeded" && ctx.Err() == nil {
		snap = s.snapshots.ObserveFresh(ctx, ws.Root)
	}
	if snap.Quality != workspace.QualityFresh || snap.Validate() != nil || snap.RepositoryID != ws.RepositoryID || snap.WorkspaceID != ws.ID || snap.Generation != episode.Baseline.SourceGeneration {
		return core.SourceGenerationStale
	}
	return core.SourceGenerationCurrent
}
