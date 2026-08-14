package activity

import (
	"context"
	"fmt"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/activity"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestWorkspaceObservationFromSampleMapsPathFactsAndCompleteness(t *testing.T) {
	ws := workspace.WorkspaceID("ws_01K00000000000000000000000")
	sample := workspace.DeltaSample{
		SchemaVersion: workspace.DeltaSampleSchemaVersion,
		WorkspaceID:   ws,
		Freshness:     workspace.SampleFreshlySampled,
		Completeness:  workspace.SelectionComplete,
		ObservedAt:    time.Now().UTC(),
		Ref:           "refs/heads/main",
		Head:          "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Changes: []workspace.ChangeRecord{
			{PathTransition: workspace.PathModified, NewPath: "modified.go", SourceTransition: workspace.SourceBytesChanged, VCSTransition: workspace.VCSOther},
			{PathTransition: workspace.PathAdded, NewPath: "added.go", SourceTransition: workspace.SourceAvailabilityChanged, VCSTransition: workspace.VCSStaged},
			{PathTransition: workspace.PathAdded, NewPath: "untracked.go", SourceTransition: workspace.SourceAvailabilityChanged, VCSTransition: workspace.VCSOther, Untracked: true},
			{PathTransition: workspace.PathDeleted, OldPath: "deleted.go", SourceTransition: workspace.SourceAvailabilityChanged, VCSTransition: workspace.VCSOther},
			{PathTransition: workspace.PathReplaced, OldPath: "old.go", NewPath: "new.go", SourceTransition: workspace.SourceIdentityChanged, VCSTransition: workspace.VCSOther},
			{PathTransition: workspace.PathUnmerged, NewPath: "conflict.go", SourceTransition: workspace.SourceIdentityChanged, VCSTransition: workspace.VCSOther},
		},
	}
	got := workspaceObservationFromSample(sample)
	if got.WorkspaceID != ws || got.Ref != sample.Ref || got.Head != sample.Head || got.Quality != workspace.QualityFresh || got.Completeness != workspace.SelectionComplete || got.PathsTruncated {
		t.Fatalf("observation=%#v", got)
	}
	want := map[string]core.PathFact{
		"modified.go":  {Path: "modified.go", State: core.PathModified},
		"added.go":     {Path: "added.go", State: core.PathAdded},
		"untracked.go": {Path: "untracked.go", State: core.PathUntracked},
		"deleted.go":   {Path: "deleted.go", State: core.PathDeleted},
		"new.go":       {Path: "new.go", State: core.PathRenamed, OriginalPath: "old.go"},
		"conflict.go":  {Path: "conflict.go", State: core.PathUnmerged},
	}
	if len(got.Paths) != len(want) {
		t.Fatalf("paths=%#v", got.Paths)
	}
	for _, fact := range got.Paths {
		expected, ok := want[fact.Path]
		if !ok || expected != fact {
			t.Fatalf("fact=%#v want=%#v", fact, expected)
		}
	}
}

func TestWorkspaceObservationFromSamplePreservesDegradedSelection(t *testing.T) {
	ws := workspace.WorkspaceID("ws_01K00000000000000000000000")
	cases := []struct {
		completeness workspace.SelectionCompleteness
		quality      workspace.ObservationQuality
	}{
		{workspace.SelectionPartial, workspace.QualityFresh},
		{workspace.SelectionPotentiallyStale, workspace.QualityStale},
		{workspace.SelectionDiverged, workspace.QualityStale},
		{workspace.SelectionUnavailable, workspace.QualityUnavailable},
	}
	for _, tc := range cases {
		sample := workspace.DeltaSample{SchemaVersion: workspace.DeltaSampleSchemaVersion, WorkspaceID: ws, Freshness: workspace.SampleFreshlySampled, Completeness: tc.completeness, ObservedAt: time.Now().UTC()}
		got := workspaceObservationFromSample(sample)
		if got.Completeness != tc.completeness || got.Quality != tc.quality || !got.PathsTruncated {
			t.Fatalf("%s observation=%#v", tc.completeness, got)
		}
	}
}

