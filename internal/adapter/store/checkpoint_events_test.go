package store

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	core "github.com/maemreyo/shellbeam/internal/core/checkpoint"
	"github.com/maemreyo/shellbeam/internal/core/observation"
)

func TestCheckpointEventsAreExactlyOnceAcrossDurableReplay(t *testing.T) {
	r := openCheckpointRepository(t, filepath.Join(t.TempDir(), "state"))
	ctx := context.Background()
	prepareCheckpointForRestore(t, r, ctx)

	assertCheckpointObligations(t, r, []observation.EventKind{observation.EventCheckpointCreated})
	create := checkpointCreateReservation()
	bound, err := r.BindCheckpointSource(ctx, create.CreateID, checkpointTestGeneration)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.CompleteCheckpointCreate(ctx, create.CreateID, checkpointMetadata(bound)); err != nil {
		t.Fatal(err)
	}
	assertCheckpointObligations(t, r, []observation.EventKind{observation.EventCheckpointCreated})

	restore := checkpointRestoreReservation()
	if _, _, _, err := r.ReserveCheckpointRestore(ctx, restore); err != nil {
		t.Fatal(err)
	}
	assertCheckpointObligations(t, r, []observation.EventKind{
		observation.EventCheckpointCreated,
		observation.EventCheckpointRestoreStarted,
	})
	if _, _, _, err := r.ReserveCheckpointRestore(ctx, restore); err != nil {
		t.Fatal(err)
	}
	assertCheckpointObligations(t, r, []observation.EventKind{
		observation.EventCheckpointCreated,
		observation.EventCheckpointRestoreStarted,
	})

	paths := []core.RestorePathResult{
		{Path: restore.Paths[0], Outcome: core.RestoreRestored},
		{Path: restore.Paths[1], Outcome: core.RestoreNoop},
	}
	for ordinal, result := range paths {
		if err := r.RecordCheckpointRestorePath(ctx, restore.RestoreID, ordinal, result); err != nil {
			t.Fatal(err)
		}
	}
	final := core.RestoreResult{SchemaVersion: core.SchemaVersion, RestoreID: restore.RestoreID, CheckpointID: restore.CheckpointID, Paths: paths, Complete: true}
	if _, err := r.CompleteCheckpointRestore(ctx, restore.RestoreID, final); err != nil {
		t.Fatal(err)
	}
	assertCheckpointObligations(t, r, []observation.EventKind{
		observation.EventCheckpointCreated,
		observation.EventCheckpointRestoreStarted,
		observation.EventCheckpointRestoreCompleted,
	})
	if _, err := r.CompleteCheckpointRestore(ctx, restore.RestoreID, final); err != nil {
		t.Fatal(err)
	}
	assertCheckpointObligations(t, r, []observation.EventKind{
		observation.EventCheckpointCreated,
		observation.EventCheckpointRestoreStarted,
		observation.EventCheckpointRestoreCompleted,
	})

	if _, err := r.MarkCheckpointRetention(ctx, restore.CheckpointID, core.RetentionExpired); err != nil {
		t.Fatal(err)
	}
	assertCheckpointObligations(t, r, []observation.EventKind{
		observation.EventCheckpointCreated,
		observation.EventCheckpointRestoreStarted,
		observation.EventCheckpointRestoreCompleted,
		observation.EventCheckpointExpired,
	})
	if _, err := r.MarkCheckpointRetention(ctx, restore.CheckpointID, core.RetentionExpired); err != nil {
		t.Fatal(err)
	}
	assertCheckpointObligations(t, r, []observation.EventKind{
		observation.EventCheckpointCreated,
		observation.EventCheckpointRestoreStarted,
		observation.EventCheckpointRestoreCompleted,
		observation.EventCheckpointExpired,
	})
}

