package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	checkpointapp "github.com/maemreyo/shellbeam/internal/app/checkpoint"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	core "github.com/maemreyo/shellbeam/internal/core/checkpoint"
	"github.com/maemreyo/shellbeam/internal/core/failure"
)

var _ checkpointapp.Repository = (*Repository)(nil)

func (r *Repository) ReserveCheckpointCreate(ctx context.Context, reservation checkpointapp.CreateReservation) (checkpointapp.CreateReservation, *core.Checkpoint, bool, error) {
	if err := ctx.Err(); err != nil {
		return checkpointapp.CreateReservation{}, nil, false, err
	}
	if err := reservation.Validate(); err != nil {
		return checkpointapp.CreateReservation{}, nil, false, err
	}
	r.checkpointMu.Lock()
	defer r.checkpointMu.Unlock()
	if err := r.initCheckpointStore(); err != nil {
		return checkpointapp.CreateReservation{}, nil, false, err
	}

	current, err := r.readCheckpointCreateUnlocked(reservation.CreateID)
	if err == nil {
		if current.RequestFingerprint != reservation.RequestFingerprint {
			return checkpointapp.CreateReservation{}, nil, false, checkpointCreateConflict(reservation.CreateID)
		}
		completed, err := optionalCheckpoint(r.readCheckpointMetadataUnlocked(current.CheckpointID))
		return current, completed, false, err
	}
	if !errors.Is(err, ErrNotFound) {
		return checkpointapp.CreateReservation{}, nil, false, err
	}
	if _, err := r.readCheckpointMetadataUnlocked(reservation.CheckpointID); err == nil {
		return checkpointapp.CreateReservation{}, nil, false, checkpointCreateConflict(reservation.CreateID)
	} else if !errors.Is(err, ErrNotFound) {
		return checkpointapp.CreateReservation{}, nil, false, err
	}

	result := r.writer.Create(r.checkpointCreatePath(reservation.CreateID), reservation)
	if result.Err == nil {
		return reservation, nil, true, nil
	}
	current, readErr := r.readCheckpointCreateUnlocked(reservation.CreateID)
	if readErr == nil {
		if current.RequestFingerprint != reservation.RequestFingerprint {
			return checkpointapp.CreateReservation{}, nil, false, checkpointCreateConflict(reservation.CreateID)
		}
		completed, completeErr := optionalCheckpoint(r.readCheckpointMetadataUnlocked(current.CheckpointID))
		if completeErr != nil {
			return checkpointapp.CreateReservation{}, nil, false, completeErr
		}
		return current, completed, result.Durability == app.AmbiguousChange, nil
	}
	return checkpointapp.CreateReservation{}, nil, false, result.Err
}

func (r *Repository) BindCheckpointSource(ctx context.Context, createID, generation string) (checkpointapp.CreateReservation, error) {
	if err := ctx.Err(); err != nil {
		return checkpointapp.CreateReservation{}, err
	}
	r.checkpointMu.Lock()
	defer r.checkpointMu.Unlock()

	current, err := r.readCheckpointCreateUnlocked(createID)
	if err != nil {
		return checkpointapp.CreateReservation{}, err
	}
	if current.SourceGeneration == generation && generation != "" {
		return current, nil
	}
	if current.SourceGeneration != "" {
		return checkpointapp.CreateReservation{}, checkpointCreateConflict(createID)
	}
	candidate := current
	candidate.SourceGeneration = generation
	if err := candidate.Validate(); err != nil {
		return checkpointapp.CreateReservation{}, err
	}
	result := r.writer.Replace(r.checkpointCreatePath(createID), candidate)
	if result.Err == nil {
		return candidate, nil
	}
	canonical, readErr := r.readCheckpointCreateUnlocked(createID)
	if readErr == nil && reflect.DeepEqual(canonical, candidate) {
		return canonical, nil
	}
	return checkpointapp.CreateReservation{}, result.Err
}

func (r *Repository) CompleteCheckpointCreate(ctx context.Context, createID string, checkpoint core.Checkpoint) (core.Checkpoint, error) {
	if err := ctx.Err(); err != nil {
		return core.Checkpoint{}, err
	}
	r.checkpointMu.Lock()
	defer r.checkpointMu.Unlock()

	reservation, err := r.readCheckpointCreateUnlocked(createID)
	if err != nil {
		return core.Checkpoint{}, err
	}
	if err := validateCheckpointCompletion(reservation, checkpoint); err != nil {
		return core.Checkpoint{}, err
	}
	if current, err := r.readCheckpointMetadataUnlocked(reservation.CheckpointID); err == nil {
		if reflect.DeepEqual(current, checkpoint) {
			return current, nil
		}
		return core.Checkpoint{}, checkpointCreateConflict(createID)
	} else if !errors.Is(err, ErrNotFound) {
		return core.Checkpoint{}, err
	}

	result := r.writer.Create(r.checkpointMetadataPath(checkpoint.CheckpointID), checkpoint)
	if result.Err == nil {
		return checkpoint, nil
	}
	canonical, readErr := r.readCheckpointMetadataUnlocked(checkpoint.CheckpointID)
	if readErr == nil {
		if reflect.DeepEqual(canonical, checkpoint) {
			return canonical, nil
		}
		return core.Checkpoint{}, checkpointCreateConflict(createID)
	}
	return core.Checkpoint{}, result.Err
}

