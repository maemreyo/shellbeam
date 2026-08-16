package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	checkpointapp "github.com/maemreyo/shellbeam/internal/app/checkpoint"
	core "github.com/maemreyo/shellbeam/internal/core/checkpoint"
	"github.com/maemreyo/shellbeam/internal/core/failure"
)

func TestCheckpointStoreOpenDoesNotCreateCheckpointStateUntilExplicitMutation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openCheckpointRepository(t, root)
	checkpointRoot := filepath.Join(root, "checkpoints")
	listed, err := r.ListCheckpointMetadata(context.Background())
	if err != nil || len(listed) != 0 {
		t.Fatalf("unused checkpoint metadata read: listed=%#v err=%v", listed, err)
	}
	if _, err := os.Stat(checkpointRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("checkpoint state created eagerly: err=%v", err)
	}
	if _, _, _, err := r.ReserveCheckpointCreate(context.Background(), checkpointCreateReservation()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(checkpointRoot)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		t.Fatalf("checkpoint state not created privately on explicit mutation: info=%#v err=%v", info, err)
	}
}

func TestCheckpointStoreReplayRejectsUnsafeCheckpointDirectoryAfterReopen(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openCheckpointRepository(t, root)
	if _, _, _, err := r.ReserveCheckpointCreate(context.Background(), checkpointCreateReservation()); err != nil {
		t.Fatal(err)
	}
	checkpointRoot := filepath.Join(root, "checkpoints")
	if err := os.Chmod(checkpointRoot, 0755); err != nil {
		t.Fatal(err)
	}
	r = openCheckpointRepository(t, root)
	if _, _, _, err := r.FindCheckpointByCreateID(context.Background(), "cp-create-1"); err == nil {
		t.Fatal("unsafe checkpoint parent directory accepted after reopen")
	}
}