func TestCheckpointPreparedObservationReconcilesFromCanonicalTruth(t *testing.T) {
	r := openCheckpointRepository(t, filepath.Join(t.TempDir(), "state"))
	ctx := context.Background()
	create := checkpointCreateReservation()
	if _, _, _, err := r.ReserveCheckpointCreate(ctx, create); err != nil {
		t.Fatal(err)
	}
	bound, err := r.BindCheckpointSource(ctx, create.CreateID, checkpointTestGeneration)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := checkpointMetadata(bound)
	seq, result := r.prepareCheckpointCreatedObservation(ctx, checkpoint)
	if result.Err != nil || seq == 0 {
		t.Fatalf("prepare checkpoint observation seq=%d result=%#v", seq, result)
	}
	if result := r.writer.Create(r.checkpointMetadataPath(checkpoint.CheckpointID), checkpoint); result.Err != nil {
		t.Fatal(result.Err)
	}
	if err := r.reconcilePreparedExecutionObservations(ctx); err != nil {
		t.Fatal(err)
	}
	obligations, err := r.ListObservationObligations(ctx, 0, 16)
	if err != nil || len(obligations) != 1 || obligations[0].State != observation.ObligationCommitted {
		t.Fatalf("reconciled obligations=%#v err=%v", obligations, err)
	}
}

func assertCheckpointObligations(t *testing.T, r *Repository, want []observation.EventKind) {
	t.Helper()
	got, err := r.ListObservationObligations(context.Background(), 0, 32)
	if err != nil {
		t.Fatal(err)
	}
	kinds := make([]observation.EventKind, 0, len(got))
	for _, obligation := range got {
		if obligation.State != observation.ObligationCommitted {
			t.Fatalf("checkpoint obligation not committed: %#v", obligation)
		}
		if strings.Contains(obligation.SubjectRef, "internal/") || strings.Contains(obligation.Summary, "internal/") || strings.Contains(obligation.SubjectRef, "tests/") || strings.Contains(obligation.Summary, "tests/") {
			t.Fatalf("checkpoint event leaked path: %#v", obligation)
		}
		kinds = append(kinds, obligation.Kind)
	}
	if !reflect.DeepEqual(kinds, want) {
		t.Fatalf("checkpoint event kinds=%v want=%v obligations=%#v", kinds, want, got)
	}
}

func TestCheckpointObservationCommitFailureKeepsCanonicalTruthAndRecovers(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openCheckpointRepository(t, root)
	ctx := context.Background()
	create := checkpointCreateReservation()
	if _, _, _, err := r.ReserveCheckpointCreate(ctx, create); err != nil {
		t.Fatal(err)
	}
	bound, err := r.BindCheckpointSource(ctx, create.CreateID, checkpointTestGeneration)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := checkpointMetadata(bound)
	replaceWrites := 0
	r.writer.fail = func(point string) error {
		if point == "replace.write" {
			replaceWrites++
			if replaceWrites == 1 {
				return errors.New("inject checkpoint observation commit failure")
			}
		}
		return nil
	}
	got, err := r.CompleteCheckpointCreate(ctx, create.CreateID, checkpoint)
	if err != nil || !reflect.DeepEqual(got, checkpoint) {
		t.Fatalf("canonical checkpoint=%#v err=%v writes=%d", got, err, replaceWrites)
	}
	loaded, err := r.LoadCheckpoint(ctx, checkpoint.CheckpointID)
	if err != nil || !reflect.DeepEqual(loaded, checkpoint) {
		t.Fatalf("checkpoint truth=%#v err=%v", loaded, err)
	}
	obligations, err := r.ListObservationObligations(ctx, 0, 16)
	if err != nil || len(obligations) != 1 || obligations[0].State != observation.ObligationPrepared {
		t.Fatalf("prepared checkpoint event=%#v err=%v", obligations, err)
	}

	r.writer.fail = nil
	if err := r.reconcilePreparedExecutionObservations(ctx); err != nil {
		t.Fatal(err)
	}
	obligations, err = r.ListObservationObligations(ctx, 0, 16)
	if err != nil || len(obligations) != 1 || obligations[0].State != observation.ObligationCommitted {
		t.Fatalf("recovered checkpoint event=%#v err=%v", obligations, err)
	}
}
