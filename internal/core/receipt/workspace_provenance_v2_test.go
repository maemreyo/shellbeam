package receipt

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestWorkspaceProvenanceV2CachedToUnreconciled(t *testing.T) {
	now := time.Now().UTC()
	binding := WorkspaceBinding{
		RepositoryID: workspace.RepositoryID("repo_01K00000000000000000000000"),
		WorkspaceID:  workspace.WorkspaceID("ws_01K00000000000000000000000"),
	}
	pre := WorkspaceObservationRef{
		Kind:       WorkspaceCached,
		Generation: "gen_" + strings.Repeat("a", 64),
		Quality:    workspace.QualityCached,
		ObservedAt: now,
	}
	post := WorkspaceObservationRef{Kind: WorkspaceUnreconciled, ObservationInvalidated: true}
	got := NewWorkspaceProvenanceV2(binding, pre, post, false)
	if got == nil || got.SchemaVersion != 2 || got.Binding != binding || got.Pre.Kind != WorkspaceCached || got.Post.Kind != WorkspaceUnreconciled {
		t.Fatalf("provenance=%#v", got)
	}
	if !got.PreObservedAt.IsZero() || !got.PostObservedAt.IsZero() || got.PreGeneration != "" || got.PostGeneration != "" {
		t.Fatalf("v2 invented legacy observation fields: %#v", got)
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}

	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "pre_generation") || strings.Contains(string(encoded), "pre_observed_at") {
		t.Fatalf("v2 encoded legacy fields: %s", encoded)
	}
	var roundTrip WorkspaceProvenance
	if err := json.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if err := roundTrip.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceProvenanceV2ObservedChangeRequiresFreshDistinctSamples(t *testing.T) {
	now := time.Now().UTC()
	binding := WorkspaceBinding{WorkspaceID: workspace.WorkspaceID("ws_01K00000000000000000000000")}
	freshA := WorkspaceObservationRef{Kind: WorkspaceFreshlySampled, Generation: "gen_" + strings.Repeat("a", 64), Quality: workspace.QualityFresh, ObservedAt: now}
	freshB := WorkspaceObservationRef{Kind: WorkspaceFreshlySampled, Generation: "gen_" + strings.Repeat("b", 64), Quality: workspace.QualityFresh, ObservedAt: now.Add(time.Millisecond)}
	if err := NewWorkspaceProvenanceV2(binding, freshA, freshB, true).Validate(); err != nil {
		t.Fatalf("fresh changed provenance: %v", err)
	}

	cases := []*WorkspaceProvenance{
		NewWorkspaceProvenanceV2(binding, freshA, freshA, true),
		NewWorkspaceProvenanceV2(binding, WorkspaceObservationRef{Kind: WorkspaceCached, Generation: freshA.Generation, Quality: workspace.QualityCached, ObservedAt: now}, freshB, true),
		NewWorkspaceProvenanceV2(binding, freshA, WorkspaceObservationRef{Kind: WorkspaceUnreconciled}, true),
		NewWorkspaceProvenanceV2(binding, WorkspaceObservationRef{Kind: WorkspaceFreshlySampled, Quality: workspace.QualityFresh, ObservedAt: now}, freshB, false),
		NewWorkspaceProvenanceV2(binding, WorkspaceObservationRef{Kind: WorkspaceCached}, WorkspaceObservationRef{Kind: WorkspaceUnreconciled}, false),
	}
	for i, provenance := range cases {
		if err := provenance.Validate(); err == nil {
			t.Fatalf("case %d unexpectedly valid: %#v", i, provenance)
		}
	}
}

func TestWorkspaceProvenanceLegacyV1JSONReadability(t *testing.T) {
	pre := provenanceSnapshot(t, strings.Repeat("d", 40), workspace.QualityFresh)
	post := provenanceSnapshot(t, strings.Repeat("e", 40), workspace.QualityCached)
	legacy := NewWorkspaceProvenance(pre, post)
	encoded, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	var decoded WorkspaceProvenance
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.SchemaVersion != 1 || decoded.PreGeneration != legacy.PreGeneration || decoded.PostGeneration != legacy.PostGeneration || decoded.PreQuality != legacy.PreQuality || decoded.PostQuality != legacy.PostQuality {
		t.Fatalf("decoded legacy provenance=%#v", decoded)
	}
	if err := decoded.Validate(); err != nil {
		t.Fatal(err)
	}
}
