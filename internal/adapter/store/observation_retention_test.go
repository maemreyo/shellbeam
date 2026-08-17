//go:build linux || darwin

package store

import (
	"context"
	"path/filepath"
	"testing"
)

// seedMaterializedObligations commits count obligations and advances the event
// projection over all of them, which is the state retention is allowed to act
// on.
func seedMaterializedObligations(t *testing.T, r *Repository, count int) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < count; i++ {
		prepared, result := r.PrepareObservation(ctx, observationRequest("subject:seed"))
		if result.Err != nil {
			t.Fatal(result.Err)
		}
		if commit := r.CommitObservation(ctx, prepared.Obligation.ChangeSeq); commit.Err != nil {
			t.Fatal(commit.Err)
		}
	}
	high, err := r.ObservationHighWatermark(ctx)
	if err != nil {
		t.Fatal(err)
	}
	state, err := r.LoadEventProjectionState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	state.MaterializedThroughSeq = high
	if err := r.SaveEventProjectionState(ctx, state); err != nil {
		t.Fatal(err)
	}
}

// TestCollectingObligationsKeepsTheWatermarkIntactAcrossReopen is the invariant
// the whole mechanism turns on.
//
// A reopened store recovers its high watermark from the highest obligation
// filename that survived. Collect that file and the watermark falls back below
// the event projection, and the materializer refuses to run ever again --
// permanent damage from a housekeeping pass. The newest record is therefore
// never collectible, however old it gets.
func TestCollectingObligationsKeepsTheWatermarkIntactAcrossReopen(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	repo := openObservationRepository(t, dir)
	seedMaterializedObligations(t, repo, 8)
	before, err := repo.ObservationHighWatermark(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := repo.CollectMaterializedObligations(context.Background(), ObligationRetentionPolicy{MaxDeletions: 100}); err != nil {
		t.Fatal(err)
	}

	reopened := openObservationRepository(t, dir)
	after, err := reopened.ObservationHighWatermark(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("high watermark after collection and reopen = %d, want %d", after, before)
	}
}

// TestCollectingObligationsReclaimsMaterializedRecords keeps the mechanism from
// being a no-op that still satisfies the invariant above.
func TestCollectingObligationsReclaimsMaterializedRecords(t *testing.T) {
	repo := openObservationRepository(t, filepath.Join(t.TempDir(), "state"))
	seedMaterializedObligations(t, repo, 8)

	report, err := repo.CollectMaterializedObligations(context.Background(), ObligationRetentionPolicy{MaxDeletions: 100})
	if err != nil {
		t.Fatal(err)
	}
	// Eight records, of which the newest is pinned as the watermark anchor.
	if report.Collected != 7 {
		t.Fatalf("collected = %d, want 7", report.Collected)
	}
}

// TestCollectingObligationsSparesPreparedRecords: an obligation still prepared
// has an unproven subject that reconciliation has to reason about, and it has
// not reached the event projection at all. Collecting it would discard the only
// record that a write may have happened.
func TestCollectingObligationsSparesPreparedRecords(t *testing.T) {
	ctx := context.Background()
	repo := openObservationRepository(t, filepath.Join(t.TempDir(), "state"))
	seedMaterializedObligations(t, repo, 4)
	prepared, result := repo.PrepareObservation(ctx, observationRequest("subject:pending"))
	if result.Err != nil {
		t.Fatal(result.Err)
	}

	if _, err := repo.CollectMaterializedObligations(ctx, ObligationRetentionPolicy{MaxDeletions: 100}); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.readObservation(prepared.Obligation.ChangeSeq); err != nil {
		t.Fatalf("a prepared obligation was collected: %v", err)
	}
}

// TestCollectingObligationsStopsAtTheProjection. Records the projection has not
// absorbed yet are the materializer's next input, and it demands an unbroken
// run of sequences.
func TestCollectingObligationsStopsAtTheProjection(t *testing.T) {
	ctx := context.Background()
	repo := openObservationRepository(t, filepath.Join(t.TempDir(), "state"))
	seedMaterializedObligations(t, repo, 4)
	// Four more committed records the projection has not reached.
	for i := 0; i < 4; i++ {
		prepared, result := repo.PrepareObservation(ctx, observationRequest("subject:ahead"))
		if result.Err != nil {
			t.Fatal(result.Err)
		}
		if commit := repo.CommitObservation(ctx, prepared.Obligation.ChangeSeq); commit.Err != nil {
			t.Fatal(commit.Err)
		}
	}

	report, err := repo.CollectMaterializedObligations(ctx, ObligationRetentionPolicy{MaxDeletions: 100})
	if err != nil {
		t.Fatal(err)
	}
	// Only the first four are materialized, and none of them is the newest
	// record overall, so all four go.
	if report.Collected != 4 {
		t.Fatalf("collected = %d, want 4", report.Collected)
	}
}

// TestCollectingObligationsRespectsItsBound keeps one pass from turning into an
// unbounded burst that competes with real work for the disk.
func TestCollectingObligationsRespectsItsBound(t *testing.T) {
	repo := openObservationRepository(t, filepath.Join(t.TempDir(), "state"))
	seedMaterializedObligations(t, repo, 20)

	report, err := repo.CollectMaterializedObligations(context.Background(), ObligationRetentionPolicy{MaxDeletions: 5})
	if err != nil {
		t.Fatal(err)
	}
	if report.Collected != 5 {
		t.Fatalf("collected = %d, want 5", report.Collected)
	}
	if !report.Remaining {
		t.Fatal("a bounded sweep with work left did not report it")
	}
}