func TestCheckpointStoreCreateClaimReplayConflictAndSourceBind(t *testing.T) {
	r := openCheckpointRepository(t, filepath.Join(t.TempDir(), "state"))
	ctx := context.Background()
	reservation := checkpointCreateReservation()

	got, completed, created, err := r.ReserveCheckpointCreate(ctx, reservation)
	if err != nil || !created || completed != nil || !reflect.DeepEqual(got, reservation) {
		t.Fatalf("first reserve got=%#v completed=%#v created=%v err=%v", got, completed, created, err)
	}
	got, completed, created, err = r.ReserveCheckpointCreate(ctx, reservation)
	if err != nil || created || completed != nil || !reflect.DeepEqual(got, reservation) {
		t.Fatalf("replay reserve got=%#v completed=%#v created=%v err=%v", got, completed, created, err)
	}

	conflict := reservation
	conflict.Paths = []string{"internal/runtime/other.go", "tests/runtime/**"}
	conflict.RequestFingerprint = checkpointCreateFingerprint(conflict)
	if _, _, created, err := r.ReserveCheckpointCreate(ctx, conflict); err == nil || created || !failureIs(err, failure.CheckpointCreateConflict) {
		t.Fatalf("conflicting reserve created=%v err=%v", created, err)
	}

	bound, err := r.BindCheckpointSource(ctx, reservation.CreateID, checkpointTestGeneration)
	if err != nil || bound.SourceGeneration != checkpointTestGeneration {
		t.Fatalf("bind source=%#v err=%v", bound, err)
	}
	replayedBound, err := r.BindCheckpointSource(ctx, reservation.CreateID, checkpointTestGeneration)
	if err != nil || !reflect.DeepEqual(replayedBound, bound) {
		t.Fatalf("replay source bind=%#v err=%v", replayedBound, err)
	}
	if _, err := r.BindCheckpointSource(ctx, reservation.CreateID, "gen_"+strings.Repeat("b", 64)); err == nil || !failureIs(err, failure.CheckpointCreateConflict) {
		t.Fatalf("source rebind err=%v", err)
	}

	checkpoint := checkpointMetadata(bound)
	stored, err := r.CompleteCheckpointCreate(ctx, reservation.CreateID, checkpoint)
	if err != nil || !reflect.DeepEqual(stored, checkpoint) {
		t.Fatalf("complete checkpoint=%#v err=%v", stored, err)
	}
	stored, err = r.CompleteCheckpointCreate(ctx, reservation.CreateID, checkpoint)
	if err != nil || !reflect.DeepEqual(stored, checkpoint) {
		t.Fatalf("replay complete=%#v err=%v", stored, err)
	}
	changed := checkpoint
	changed.TotalBytes++
	if _, err := r.CompleteCheckpointCreate(ctx, reservation.CreateID, changed); err == nil || !failureIs(err, failure.CheckpointCreateConflict) {
		t.Fatalf("changed checkpoint metadata err=%v", err)
	}

	foundReservation, foundCheckpoint, found, err := r.FindCheckpointByCreateID(ctx, reservation.CreateID)
	if err != nil || !found || foundCheckpoint == nil || !reflect.DeepEqual(foundReservation, bound) || !reflect.DeepEqual(*foundCheckpoint, checkpoint) {
		t.Fatalf("find reservation=%#v checkpoint=%#v found=%v err=%v", foundReservation, foundCheckpoint, found, err)
	}
	loaded, err := r.LoadCheckpoint(ctx, checkpoint.CheckpointID)
	if err != nil || !reflect.DeepEqual(loaded, checkpoint) {
		t.Fatalf("load checkpoint=%#v err=%v", loaded, err)
	}
	listed, err := r.ListCheckpointMetadata(ctx)
	if err != nil || len(listed) != 1 || !reflect.DeepEqual(listed[0], checkpoint) {
		t.Fatalf("listed=%#v err=%v", listed, err)
	}

	compacted, err := r.MarkCheckpointRetention(ctx, checkpoint.CheckpointID, core.RetentionPartiallyCompacted)
	if err != nil || compacted.RetentionState != core.RetentionPartiallyCompacted {
		t.Fatalf("retention update=%#v err=%v", compacted, err)
	}
	wantCompacted := checkpoint
	wantCompacted.RetentionState = core.RetentionPartiallyCompacted
	if !reflect.DeepEqual(compacted, wantCompacted) {
		t.Fatalf("retention update changed unrelated metadata: got=%#v want=%#v", compacted, wantCompacted)
	}
}

