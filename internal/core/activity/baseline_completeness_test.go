package activity

import (
	"testing"
	"time"

	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestBaselineFromRecordsSelectionCompleteness(t *testing.T) {
	now := time.Now().UTC()
	base := Observation{
		WorkspaceID: workspace.WorkspaceID("ws_01K00000000000000000000000"),
		Ref:         "refs/heads/main",
		Head:        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Quality:     workspace.QualityFresh,
		ObservedAt:  now,
		Paths:       []PathFact{{Path: "dirty.go", State: PathModified}},
	}
	if got := BaselineFrom(base); got.Completeness != workspace.SelectionComplete {
		t.Fatalf("complete baseline=%#v", got)
	}

	truncated := base
	truncated.PathsTruncated = true
	if got := BaselineFrom(truncated); got.Completeness != workspace.SelectionPartial {
		t.Fatalf("truncated baseline=%#v", got)
	}

	unavailable := base
	unavailable.Quality = workspace.QualityUnavailable
	if got := BaselineFrom(unavailable); got.Completeness != workspace.SelectionUnavailable {
		t.Fatalf("unavailable baseline=%#v", got)
	}

	partial := base
	partial.Completeness = workspace.SelectionPotentiallyStale
	if got := BaselineFrom(partial); got.Completeness != workspace.SelectionPotentiallyStale {
		t.Fatalf("explicit degraded baseline=%#v", got)
	}
}

func TestCompareDivergesWhenBaselinePathSetIsNotComplete(t *testing.T) {
	now := time.Now().UTC()
	base := Baseline{
		SchemaVersion: 1,
		WorkspaceID:   workspace.WorkspaceID("ws_01K00000000000000000000000"),
		Ref:           "refs/heads/main",
		Head:          "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Quality:       workspace.QualityFresh,
		ObservedAt:    now,
		Completeness:  workspace.SelectionPartial,
		Paths:         []PathFact{{Path: "dirty.go", State: PathModified}},
	}
	current := Observation{WorkspaceID: base.WorkspaceID, Ref: base.Ref, Head: base.Head, Quality: workspace.QualityFresh, ObservedAt: now.Add(time.Second)}
	got := Compare(base, current)
	if !got.BaselineDiverged || got.DivergenceReason != "evidence_unavailable" {
		t.Fatalf("comparison=%#v", got)
	}
}

func TestCompareReadsLegacyBaselineWithoutCompleteness(t *testing.T) {
	now := time.Now().UTC()
	base := Baseline{
		SchemaVersion: 1,
		WorkspaceID:   workspace.WorkspaceID("ws_01K00000000000000000000000"),
		Ref:           "refs/heads/main",
		Head:          "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Quality:       workspace.QualityFresh,
		ObservedAt:    now,
		Paths:         []PathFact{{Path: "dirty.go", State: PathModified}},
	}
	current := Observation{WorkspaceID: base.WorkspaceID, Ref: base.Ref, Head: base.Head, Quality: workspace.QualityFresh, ObservedAt: now.Add(time.Second), Paths: base.Paths}
	if got := Compare(base, current); got.BaselineDiverged {
		t.Fatalf("legacy baseline should remain readable: %#v", got)
	}
}