func (r *Repository) FindCheckpointByCreateID(ctx context.Context, createID string) (checkpointapp.CreateReservation, *core.Checkpoint, bool, error) {
	if err := ctx.Err(); err != nil {
		return checkpointapp.CreateReservation{}, nil, false, err
	}
	r.checkpointMu.Lock()
	defer r.checkpointMu.Unlock()

	reservation, err := r.readCheckpointCreateUnlocked(createID)
	if errors.Is(err, ErrNotFound) {
		return checkpointapp.CreateReservation{}, nil, false, nil
	}
	if err != nil {
		return checkpointapp.CreateReservation{}, nil, false, err
	}
	completed, err := optionalCheckpoint(r.readCheckpointMetadataUnlocked(reservation.CheckpointID))
	if err != nil {
		return checkpointapp.CreateReservation{}, nil, false, err
	}
	return reservation, completed, true, nil
}

func (r *Repository) LoadCheckpoint(ctx context.Context, checkpointID string) (core.Checkpoint, error) {
	if err := ctx.Err(); err != nil {
		return core.Checkpoint{}, err
	}
	r.checkpointMu.Lock()
	defer r.checkpointMu.Unlock()

	checkpoint, err := r.readCheckpointMetadataUnlocked(checkpointID)
	if errors.Is(err, ErrNotFound) {
		return core.Checkpoint{}, failure.New(failure.CheckpointNotFound, map[string]string{"checkpoint_id": checkpointID}, nil)
	}
	return checkpoint, err
}

func (r *Repository) ListCheckpointMetadata(ctx context.Context) ([]core.Checkpoint, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.checkpointMu.Lock()
	defer r.checkpointMu.Unlock()
	return r.listCheckpointMetadataUnlocked()
}

func (r *Repository) MarkCheckpointRetention(ctx context.Context, checkpointID string, target core.RetentionState) (core.Checkpoint, error) {
	if err := ctx.Err(); err != nil {
		return core.Checkpoint{}, err
	}
	r.checkpointMu.Lock()
	defer r.checkpointMu.Unlock()

	current, err := r.readCheckpointMetadataUnlocked(checkpointID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return core.Checkpoint{}, failure.New(failure.CheckpointNotFound, map[string]string{"checkpoint_id": checkpointID}, nil)
		}
		return core.Checkpoint{}, err
	}
	if current.RetentionState == target {
		return current, nil
	}
	if !validRetentionTransition(current.RetentionState, target) {
		return core.Checkpoint{}, fmt.Errorf("invalid checkpoint retention transition")
	}
	candidate := current
	candidate.RetentionState = target
	if err := candidate.Validate(); err != nil {
		return core.Checkpoint{}, err
	}
	result := r.writer.Replace(r.checkpointMetadataPath(checkpointID), candidate)
	if result.Err == nil {
		return candidate, nil
	}
	canonical, readErr := r.readCheckpointMetadataUnlocked(checkpointID)
	if readErr == nil && reflect.DeepEqual(canonical, candidate) {
		return canonical, nil
	}
	return core.Checkpoint{}, result.Err
}

func validateCheckpointCompletion(reservation checkpointapp.CreateReservation, checkpoint core.Checkpoint) error {
	if err := checkpoint.Validate(); err != nil {
		return err
	}
	if reservation.SourceGeneration == "" ||
		checkpoint.CheckpointID != reservation.CheckpointID ||
		checkpoint.CreateID != reservation.CreateID ||
		checkpoint.Provider != reservation.Provider ||
		checkpoint.WorkspaceID != reservation.WorkspaceID ||
		checkpoint.ActivityID != reservation.ActivityID ||
		checkpoint.SourceGeneration != reservation.SourceGeneration ||
		!checkpoint.CreatedAt.Equal(reservation.CreatedAt) ||
		checkpoint.RetentionState != core.RetentionAvailable {
		return checkpointCreateConflict(reservation.CreateID)
	}
	return nil
}

func validRetentionTransition(from, to core.RetentionState) bool {
	switch from {
	case core.RetentionAvailable:
		return to == core.RetentionPartiallyCompacted || to == core.RetentionExpired
	case core.RetentionPartiallyCompacted:
		return to == core.RetentionExpired
	case core.RetentionExpired:
		return false
	default:
		return false
	}
}

func checkpointCreateConflict(createID string) error {
	return failure.New(failure.CheckpointCreateConflict, map[string]string{"checkpoint_create_id": createID}, nil)
}
