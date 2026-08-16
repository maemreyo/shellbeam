package store

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	core "github.com/maemreyo/shellbeam/internal/core/checkpoint"
)

func TestCheckpointStoreAmbiguousAtomicWritesRecoverByCanonicalRead(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	ctx := context.Background()
	r := openCheckpointRepository(t, root)
	create := checkpointCreateReservation()

	r.writer = failNthAtomicWriter("create.dir_sync", 1)
	if got, completed, created, err := r.ReserveCheckpointCreate(ctx, create); err != nil || !created || completed != nil || !reflect.DeepEqual(got, create) {
		t.Fatalf("ambiguous create claim got=%#v completed=%#v created=%v err=%v", got, completed, created, err)
	}

	r.writer = failNthAtomicWriter("replace.dir_sync", 1)
	bound, err := r.BindCheckpointSource(ctx, create.CreateID, checkpointTestGeneration)
	if err != nil || bound.SourceGeneration != checkpointTestGeneration {
		t.Fatalf("ambiguous source bind=%#v err=%v", bound, err)
	}

	checkpoint := checkpointMetadata(bound)
	r.writer = failNthAtomicWriter("create.dir_sync", 1)
	if got, err := r.CompleteCheckpointCreate(ctx, create.CreateID, checkpoint); err != nil || !reflect.DeepEqual(got, checkpoint) {
		t.Fatalf("ambiguous metadata publish=%#v err=%v", got, err)
	}

	restore := checkpointRestoreReservation()
	r.writer = failNthAtomicWriter("create.dir_sync", 1)
	if got, completed, created, err := r.ReserveCheckpointRestore(ctx, restore); err != nil || !created || completed != nil || !reflect.DeepEqual(got, restore) {
		t.Fatalf("ambiguous restore claim got=%#v completed=%#v created=%v err=%v", got, completed, created, err)
	}

	pathResult := core.RestorePathResult{Path: restore.Paths[0], Outcome: core.RestoreRestored}
	r.writer = failNthAtomicWriter("create.dir_sync", 1)
	if err := r.RecordCheckpointRestorePath(ctx, restore.RestoreID, 0, pathResult); err != nil {
		t.Fatalf("ambiguous path publish: %v", err)
	}
	second := core.RestorePathResult{Path: restore.Paths[1], Outcome: core.RestoreNoop}
	if err := r.RecordCheckpointRestorePath(ctx, restore.RestoreID, 1, second); err != nil {
		t.Fatal(err)
	}

	final := core.RestoreResult{
		SchemaVersion: core.SchemaVersion,
		RestoreID:     restore.RestoreID,
		CheckpointID:  restore.CheckpointID,
		Paths:         []core.RestorePathResult{pathResult, second},
		Complete:      true,
	}
	r.writer = failNthAtomicWriter("replace.dir_sync", 1)
	if got, err := r.CompleteCheckpointRestore(ctx, restore.RestoreID, final); err != nil || !reflect.DeepEqual(got, final) {
		t.Fatalf("ambiguous final publish=%#v err=%v", got, err)
	}

	r = openCheckpointRepository(t, root)
	gotCreate, gotCheckpoint, found, err := r.FindCheckpointByCreateID(ctx, create.CreateID)
	if err != nil || !found || gotCheckpoint == nil || gotCreate.SourceGeneration != checkpointTestGeneration || !reflect.DeepEqual(*gotCheckpoint, checkpoint) {
		t.Fatalf("reopen create=%#v checkpoint=%#v found=%v err=%v", gotCreate, gotCheckpoint, found, err)
	}
	gotRestore, paths, gotFinal, err := r.LoadCheckpointRestore(ctx, restore.RestoreID)
	if err != nil || gotFinal == nil || !reflect.DeepEqual(gotRestore, restore) || !reflect.DeepEqual(paths, final.Paths) || !reflect.DeepEqual(*gotFinal, final) {
		t.Fatalf("reopen restore=%#v paths=%#v final=%#v err=%v", gotRestore, paths, gotFinal, err)
	}
}