func TestCheckpointStoreRestoreClaimAndPerPathTruthAreAppendOnce(t *testing.T) {
	r := openCheckpointRepository(t, filepath.Join(t.TempDir(), "state"))
	ctx := context.Background()
	prepareCheckpointForRestore(t, r, ctx)
	restore := checkpointRestoreReservation()
	got, completed, created, err := r.ReserveCheckpointRestore(ctx, restore)
	if err != nil || !created || completed != nil || !reflect.DeepEqual(got, restore) {
		t.Fatalf("first restore reserve got=%#v completed=%#v created=%v err=%v", got, completed, created, err)
	}
	got, completed, created, err = r.ReserveCheckpointRestore(ctx, restore)
	if err != nil || created || completed != nil || !reflect.DeepEqual(got, restore) {
		t.Fatalf("restore replay got=%#v completed=%#v created=%v err=%v", got, completed, created, err)
	}
	conflict := restore
	conflict.Paths = []string{"internal/runtime/other.go", "tests/runtime/file_test.go"}
	conflict.RequestFingerprint = checkpointRestoreFingerprint(conflict)
	if _, _, created, err := r.ReserveCheckpointRestore(ctx, conflict); err == nil || created || !failureIs(err, failure.CheckpointRestoreRequestConflict) {
		t.Fatalf("conflicting restore reserve created=%v err=%v", created, err)
	}

	first := core.RestorePathResult{Path: restore.Paths[0], Outcome: core.RestoreRestored}
	if err := r.RecordCheckpointRestorePath(ctx, restore.RestoreID, 0, first); err != nil {
		t.Fatal(err)
	}
	if err := r.RecordCheckpointRestorePath(ctx, restore.RestoreID, 0, first); err != nil {
		t.Fatalf("exact path replay failed: %v", err)
	}
	changedFirst := first
	changedFirst.Outcome = core.RestoreConflict
	changedFirst.Reason = "current_state_changed"
	if err := r.RecordCheckpointRestorePath(ctx, restore.RestoreID, 0, changedFirst); err == nil || !failureIs(err, failure.CheckpointRestoreRequestConflict) {
		t.Fatalf("changed ordinal replay err=%v", err)
	}
	wrongSecond := core.RestorePathResult{Path: restore.Paths[0], Outcome: core.RestoreConflict, Reason: "current_state_changed"}
	if err := r.RecordCheckpointRestorePath(ctx, restore.RestoreID, 1, wrongSecond); err == nil || !failureIs(err, failure.CheckpointRestoreRequestConflict) {
		t.Fatalf("wrong ordinal path err=%v", err)
	}
	second := core.RestorePathResult{Path: restore.Paths[1], Outcome: core.RestoreConflict, Reason: "current_state_changed"}
	if err := r.RecordCheckpointRestorePath(ctx, restore.RestoreID, 1, second); err != nil {
		t.Fatal(err)
	}

	loadedReservation, paths, final, err := r.LoadCheckpointRestore(ctx, restore.RestoreID)
	if err != nil || final != nil || !reflect.DeepEqual(loadedReservation, restore) || !reflect.DeepEqual(paths, []core.RestorePathResult{first, second}) {
		t.Fatalf("load restore reservation=%#v paths=%#v final=%#v err=%v", loadedReservation, paths, final, err)
	}
	result := core.RestoreResult{
		SchemaVersion: core.SchemaVersion,
		RestoreID:     restore.RestoreID,
		CheckpointID:  restore.CheckpointID,
		Paths:         []core.RestorePathResult{first, second},
		Complete:      false,
	}
	storedResult, err := r.CompleteCheckpointRestore(ctx, restore.RestoreID, result)
	if err != nil || !reflect.DeepEqual(storedResult, result) {
		t.Fatalf("complete restore=%#v err=%v", storedResult, err)
	}
	storedResult, err = r.CompleteCheckpointRestore(ctx, restore.RestoreID, result)
	if err != nil || !reflect.DeepEqual(storedResult, result) {
		t.Fatalf("replay restore complete=%#v err=%v", storedResult, err)
	}
	changedResult := result
	changedResult.Paths = append([]core.RestorePathResult(nil), result.Paths...)
	changedResult.Paths[1].Reason = "different_reason"
	if _, err := r.CompleteCheckpointRestore(ctx, restore.RestoreID, changedResult); err == nil || !failureIs(err, failure.CheckpointRestoreRequestConflict) {
		t.Fatalf("changed final restore err=%v", err)
	}

	loadedReservation, paths, final, err = r.LoadCheckpointRestore(ctx, restore.RestoreID)
	if err != nil || final == nil || !reflect.DeepEqual(*final, result) || !reflect.DeepEqual(paths, result.Paths) || !reflect.DeepEqual(loadedReservation, restore) {
		t.Fatalf("final load reservation=%#v paths=%#v final=%#v err=%v", loadedReservation, paths, final, err)
	}
}

func TestCheckpointStoreStrictRecordsRejectUnknownTrailingAndCorruptJSON(t *testing.T) {
	for _, tc := range []struct {
		name string
		data string
	}{
		{name: "unknown", data: `{"schema_version":1,"unknown":true}`},
		{name: "trailing", data: `{"schema_version":1} {"extra":true}`},
		{name: "corrupt", data: `{`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := openCheckpointRepository(t, filepath.Join(t.TempDir(), "state"))
			if _, _, _, err := r.ReserveCheckpointCreate(context.Background(), checkpointCreateReservation()); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(r.root, "checkpoints", "v1", "create", "cp-create-1.json")
			if err := os.WriteFile(path, []byte(tc.data), 0600); err != nil {
				t.Fatal(err)
			}
			if _, _, _, err := r.FindCheckpointByCreateID(context.Background(), "cp-create-1"); err == nil {
				t.Fatal("unsafe checkpoint record accepted")
			}
		})
	}
}

