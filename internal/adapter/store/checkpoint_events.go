package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	checkpointapp "github.com/maemreyo/shellbeam/internal/app/checkpoint"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	core "github.com/maemreyo/shellbeam/internal/core/checkpoint"
	corefailure "github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/observation"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func (r *Repository) prepareCheckpointCreatedObservation(ctx context.Context, checkpoint core.Checkpoint) (observation.ChangeSeq, app.StoreResult) {
	request := observation.PrepareRequest{
		Kind:        observation.EventCheckpointCreated,
		Correlation: checkpointObservationCorrelation(checkpoint),
		SubjectRef:  "checkpoint:" + checkpoint.CheckpointID + ":created",
		Summary:     "checkpoint created",
	}
	return r.prepareExecutionObservation(ctx, request)
}

func (r *Repository) prepareCheckpointRestoreStartedObservation(ctx context.Context, checkpoint core.Checkpoint, reservation checkpointapp.RestoreReservation) (observation.ChangeSeq, app.StoreResult) {
	request := observation.PrepareRequest{
		Kind:        observation.EventCheckpointRestoreStarted,
		Correlation: checkpointObservationCorrelation(checkpoint),
		SubjectRef:  checkpointRestoreObservationSubject(checkpoint.CheckpointID, reservation.RestoreID, "started"),
		Summary:     "checkpoint restore started",
	}
	return r.prepareExecutionObservation(ctx, request)
}

func (r *Repository) prepareCheckpointRestoreCompletedObservation(ctx context.Context, checkpoint core.Checkpoint, result core.RestoreResult) (observation.ChangeSeq, app.StoreResult) {
	request := observation.PrepareRequest{
		Kind:        observation.EventCheckpointRestoreCompleted,
		Correlation: checkpointObservationCorrelation(checkpoint),
		SubjectRef:  checkpointRestoreObservationSubject(checkpoint.CheckpointID, result.RestoreID, "completed"),
		Summary:     "checkpoint restore completed",
	}
	return r.prepareExecutionObservation(ctx, request)
}

func (r *Repository) prepareCheckpointExpiredObservation(ctx context.Context, checkpoint core.Checkpoint) (observation.ChangeSeq, app.StoreResult) {
	request := observation.PrepareRequest{
		Kind:        observation.EventCheckpointExpired,
		Correlation: checkpointObservationCorrelation(checkpoint),
		SubjectRef:  "checkpoint:" + checkpoint.CheckpointID + ":expired",
		Summary:     "checkpoint expired",
	}
	return r.prepareExecutionObservation(ctx, request)
}

func checkpointObservationCorrelation(checkpoint core.Checkpoint) observation.Correlation {
	return observation.Correlation{
		WorkspaceID:         checkpoint.WorkspaceID,
		ActivityID:          checkpoint.ActivityID,
		WorkspaceGeneration: checkpoint.SourceGeneration,
	}
}

func checkpointRestoreObservationSubject(checkpointID, restoreID, state string) string {
	return "checkpoint:" + checkpointID + ":restore:" + restoreID + ":" + state
}

func (r *Repository) finishCheckpointObservation(seq observation.ChangeSeq, result app.StoreResult, canonicalMatches func() bool) {
	if seq == 0 {
		return
	}
	if result.Err == nil || result.Durability == app.DurableChange {
		r.commitObservationBestEffort(seq)
		return
	}
	if result.Durability == app.NoDurableChange {
		r.abortObservationBestEffort(seq, observationAbortWriteFailed)
		return
	}
	if canonicalMatches != nil && canonicalMatches() {
		r.commitObservationBestEffort(seq)
	}
}

func (r *Repository) checkpointObservationSubjectPresent(ctx context.Context, obligation observation.ObservationObligation) (bool, error) {
	parts := strings.Split(obligation.SubjectRef, ":")
	if len(parts) < 3 || parts[0] != "checkpoint" || !storeCheckpointIDPattern.MatchString(parts[1]) {
		return false, fmt.Errorf("invalid checkpoint observation subject")
	}
	checkpointID := parts[1]
	switch obligation.Kind {
	case observation.EventCheckpointCreated:
		if len(parts) != 3 || parts[2] != "created" {
			return false, fmt.Errorf("invalid checkpoint created subject")
		}
		checkpoint, err := r.LoadCheckpoint(ctx, checkpointID)
		if checkpointFailureIs(err, corefailure.CheckpointNotFound) {
			return false, nil
		}
		return err == nil && checkpoint.CheckpointID == checkpointID, err
	case observation.EventCheckpointExpired:
		if len(parts) != 3 || parts[2] != "expired" {
			return false, fmt.Errorf("invalid checkpoint expired subject")
		}
		checkpoint, err := r.LoadCheckpoint(ctx, checkpointID)
		if checkpointFailureIs(err, corefailure.CheckpointNotFound) {
			return false, nil
		}
		return err == nil && checkpoint.RetentionState == core.RetentionExpired, err
	case observation.EventCheckpointRestoreStarted, observation.EventCheckpointRestoreCompleted:
		if len(parts) != 5 || parts[2] != "restore" {
			return false, fmt.Errorf("invalid checkpoint restore observation subject")
		}
		restoreID := parts[3]
		if _, err := operation.ParseID(restoreID); err != nil {
			return false, fmt.Errorf("invalid checkpoint restore observation id")
		}
		wantState := "started"
		if obligation.Kind == observation.EventCheckpointRestoreCompleted {
			wantState = "completed"
		}
		if parts[4] != wantState {
			return false, fmt.Errorf("invalid checkpoint restore observation state")
		}
		reservation, _, final, err := r.LoadCheckpointRestore(ctx, restoreID)
		if errors.Is(err, checkpointapp.ErrRestoreNotFound) {
			return false, nil
		}
		if err != nil || reservation.CheckpointID != checkpointID {
			return false, err
		}
		if obligation.Kind == observation.EventCheckpointRestoreStarted {
			return true, nil
		}
		return final != nil && final.CheckpointID == checkpointID, nil
	default:
		return false, nil
	}
}

func checkpointFailureIs(err error, code corefailure.Code) bool {
	var typed *corefailure.Failure
	return errors.As(err, &typed) && typed.Code == code
}
