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
		return core.DecisionProjection{}, fmt.Errorf("decision episode unavailable")
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
			return core.DecisionProjection{}, fmt.Errorf("requested candidate unavailable")
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
	return core.DecisionProjection{EpisodeID: episode.EpisodeID, EpisodeState: state, EpisodeKind: episode.EpisodeKind, PolicyBinding: episode.PolicyBinding, CandidateID: requested, Candidates: projections}, nil
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
	if snap.Quality != workspace.QualityFresh || snap.Validate() != nil || snap.RepositoryID != ws.RepositoryID || snap.WorkspaceID != ws.ID || snap.Generation != episode.Baseline.SourceGeneration {
		return core.SourceGenerationStale
	}
	return core.SourceGenerationCurrent
}