const checkpointTestGeneration = "gen_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func prepareCheckpointForRestore(t *testing.T, r *Repository, ctx context.Context) {
	t.Helper()
	create := checkpointCreateReservation()
	if _, _, _, err := r.ReserveCheckpointCreate(ctx, create); err != nil {
		t.Fatal(err)
	}
	bound, err := r.BindCheckpointSource(ctx, create.CreateID, checkpointTestGeneration)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.CompleteCheckpointCreate(ctx, create.CreateID, checkpointMetadata(bound)); err != nil {
		t.Fatal(err)
	}
}

func checkpointCreateReservation() checkpointapp.CreateReservation {
	reservation := checkpointapp.CreateReservation{
		SchemaVersion: checkpointapp.ReservationSchemaVersion,
		CreateID:      "cp-create-1",
		CheckpointID:  "chk_01K00000000000000000000000",
		Provider:      core.ProviderIdentity{ID: "localfs", Version: 1},
		WorkspaceID:   "ws_01K00000000000000000000000",
		ActivityID:    "PI-756",
		Paths:         []string{"internal/runtime/file.go", "tests/runtime/**"},
		CreatedAt:     time.Date(2026, 8, 16, 8, 0, 0, 0, time.UTC),
	}
	reservation.RequestFingerprint = checkpointCreateFingerprint(reservation)
	return reservation
}

func checkpointRestoreReservation() checkpointapp.RestoreReservation {
	reservation := checkpointapp.RestoreReservation{
		SchemaVersion: checkpointapp.ReservationSchemaVersion,
		RestoreID:     "restore-1",
		CheckpointID:  "chk_01K00000000000000000000000",
		WorkspaceID:   "ws_01K00000000000000000000000",
		Paths:         []string{"internal/runtime/file.go", "tests/runtime/file_test.go"},
		StartedAt:     time.Date(2026, 8, 16, 8, 1, 0, 0, time.UTC),
	}
	reservation.RequestFingerprint = checkpointRestoreFingerprint(reservation)
	return reservation
}

func checkpointCreateFingerprint(reservation checkpointapp.CreateReservation) string {
	fingerprint, err := (core.CreateRequest{CreateID: reservation.CreateID, WorkspaceID: reservation.WorkspaceID, ActivityID: reservation.ActivityID, Paths: reservation.Paths}).Fingerprint()
	if err != nil {
		panic(err)
	}
	return fingerprint
}

func checkpointRestoreFingerprint(reservation checkpointapp.RestoreReservation) string {
	fingerprint, err := (core.RestoreRequest{RestoreID: reservation.RestoreID, CheckpointID: reservation.CheckpointID, Paths: reservation.Paths}).Fingerprint()
	if err != nil {
		panic(err)
	}
	return fingerprint
}

func checkpointMetadata(reservation checkpointapp.CreateReservation) core.Checkpoint {
	return core.Checkpoint{
		SchemaVersion:     core.SchemaVersion,
		CheckpointID:      reservation.CheckpointID,
		CreateID:          reservation.CreateID,
		Provider:          reservation.Provider,
		WorkspaceID:       reservation.WorkspaceID,
		ActivityID:        reservation.ActivityID,
		SourceGeneration:  reservation.SourceGeneration,
		CreatedAt:         reservation.CreatedAt,
		CapturedPathCount: 1,
		TotalBytes:        7,
		CaptureQuality:    core.CaptureComplete,
		RetentionState:    core.RetentionAvailable,
		OpaqueEntryRefs:   []string{"entry_01K00000000000000000000000"},
	}
}

func openCheckpointRepository(t *testing.T, root string) *Repository {
	t.Helper()
	r, err := Open(root, Limits{
		MaxSessions:      8,
		MaxSessionOutput: 1 << 20,
		MaxTotalState:    16 << 20,
		ControlReserve:   1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func failureIs(err error, code failure.Code) bool {
	var typed *failure.Failure
	return errors.As(err, &typed) && typed.Code == code
}
