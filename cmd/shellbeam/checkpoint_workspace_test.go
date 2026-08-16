package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	workspaceapp "github.com/maemreyo/shellbeam/internal/app/workspace"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type checkpointWorkspaceLookupFake struct {
	record workspacecore.Workspace
	err    error
	calls  []string
}

func (f *checkpointWorkspaceLookupFake) Inspect(_ context.Context, id string) (workspacecore.Workspace, error) {
	f.calls = append(f.calls, id)
	if f.err != nil {
		return workspacecore.Workspace{}, f.err
	}
	return f.record, nil
}

type checkpointFreshObserverFake struct {
	snapshots []workspacecore.FastSnapshot
	calls     []string
}

func (f *checkpointFreshObserverFake) ObserveFresh(_ context.Context, root string) workspacecore.FastSnapshot {
	f.calls = append(f.calls, root)
	if len(f.snapshots) == 0 {
		return workspacecore.FastSnapshot{}
	}
	got := f.snapshots[0]
	if len(f.snapshots) > 1 {
		f.snapshots = f.snapshots[1:]
	}
	return got
}

func TestCheckpointWorkspaceSourceResolvesOnlyFreshBoundGeneration(t *testing.T) {
	record := checkpointWorkspaceRecord()
	lookup := &checkpointWorkspaceLookupFake{record: record}
	observer := &checkpointFreshObserverFake{snapshots: []workspacecore.FastSnapshot{checkpointFreshSnapshot(record, "a")}}
	coherence := workspaceapp.NewCoherenceTracker("daemon-test")
	source := newCheckpointWorkspaceSource(lookup, observer, coherence)

	got, err := source.ResolveFresh(context.Background(), string(record.ID))
	if err != nil {
		t.Fatal(err)
	}
	if got.WorkspaceID != string(record.ID) || got.RepositoryID != string(record.RepositoryID) || got.Root != record.Root || got.SourceGeneration != "gen_"+strings.Repeat("a", 64) {
		t.Fatalf("workspace context=%#v", got)
	}
	if !reflect.DeepEqual(lookup.calls, []string{string(record.ID)}) || !reflect.DeepEqual(observer.calls, []string{record.Root}) {
		t.Fatalf("lookup=%v observe=%v", lookup.calls, observer.calls)
	}
}

func TestCheckpointWorkspaceSourceRejectsStaleOrMismatchedObservation(t *testing.T) {
	record := checkpointWorkspaceRecord()
	cases := []struct {
		name   string
		mutate func(*workspacecore.FastSnapshot)
	}{
		{name: "cached", mutate: func(s *workspacecore.FastSnapshot) { s.Quality = workspacecore.QualityCached }},
		{name: "wrong workspace", mutate: func(s *workspacecore.FastSnapshot) {
			s.WorkspaceID = workspacecore.WorkspaceID("ws_01K00000000000000000000099")
		}},
		{name: "wrong repository", mutate: func(s *workspacecore.FastSnapshot) {
			s.RepositoryID = workspacecore.RepositoryID("repo_01K00000000000000000000099")
		}},
		{name: "missing generation", mutate: func(s *workspacecore.FastSnapshot) { s.Generation = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := checkpointFreshSnapshot(record, "b")
			tc.mutate(&snapshot)
			source := newCheckpointWorkspaceSource(
				&checkpointWorkspaceLookupFake{record: record},
				&checkpointFreshObserverFake{snapshots: []workspacecore.FastSnapshot{snapshot}},
				workspaceapp.NewCoherenceTracker("daemon-test"),
			)
			if _, err := source.ResolveFresh(context.Background(), string(record.ID)); err == nil {
				t.Fatal("unsafe workspace observation accepted")
			}
		})
	}
}

func TestCheckpointWorkspaceInvalidationBumpsCoherenceAndNextResolveUsesFreshTruth(t *testing.T) {
	record := checkpointWorkspaceRecord()
	lookup := &checkpointWorkspaceLookupFake{record: record}
	observer := &checkpointFreshObserverFake{snapshots: []workspacecore.FastSnapshot{
		checkpointFreshSnapshot(record, "c"),
		checkpointFreshSnapshot(record, "d"),
	}}
	coherence := workspaceapp.NewCoherenceTracker("daemon-test")
	source := newCheckpointWorkspaceSource(lookup, observer, coherence)

	before, err := source.ResolveFresh(context.Background(), string(record.ID))
	if err != nil {
		t.Fatal(err)
	}
	barrierBefore := coherence.CaptureBarrier()
	if err := source.InvalidateAfterMutation(context.Background(), string(record.ID)); err != nil {
		t.Fatal(err)
	}
	barrierAfter := coherence.CaptureBarrier()
	if barrierAfter.Epoch != barrierBefore.Epoch+1 {
		t.Fatalf("coherence epoch before=%d after=%d", barrierBefore.Epoch, barrierAfter.Epoch)
	}
	after, err := source.ResolveFresh(context.Background(), string(record.ID))
	if err != nil {
		t.Fatal(err)
	}
	if before.SourceGeneration == after.SourceGeneration || after.SourceGeneration != "gen_"+strings.Repeat("d", 64) {
		t.Fatalf("fresh generations before=%s after=%s", before.SourceGeneration, after.SourceGeneration)
	}
	if len(observer.calls) != 2 {
		t.Fatalf("fresh observations=%v", observer.calls)
	}
}

func TestCheckpointWorkspaceInvalidationRejectsUnknownWorkspace(t *testing.T) {
	source := newCheckpointWorkspaceSource(
		&checkpointWorkspaceLookupFake{err: errors.New("missing")},
		&checkpointFreshObserverFake{},
		workspaceapp.NewCoherenceTracker("daemon-test"),
	)
	if err := source.InvalidateAfterMutation(context.Background(), "ws_01K00000000000000000000000"); err == nil {
		t.Fatal("unknown workspace invalidated coherence")
	}
}

func checkpointWorkspaceRecord() workspacecore.Workspace {
	return workspacecore.Workspace{
		SchemaVersion: workspacecore.SchemaVersion,
		ID:            workspacecore.WorkspaceID("ws_01K00000000000000000000000"),
		RepositoryID:  workspacecore.RepositoryID("repo_01K00000000000000000000000"),
		Label:         "checkpoint-test",
		Root:          "/repo",
		GitDir:        "/repo/.git",
		Branch:        "main",
		CreatedAt:     time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC),
		LastSeenAt:    time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC),
	}
}

func checkpointFreshSnapshot(record workspacecore.Workspace, hexDigit string) workspacecore.FastSnapshot {
	return workspacecore.FastSnapshot{
		SchemaVersion:   workspacecore.SnapshotSchemaVersion,
		RepositoryID:    record.RepositoryID,
		WorkspaceID:     record.ID,
		Generation:      "gen_" + strings.Repeat(hexDigit, 64),
		Head:            strings.Repeat(hexDigit, 40),
		Dirty:           workspacecore.DirtySummary{Digest: strings.Repeat(hexDigit, 64)},
		Quality:         workspacecore.QualityFresh,
		UpstreamQuality: workspacecore.QualityUnavailable,
		ObservedAt:      time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC),
	}
}
