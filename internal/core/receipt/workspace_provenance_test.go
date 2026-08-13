package receipt

import (
	"strings"
	"testing"
	"time"

	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestWorkspaceProvenanceRecordsPrePostGenerationAndObservedChange(t *testing.T) {
	pre := provenanceSnapshot(t, strings.Repeat("a", 40), workspace.QualityFresh)
	post := provenanceSnapshot(t, strings.Repeat("b", 40), workspace.QualityCached)
	got := NewWorkspaceProvenance(pre, post)
	if got == nil || got.SchemaVersion != 1 {
		t.Fatalf("provenance=%#v", got)
	}
	if got.RepositoryID != pre.RepositoryID || got.WorkspaceID != pre.WorkspaceID {
		t.Fatalf("identity=%#v", got)
	}
	if got.PreGeneration != pre.Generation || got.PostGeneration != post.Generation || !got.ObservedChange {
		t.Fatalf("provenance=%#v", got)
	}
	if got.PreQuality != workspace.QualityFresh || got.PostQuality != workspace.QualityCached {
		t.Fatalf("quality=%#v", got)
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceProvenanceUnavailableDoesNotInventChange(t *testing.T) {
	now := time.Now().UTC()
	pre := workspace.FastSnapshot{SchemaVersion: workspace.SnapshotSchemaVersion, Quality: workspace.QualityUnavailable, ObservedAt: now, DiagnosticCode: "workspace_unregistered"}
	post := workspace.FastSnapshot{SchemaVersion: workspace.SnapshotSchemaVersion, Quality: workspace.QualityUnavailable, ObservedAt: now.Add(time.Millisecond), DiagnosticCode: "workspace_unregistered"}
	got := NewWorkspaceProvenance(pre, post)
	if got == nil || got.ObservedChange || got.PreGeneration != "" || got.PostGeneration != "" {
		t.Fatalf("provenance=%#v", got)
	}
	if got.PreQuality != workspace.QualityUnavailable || got.PostQuality != workspace.QualityUnavailable {
		t.Fatalf("provenance=%#v", got)
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
}

func provenanceSnapshot(t *testing.T, head string, quality workspace.ObservationQuality) workspace.FastSnapshot {
	t.Helper()
	now := time.Now().UTC()
	snapshot := workspace.FastSnapshot{
		SchemaVersion: workspace.SnapshotSchemaVersion,
		RepositoryID:  workspace.RepositoryID("repo_01K00000000000000000000000"),
		WorkspaceID:   workspace.WorkspaceID("ws_01K00000000000000000000000"),
		Head:          head,
		Ref:           "refs/heads/main",
		Dirty:         workspace.DirtySummary{Digest: strings.Repeat("c", 64)},
		Quality:       quality,
		ObservedAt:    now,
	}
	if quality == workspace.QualityCached {
		snapshot.CacheAgeMS = 10
	}
	got, err := workspace.WithGeneration(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
