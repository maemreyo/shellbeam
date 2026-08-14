package activity

import (
	"testing"
	"time"

	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestBaselineClassifiesInheritedObservedAndResolvedPaths(t *testing.T) {
	now := time.Now().UTC()
	baseline := Baseline{SchemaVersion: 1, WorkspaceID: workspace.WorkspaceID("ws_01K00000000000000000000000"), Ref: "refs/heads/main", Head: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Quality: workspace.QualityFresh, ObservedAt: now, Paths: []PathFact{{Path: "dirty.go", State: PathModified}, {Path: "old.txt", State: PathUntracked}, {Path: "rename.txt", State: PathRenamed, OriginalPath: "before.txt"}, {Path: "conflict.go", State: PathUnmerged}}}
	current := Observation{WorkspaceID: baseline.WorkspaceID, Ref: baseline.Ref, Head: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Quality: workspace.QualityFresh, ObservedAt: now.Add(time.Minute), Paths: []PathFact{{Path: "dirty.go", State: PathModified}, {Path: "rename.txt", State: PathModified}, {Path: "new.txt", State: PathUntracked}, {Path: "conflict.go", State: PathUnmerged}}}
	got := Compare(baseline, current)
	if got.BaselineDiverged {
		t.Fatalf("comparison=%#v", got)
	}
	assertPaths(t, got.InheritedDirty, "conflict.go", "dirty.go")
	assertPaths(t, got.ObservedSinceBaseline, "new.txt", "rename.txt")
	assertPaths(t, got.ResolvedSinceBaseline, "old.txt")
}

func TestDivergedOnBranchRebaseOrUnavailableEvidence(t *testing.T) {
	now := time.Now().UTC()
	base := Baseline{SchemaVersion: 1, WorkspaceID: workspace.WorkspaceID("ws_01K00000000000000000000000"), Ref: "refs/heads/main", Head: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Quality: workspace.QualityFresh, ObservedAt: now}
	cases := []Observation{
		{WorkspaceID: base.WorkspaceID, Ref: "refs/heads/topic", Head: base.Head, Quality: workspace.QualityFresh, ObservedAt: now},
		{WorkspaceID: base.WorkspaceID, Ref: base.Ref, Head: base.Head, Quality: workspace.QualityFresh, ObservedAt: now, RebaseInProgress: true},
		{WorkspaceID: base.WorkspaceID, Ref: base.Ref, Quality: workspace.QualityUnavailable, ObservedAt: now},
		{WorkspaceID: base.WorkspaceID, Ref: base.Ref, Head: base.Head, Quality: workspace.QualityFresh, ObservedAt: now, HistoryDiverged: true},
	}
	for i, current := range cases {
		if got := Compare(base, current); !got.BaselineDiverged || got.DivergenceReason == "" {
			t.Fatalf("case %d comparison=%#v", i, got)
		}
	}
}

func assertPaths(t *testing.T, facts []PathFact, want ...string) {
	t.Helper()
	if len(facts) != len(want) {
		t.Fatalf("facts=%#v want=%v", facts, want)
	}
	for i := range want {
		if facts[i].Path != want[i] {
			t.Fatalf("facts=%#v want=%v", facts, want)
		}
	}
}

func TestBaselineValidateRejectsUnboundedPathFacts(t *testing.T) {
	now := time.Now().UTC()
	baseline := Baseline{SchemaVersion: 1, WorkspaceID: workspace.WorkspaceID("ws_01K00000000000000000000000"), Ref: "refs/heads/main", Head: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Quality: workspace.QualityFresh, ObservedAt: now}
	for i := 0; i < MaxBaselinePathFacts+1; i++ {
		baseline.Paths = append(baseline.Paths, PathFact{Path: "dirty.go", State: PathModified})
	}
	if err := baseline.Validate(); err == nil {
		t.Fatal("unbounded baseline path facts accepted")
	}
}
