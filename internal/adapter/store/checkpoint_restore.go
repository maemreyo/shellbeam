package store

import (
	"context"
	"errors"
	"reflect"

	checkpointapp "github.com/maemreyo/shellbeam/internal/app/checkpoint"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	core "github.com/maemreyo/shellbeam/internal/core/checkpoint"
	"github.com/maemreyo/shellbeam/internal/core/failure"
)

func (r *Repository) ReserveCheckpointRestore(ctx context.Context, reservation checkpointapp.RestoreReservation) (checkpointapp.RestoreReservation, *core.RestoreResult, bool, error) {
	if err := ctx.Err(); err != nil {
		return checkpointapp.RestoreReservation{}, nil, false, err
	}
	if err := reservation.Validate(); err != nil {
		return checkpointapp.RestoreReservation{}, nil, false, err
	}
	r.checkpointMu.Lock()
	defer r.checkpointMu.Unlock()

	checkpoint, err := r.readCheckpointMetadataUnlocked(reservation.CheckpointID)
	if errors.Is(err, ErrNotFound) {
		return checkpointapp.RestoreReservation{}, nil, false, failure.New(failure.CheckpointNotFound, map[string]string{"checkpoint_id": reservation.CheckpointID}, nil)
	}
	if err != nil {
		return checkpointapp.RestoreReservation{}, nil, false, err
	}
	if checkpoint.RetentionState == core.RetentionExpired {
		return checkpointapp.RestoreReservation{}, nil, false, failure.New(failure.CheckpointExpired, map[string]string{"checkpoint_id": reservation.CheckpointID}, nil)
	}
	if checkpoint.WorkspaceID != reservation.WorkspaceID {
		return checkpointapp.RestoreReservation{}, nil, false, checkpointRestoreRequestConflict(reservation.RestoreID)
	}

	current, err := r.readCheckpointRestoreReservationUnlocked(reservation.RestoreID)
	if err == nil {
		if current.RequestFingerprint != reservation.RequestFingerprint {
			return checkpointapp.RestoreReservation{}, nil, false, checkpointRestoreRequestConflict(reservation.RestoreID)
		}
		final, err := optionalRestoreResult(r.readCheckpointRestoreResultUnlocked(current.RestoreID))
		return current, final, false, err
	}
	if !errors.Is(err, ErrNotFound) {
		return checkpointapp.RestoreReservation{}, nil, false, err
	}
	if err := ensurePrivateDir(r.checkpointRestoreDir(reservation.RestoreID)); err != nil {
		return checkpointapp.RestoreReservation{}, nil, false, err
	}
	if err := ensurePrivateDir(r.checkpointRestorePathsDir(reservation.RestoreID)); err != nil {
		return checkpointapp.RestoreReservation{}, nil, false, err
	}

	r.observationVisibilityMu.Lock()
	defer r.observationVisibilityMu.Unlock()
	seq, prepared := r.prepareCheckpointRestoreStartedObservation(ctx, checkpoint, reservation)
	if prepared.Err != nil {
		return checkpointapp.RestoreReservation{}, nil, false, prepared.Err
	}
	result := r.writer.Create(r.checkpointRestoreReservationPath(reservation.RestoreID), reservation)
	r.finishCheckpointObservation(seq, result, func() bool {
		canonical, readErr := r.readCheckpointRestoreReservationUnlocked(reservation.RestoreID)
		return readErr == nil && reflect.DeepEqual(canonical, reservation)
	})
	if result.Err == nil {
		return reservation, nil, true, nil
	}
	current, readErr := r.readCheckpointRestoreReservationUnlocked(reservation.RestoreID)
	if readErr == nil {
		if current.RequestFingerprint != reservation.RequestFingerprint {
			return checkpointapp.RestoreReservation{}, nil, false, checkpointRestoreRequestConflict(reservation.RestoreID)
		}
		final, finalErr := optionalRestoreResult(r.readCheckpointRestoreResultUnlocked(current.RestoreID))
		if finalErr != nil {
			return checkpointapp.RestoreReservation{}, nil, false, finalErr
		}
		return current, final, result.Durability == app.AmbiguousChange, nil
	}
	return checkpointapp.RestoreReservation{}, nil, false, result.Err
}

func (r *Repository) RecordCheckpointRestorePath(ctx context.Context, restoreID string, ordinal int, pathResult core.RestorePathResult) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := pathResult.Validate(); err != nil {
		return err
	}
	r.checkpointMu.Lock()
	defer r.checkpointMu.Unlock()

	reservation, err := r.readCheckpointRestoreReservationUnlocked(restoreID)
	if err != nil {
		return err
	}
	if ordinal < 0 || ordinal >= len(reservation.Paths) || reservation.Paths[ordinal] != pathResult.Path {
		return checkpointRestoreRequestConflict(restoreID)
	}
	if current, err := r.readCheckpointRestorePathUnlocked(restoreID, ordinal); err == nil {
		if reflect.DeepEqual(current, pathResult) {
			return nil
		}
		return checkpointRestoreRequestConflict(restoreID)
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}

	result := r.writer.Create(r.checkpointRestorePathResultPath(restoreID, ordinal), pathResult)
	if result.Err == nil {
		return nil
	}
	canonical, readErr := r.readCheckpointRestorePathUnlocked(restoreID, ordinal)
	if readErr == nil {
		if reflect.DeepEqual(canonical, pathResult) {
			return nil
		}
		return checkpointRestoreRequestConflict(restoreID)
	}
	return result.Err
}

