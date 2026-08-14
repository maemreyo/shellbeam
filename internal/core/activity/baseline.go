package activity

import (
	"fmt"
	"sort"
	"time"

	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

const MaxBaselinePathFacts = 256

type PathState string

const (
	PathModified  PathState = "modified"
	PathAdded     PathState = "added"
	PathDeleted   PathState = "deleted"
	PathRenamed   PathState = "renamed"
	PathUnmerged  PathState = "unmerged"
	PathUntracked PathState = "untracked"
)

type PathFact struct {
	Path         string    `json:"path"`
	State        PathState `json:"state"`
	OriginalPath string    `json:"original_path,omitempty"`
}

type Baseline struct {
	SchemaVersion  int                             `json:"schema_version"`
	WorkspaceID    workspace.WorkspaceID           `json:"workspace_id"`
	Ref            string                          `json:"ref,omitempty"`
	Head           string                          `json:"head,omitempty"`
	Quality        workspace.ObservationQuality    `json:"quality"`
	ObservedAt     time.Time                       `json:"observed_at"`
	Paths          []PathFact                      `json:"paths,omitempty"`
	PathsTruncated bool                            `json:"paths_truncated"`
	Completeness   workspace.SelectionCompleteness `json:"completeness,omitempty"`
}

type Observation struct {
	WorkspaceID      workspace.WorkspaceID
	Ref              string
	Head             string
	Quality          workspace.ObservationQuality
	ObservedAt       time.Time
	Paths            []PathFact
	PathsTruncated   bool
	Completeness     workspace.SelectionCompleteness
	RebaseInProgress bool
	HistoryDiverged  bool
}

type Comparison struct {
	InheritedDirty        []PathFact `json:"inherited_dirty,omitempty"`
	ObservedSinceBaseline []PathFact `json:"observed_since_baseline,omitempty"`
	ResolvedSinceBaseline []PathFact `json:"resolved_since_baseline,omitempty"`
	BaselineDiverged      bool       `json:"baseline_diverged"`
	DivergenceReason      string     `json:"divergence_reason,omitempty"`
}

func BaselineFrom(observation Observation) Baseline {
	paths := normalizedFacts(observation.Paths)
	truncated := observation.PathsTruncated
	if len(paths) > MaxBaselinePathFacts {
		paths = paths[:MaxBaselinePathFacts]
		truncated = true
	}
	completeness := observationCompleteness(observation.Quality, observation.Completeness, truncated)
	return Baseline{
		SchemaVersion: SchemaVersion, WorkspaceID: observation.WorkspaceID, Ref: observation.Ref,
		Head: observation.Head, Quality: observation.Quality, ObservedAt: observation.ObservedAt.UTC(),
		Paths: paths, PathsTruncated: truncated, Completeness: completeness,
	}
}

func (b Baseline) Validate() error {
	if b.SchemaVersion != SchemaVersion || b.ObservedAt.IsZero() {
		return fmt.Errorf("invalid activity baseline metadata")
	}
	if _, err := workspace.ParseWorkspaceID(string(b.WorkspaceID)); err != nil {
		return err
	}
	if len(b.Paths) > MaxBaselinePathFacts {
		return fmt.Errorf("baseline path facts exceed limit")
	}
	if b.Completeness != "" {
		if err := b.Completeness.Validate(); err != nil {
			return err
		}
		if b.Completeness == workspace.SelectionComplete && b.PathsTruncated {
			return fmt.Errorf("complete baseline cannot be truncated")
		}
	}
	if b.Quality == workspace.QualityUnavailable {
		if b.Completeness == workspace.SelectionComplete {
			return fmt.Errorf("unavailable baseline cannot be complete")
		}
		return nil
	}
	if b.Quality != workspace.QualityFresh && b.Quality != workspace.QualityCached && b.Quality != workspace.QualityStale {
		return fmt.Errorf("invalid baseline quality")
	}
	for _, fact := range b.Paths {
		if err := fact.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (f PathFact) Validate() error {
	if f.Path == "" || f.Path == "." || f.Path == ".." {
		return fmt.Errorf("invalid dirty path fact")
	}
	switch f.State {
	case PathModified, PathAdded, PathDeleted, PathUnmerged, PathUntracked:
		if f.OriginalPath != "" {
			return fmt.Errorf("original path only valid for rename")
		}
	case PathRenamed:
		if f.OriginalPath == "" {
			return fmt.Errorf("rename original path missing")
		}
	default:
		return fmt.Errorf("invalid dirty path state")
	}
	return nil
}

func Compare(baseline Baseline, current Observation) Comparison {
	if reason := divergenceReason(baseline, current); reason != "" {
		return Comparison{BaselineDiverged: true, DivergenceReason: reason}
	}
	base := factsByPath(baseline.Paths)
	now := factsByPath(current.Paths)
	var result Comparison
	for path, before := range base {
		after, exists := now[path]
		switch {
		case !exists:
			result.ResolvedSinceBaseline = append(result.ResolvedSinceBaseline, before)
		case factsEqual(before, after):
			result.InheritedDirty = append(result.InheritedDirty, after)
		default:
			result.ObservedSinceBaseline = append(result.ObservedSinceBaseline, after)
		}
	}
	for path, after := range now {
		if _, existed := base[path]; !existed {
			result.ObservedSinceBaseline = append(result.ObservedSinceBaseline, after)
		}
	}
	sortFacts(result.InheritedDirty)
	sortFacts(result.ObservedSinceBaseline)
	sortFacts(result.ResolvedSinceBaseline)
	return result
}

func divergenceReason(baseline Baseline, current Observation) string {
	if baselineCompleteness(baseline) != workspace.SelectionComplete || observationCompleteness(current.Quality, current.Completeness, current.PathsTruncated) != workspace.SelectionComplete {
		return "evidence_unavailable"
	}
	if baseline.WorkspaceID != current.WorkspaceID {
		return "workspace_changed"
	}
	if baseline.Ref != current.Ref {
		return "branch_changed"
	}
	if current.RebaseInProgress {
		return "rebase_in_progress"
	}
	if current.HistoryDiverged {
		return "history_diverged"
	}
	return ""
}

func baselineCompleteness(baseline Baseline) workspace.SelectionCompleteness {
	return observationCompleteness(baseline.Quality, baseline.Completeness, baseline.PathsTruncated)
}

func observationCompleteness(quality workspace.ObservationQuality, declared workspace.SelectionCompleteness, truncated bool) workspace.SelectionCompleteness {
	if quality == workspace.QualityUnavailable {
		return workspace.SelectionUnavailable
	}
	if declared != "" {
		if err := declared.Validate(); err != nil {
			return workspace.SelectionUnavailable
		}
		if truncated && declared == workspace.SelectionComplete {
			return workspace.SelectionPartial
		}
		return declared
	}
	if truncated {
		return workspace.SelectionPartial
	}
	return workspace.SelectionComplete
}

func normalizedFacts(facts []PathFact) []PathFact {
	out := append([]PathFact(nil), facts...)
	sortFacts(out)
	return out
}
func factsByPath(facts []PathFact) map[string]PathFact {
	out := make(map[string]PathFact, len(facts))
	for _, fact := range facts {
		out[fact.Path] = fact
	}
	return out
}
func factsEqual(a, b PathFact) bool {
	return a.Path == b.Path && a.State == b.State && a.OriginalPath == b.OriginalPath
}
func sortFacts(facts []PathFact) {
	sort.Slice(facts, func(i, j int) bool { return facts[i].Path < facts[j].Path })
}