func TestActivityWorkspaceSampleCapturedOnceAndBaselineIsBounded(t *testing.T) {
	registry := newMemoryRegistry()
	ws := workspace.WorkspaceID("ws_01K00000000000000000000000")
	changes := make([]workspace.ChangeRecord, core.MaxBaselinePathFacts+1)
	for i := range changes {
		changes[i] = workspace.ChangeRecord{PathTransition: workspace.PathModified, NewPath: fmt.Sprintf("p-%03d.go", i), SourceTransition: workspace.SourceBytesChanged, VCSTransition: workspace.VCSOther}
	}
	source := &workspaceSampleSource{sample: workspace.DeltaSample{SchemaVersion: workspace.DeltaSampleSchemaVersion, WorkspaceID: ws, Freshness: workspace.SampleFreshlySampled, Completeness: workspace.SelectionComplete, ObservedAt: time.Now().UTC(), Ref: "refs/heads/main", Head: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Changes: changes, RecordsObserved: len(changes)}}
	service := New(registry, source, 4)
	now := time.Now().UTC()
	for i := 0; i < 2; i++ {
		_, err := service.Admit(context.Background(), core.Admission{ActivityID: core.ID("activity-baseline"), OperationID: fmt.Sprintf("op-%d", i), SessionID: fmt.Sprintf("s-%d", i), WorkspaceID: ws, CWD: "/repo", ObservedAt: now.Add(time.Duration(i) * time.Second)})
		if err != nil {
			t.Fatal(err)
		}
	}
	record, found, err := registry.LoadActivity(context.Background(), core.ID("activity-baseline"))
	if err != nil || !found {
		t.Fatalf("load found=%v err=%v", found, err)
	}
	baseline, ok := record.BaselineFor(ws)
	if !ok {
		t.Fatal("baseline missing")
	}
	if source.calls != 1 || len(baseline.Paths) != core.MaxBaselinePathFacts || !baseline.PathsTruncated || baseline.Completeness != workspace.SelectionPartial {
		t.Fatalf("calls=%d baseline=%#v", source.calls, baseline)
	}
}

func TestActivityCompareWorkspaceClassifiesAndDegrades(t *testing.T) {
	registry := newMemoryRegistry()
	ws := workspace.WorkspaceID("ws_01K00000000000000000000000")
	now := time.Now().UTC()
	source := &workspaceSampleSource{sample: workspace.DeltaSample{SchemaVersion: workspace.DeltaSampleSchemaVersion, WorkspaceID: ws, Freshness: workspace.SampleFreshlySampled, Completeness: workspace.SelectionComplete, ObservedAt: now, Ref: "refs/heads/main", Head: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Changes: []workspace.ChangeRecord{
		{PathTransition: workspace.PathModified, NewPath: "inherited.go", SourceTransition: workspace.SourceBytesChanged, VCSTransition: workspace.VCSOther},
	}}}
	service := New(registry, source, 4)
	if _, err := service.Admit(context.Background(), core.Admission{ActivityID: core.ID("activity-compare"), OperationID: "op-1", SessionID: "s-1", WorkspaceID: ws, CWD: "/repo", ObservedAt: now}); err != nil {
		t.Fatal(err)
	}
	current := workspace.DeltaSample{SchemaVersion: workspace.DeltaSampleSchemaVersion, WorkspaceID: ws, Freshness: workspace.SampleFreshlySampled, Completeness: workspace.SelectionComplete, ObservedAt: now.Add(time.Second), Ref: "refs/heads/main", Head: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Changes: []workspace.ChangeRecord{
		{PathTransition: workspace.PathModified, NewPath: "inherited.go", SourceTransition: workspace.SourceBytesChanged, VCSTransition: workspace.VCSOther},
		{PathTransition: workspace.PathAdded, NewPath: "new.go", SourceTransition: workspace.SourceAvailabilityChanged, VCSTransition: workspace.VCSOther},
	}}
	comparison, err := service.CompareWorkspace(context.Background(), "activity-compare", current)
	if err != nil {
		t.Fatal(err)
	}
	if comparison.BaselineDiverged || len(comparison.InheritedDirty) != 1 || len(comparison.ObservedSinceBaseline) != 1 || len(comparison.ResolvedSinceBaseline) != 0 {
		t.Fatalf("comparison=%#v", comparison)
	}
	partial := current
	partial.Completeness = workspace.SelectionPartial
	comparison, err = service.CompareWorkspace(context.Background(), "activity-compare", partial)
	if err != nil {
		t.Fatal(err)
	}
	if !comparison.BaselineDiverged || comparison.DivergenceReason != "evidence_unavailable" {
		t.Fatalf("partial comparison=%#v", comparison)
	}
}

type workspaceSampleSource struct {
	sample workspace.DeltaSample
	calls  int
}

func (s *workspaceSampleSource) Sample(context.Context, workspace.WorkspaceID, workspace.DeltaLimits) workspace.DeltaSample {
	s.calls++
	return s.sample
}