func (r *Repository) CompleteCheckpointRestore(ctx context.Context, restoreID string, result core.RestoreResult) (core.RestoreResult, error) {
	if err := ctx.Err(); err != nil {
		return core.RestoreResult{}, err
	}
	if err := result.Validate(); err != nil {
		return core.RestoreResult{}, err
	}
	r.checkpointMu.Lock()
	defer r.checkpointMu.Unlock()

	reservation, err := r.readCheckpointRestoreReservationUnlocked(restoreID)
	if err != nil {
		return core.RestoreResult{}, err
	}
	if result.RestoreID != reservation.RestoreID || result.CheckpointID != reservation.CheckpointID {
		return core.RestoreResult{}, checkpointRestoreRequestConflict(restoreID)
	}
	if current, err := r.readCheckpointRestoreResultUnlocked(restoreID); err == nil {
		if reflect.DeepEqual(current, result) {
			return current, nil
		}
		return core.RestoreResult{}, checkpointRestoreRequestConflict(restoreID)
	} else if !errors.Is(err, ErrNotFound) {
		return core.RestoreResult{}, err
	}
	paths, err := r.loadCheckpointRestorePathsUnlocked(reservation)
	if err != nil {
		return core.RestoreResult{}, err
	}
	if len(paths) != len(reservation.Paths) || !reflect.DeepEqual(paths, result.Paths) {
		return core.RestoreResult{}, failure.New(failure.CheckpointRestorePartial, map[string]string{
			"restore_id": restoreID, "checkpoint_id": reservation.CheckpointID,
		}, nil)
	}
	checkpoint, err := r.readCheckpointMetadataUnlocked(reservation.CheckpointID)
	if err != nil {
		return core.RestoreResult{}, err
	}
	r.observationVisibilityMu.Lock()
	defer r.observationVisibilityMu.Unlock()
	seq, prepared := r.prepareCheckpointRestoreCompletedObservation(ctx, checkpoint, result)
	if prepared.Err != nil {
		return core.RestoreResult{}, prepared.Err
	}
	write := r.writer.Replace(r.checkpointRestoreResultPath(restoreID), result)
	r.finishCheckpointObservation(seq, write, func() bool {
		canonical, readErr := r.readCheckpointRestoreResultUnlocked(restoreID)
		return readErr == nil && reflect.DeepEqual(canonical, result)
	})
	if write.Err == nil {
		return result, nil
	}
	canonical, readErr := r.readCheckpointRestoreResultUnlocked(restoreID)
	if readErr == nil {
		if reflect.DeepEqual(canonical, result) {
			return canonical, nil
		}
		return core.RestoreResult{}, checkpointRestoreRequestConflict(restoreID)
	}
	return core.RestoreResult{}, write.Err
}

func (r *Repository) LoadCheckpointRestore(ctx context.Context, restoreID string) (checkpointapp.RestoreReservation, []core.RestorePathResult, *core.RestoreResult, error) {
	if err := ctx.Err(); err != nil {
		return checkpointapp.RestoreReservation{}, nil, nil, err
	}
	r.checkpointMu.Lock()
	defer r.checkpointMu.Unlock()

	reservation, err := r.readCheckpointRestoreReservationUnlocked(restoreID)
	if errors.Is(err, ErrNotFound) {
		return checkpointapp.RestoreReservation{}, nil, nil, checkpointapp.ErrRestoreNotFound
	}
	if err != nil {
		return checkpointapp.RestoreReservation{}, nil, nil, err
	}
	paths, err := r.loadCheckpointRestorePathsUnlocked(reservation)
	if err != nil {
		return checkpointapp.RestoreReservation{}, nil, nil, err
	}
	final, err := optionalRestoreResult(r.readCheckpointRestoreResultUnlocked(restoreID))
	if err != nil {
		return checkpointapp.RestoreReservation{}, nil, nil, err
	}
	return reservation, paths, final, nil
}

func (r *Repository) loadCheckpointRestorePathsUnlocked(reservation checkpointapp.RestoreReservation) ([]core.RestorePathResult, error) {
	out := make([]core.RestorePathResult, 0, len(reservation.Paths))
	for ordinal, path := range reservation.Paths {
		result, err := r.readCheckpointRestorePathUnlocked(reservation.RestoreID, ordinal)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if result.Path != path {
			return nil, checkpointRestoreRequestConflict(reservation.RestoreID)
		}
		out = append(out, result)
	}
	return out, nil
}

func optionalRestoreResult(value core.RestoreResult, err error) (*core.RestoreResult, error) {
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	copy := value
	return &copy, nil
}

func checkpointRestoreRequestConflict(restoreID string) error {
	return failure.New(failure.CheckpointRestoreRequestConflict, map[string]string{"restore_id": restoreID}, nil)
}
