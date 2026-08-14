package activity

import (
	core "github.com/maemreyo/shellbeam/internal/core/activity"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func workspaceObservationFromSample(sample workspace.DeltaSample) core.Observation {
	paths := make([]core.PathFact, 0, len(sample.Changes))
	for _, change := range sample.Changes {
		if fact, ok := activityPathFact(change); ok {
			paths = append(paths, fact)
		}
	}
	return core.Observation{
		WorkspaceID:    sample.WorkspaceID,
		Ref:            sample.Ref,
		Head:           sample.Head,
		Quality:        sampleObservationQuality(sample.Completeness),
		ObservedAt:     sample.ObservedAt,
		Paths:          paths,
		PathsTruncated: sample.Completeness != workspace.SelectionComplete || sample.RecordsObserved > len(sample.Changes),
		Completeness:   sample.Completeness,
	}
}

func activityPathFact(change workspace.ChangeRecord) (core.PathFact, bool) {
	switch change.PathTransition {
	case workspace.PathModified:
		return core.PathFact{Path: change.NewPath, State: core.PathModified}, change.NewPath != ""
	case workspace.PathAdded:
		state := core.PathAdded
		if change.Untracked {
			state = core.PathUntracked
		}
		return core.PathFact{Path: change.NewPath, State: state}, change.NewPath != ""
	case workspace.PathDeleted:
		return core.PathFact{Path: change.OldPath, State: core.PathDeleted}, change.OldPath != ""
	case workspace.PathReplaced:
		return core.PathFact{Path: change.NewPath, State: core.PathRenamed, OriginalPath: change.OldPath}, change.NewPath != "" && change.OldPath != ""
	case workspace.PathUnmerged:
		return core.PathFact{Path: change.NewPath, State: core.PathUnmerged}, change.NewPath != ""
	default:
		return core.PathFact{}, false
	}
}

func sampleObservationQuality(completeness workspace.SelectionCompleteness) workspace.ObservationQuality {
	switch completeness {
	case workspace.SelectionComplete, workspace.SelectionPartial:
		return workspace.QualityFresh
	case workspace.SelectionPotentiallyStale, workspace.SelectionDiverged:
		return workspace.QualityStale
	default:
		return workspace.QualityUnavailable
	}
}
