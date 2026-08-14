package workspace

import (
	"strings"
	"testing"
	"time"
)

func TestSnapshotGenerationChangesOnlyWithObservedSourceFacts(t *testing.T) {
	now := time.Date(2026, 8, 13, 17, 0, 0, 0, time.UTC)
	base := FastSnapshot{
		SchemaVersion: SnapshotSchemaVersion,
		RepositoryID:  RepositoryID("repo_01K00000000000000000000000"),
		WorkspaceID:   WorkspaceID("ws_01K00000000000000000000000"),
		Head:          strings.Repeat("a", 40),
		Ref:           "refs/heads/main",
		Dirty:         DirtySummary{Digest: strings.Repeat("b", 64)},
		Quality:       QualityFresh,
		ObservedAt:    now,
	}
	first, err := WithGeneration(base)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(first.Generation, "gen_") {
		t.Fatalf("generation=%q", first.Generation)
	}

	later := base
	later.ObservedAt = now.Add(time.Minute)
	later.CacheAgeMS = 500
	later.Quality = QualityCached
	second, err := WithGeneration(later)
	if err != nil {
		t.Fatal(err)
	}
	if second.Generation != first.Generation {
		t.Fatalf("observation metadata changed generation: first=%q second=%q", first.Generation, second.Generation)
	}

	changed := base
	changed.Dirty.Modified = 1
	changed.Dirty.Dirty = true
	changed.Dirty.Digest = strings.Repeat("c", 64)
	third, err := WithGeneration(changed)
	if err != nil {
		t.Fatal(err)
	}
	if third.Generation == first.Generation {
		t.Fatal("dirty source facts did not change generation")
	}
}

func TestSnapshotQualityAndTransientFactsValidate(t *testing.T) {
	now := time.Now().UTC()
	for _, quality := range []ObservationQuality{QualityFresh, QualityCached, QualityStale, QualityUnavailable} {
		snapshot := FastSnapshot{
			SchemaVersion: SnapshotSchemaVersion,
			RepositoryID:  RepositoryID("repo_01K00000000000000000000000"),
			WorkspaceID:   WorkspaceID("ws_01K00000000000000000000000"),
			Quality:       quality,
			ObservedAt:    now,
		}
		if quality != QualityUnavailable {
			snapshot.Head = strings.Repeat("a", 40)
			snapshot.Dirty.Digest = strings.Repeat("b", 64)
			snapshot.Transient = TransientState{Merge: true, Rebase: true, CherryPick: true, Revert: true, Bisect: true}
			snapshot.Dirty.Conflicted = 2
			var err error
			snapshot, err = WithGeneration(snapshot)
			if err != nil {
				t.Fatal(err)
			}
		}
		if err := snapshot.Validate(); err != nil {
			t.Fatalf("quality %q rejected: %v", quality, err)
		}
	}
}

func TestSnapshotExactSourceSnapshotIsDistinctEvidence(t *testing.T) {
	exact := ExactSourceSnapshot{
		SchemaVersion:       ExactSnapshotSchemaVersion,
		SourceContentDigest: strings.Repeat("a", 64),
		VCSStateDigest:      strings.Repeat("b", 64),
		SourceView:          "worktree",
		Quality:             QualityFresh,
		ObservedAt:          time.Now().UTC(),
	}
	if err := exact.Validate(); err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(exact.SourceContentDigest, "gen_") {
		t.Fatal("exact source digest was conflated with fast generation")
	}
}
