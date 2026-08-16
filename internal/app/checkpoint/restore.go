package checkpoint

import (
	"context"
	"errors"

	core "github.com/maemreyo/shellbeam/internal/core/checkpoint"
	"github.com/maemreyo/shellbeam/internal/core/failure"
)

func (s *Service) Restore(ctx context.Context, request core.RestoreRequest) (core.RestoreResult, error) {
	normalized, err := request.Normalize()
	if err != nil {
		return core.RestoreResult{}, err
	}
	fingerprint, err := normalized.Fingerprint()
	if err != nil {
		return core.RestoreResult{}, err
	}
	if s == nil || s.repository == nil {
		return core.RestoreResult{}, checkpointProviderUnavailable("repository_unavailable", "")
	}

	existing, _, final, err := s.repository.LoadCheckpointRestore(ctx, normalized.RestoreID)
	if err == nil {
		if existing.RequestFingerprint != fingerprint {
			return core.RestoreResult{}, checkpointRestoreConflict(normalized.RestoreID)
		}
		if final != nil {
			if err := final.Validate(); err != nil {
				return core.RestoreResult{}, err
			}
			return *final, nil
		}
	} else if !errors.Is(err, ErrRestoreNotFound) {
		return core.RestoreResult{}, err
	}

	checkpoint, err := s.repository.LoadCheckpoint(ctx, normalized.CheckpointID)
	if err != nil {
		return core.RestoreResult{}, err
	}
	if checkpoint.RetentionState == core.RetentionExpired {
		return core.RestoreResult{}, failure.New(
			failure.CheckpointExpired,
			map[string]string{"checkpoint_id": checkpoint.CheckpointID},
			nil,
		)
	}
	identity, err := s.currentProviderIdentity()
	if err != nil {
		return core.RestoreResult{}, err
	}
	if identity != checkpoint.Provider {
		return core.RestoreResult{}, checkpointProviderUnavailable("provider_identity_changed", checkpoint.Provider.ID)
	}

	reservation := RestoreReservation{
		SchemaVersion:      ReservationSchemaVersion,
		RestoreID:          normalized.RestoreID,
		RequestFingerprint: fingerprint,
		CheckpointID:       normalized.CheckpointID,
		WorkspaceID:        checkpoint.WorkspaceID,
		Paths:              append([]string(nil), normalized.Paths...),
		StartedAt:          s.now(),
	}
	winner, completed, _, err := s.repository.ReserveCheckpointRestore(ctx, reservation)
	if err != nil {
		return core.RestoreResult{}, err
	}
	if winner.RequestFingerprint != fingerprint ||
		winner.CheckpointID != normalized.CheckpointID ||
		winner.WorkspaceID != checkpoint.WorkspaceID {
		return core.RestoreResult{}, checkpointRestoreConflict(normalized.RestoreID)
	}
	if completed != nil {
		if err := completed.Validate(); err != nil {
			return core.RestoreResult{}, err
		}
		return *completed, nil
	}
	return s.resumeRestore(ctx, checkpoint, winner)
}

func (s *Service) resumeRestore(
	ctx context.Context,
	checkpoint core.Checkpoint,
	reservation RestoreReservation,
) (core.RestoreResult, error) {
	if s.workspace == nil {
		return core.RestoreResult{}, checkpointScopeInvalid("workspace_source_unavailable")
	}
	current, err := s.workspace.ResolveFresh(ctx, reservation.WorkspaceID)
	if err != nil {
		return core.RestoreResult{}, failure.New(
			failure.CheckpointScopeInvalid,
			map[string]string{"field": "workspace_id", "reason": "workspace_unavailable"},
			err,
		)
	}
	if err := validateWorkspaceContext(current, reservation.WorkspaceID); err != nil {
		return core.RestoreResult{}, checkpointScopeInvalid("fresh_source_generation_unavailable")
	}
	providerResult, err := s.provider.Restore(ctx, ProviderRestoreRequest{
		RestoreID:    reservation.RestoreID,
		CheckpointID: reservation.CheckpointID,
		WorkspaceID:  reservation.WorkspaceID,
		Root:         current.Root,
		Paths:        append([]string(nil), reservation.Paths...),
	})
	if err != nil {
		return core.RestoreResult{}, failure.New(
			failure.CheckpointProviderUnavailable,
			map[string]string{"provider": checkpoint.Provider.ID, "reason": "restore_failed"},
			err,
		)
	}
	if err := validateProviderRestoreResult(reservation.Paths, providerResult.Paths); err != nil {
		return core.RestoreResult{}, failure.New(
			failure.CheckpointProviderUnavailable,
			map[string]string{"provider": checkpoint.Provider.ID, "reason": "invalid_restore_result"},
			err,
		)
	}
	for ordinal, pathResult := range providerResult.Paths {
		if err := s.repository.RecordCheckpointRestorePath(
			ctx,
			reservation.RestoreID,
			ordinal,
			pathResult,
		); err != nil {
			return core.RestoreResult{}, err
		}
	}
	result := restoreResultFromPaths(reservation, providerResult.Paths)
	result, err = s.repository.CompleteCheckpointRestore(ctx, reservation.RestoreID, result)
	if err != nil {
		return core.RestoreResult{}, err
	}
	if restoredAny(result.Paths) {
		if err := s.workspace.InvalidateAfterMutation(ctx, reservation.WorkspaceID); err != nil {
			return core.RestoreResult{}, err
		}
	}
	return result, nil
}

func validateProviderRestoreResult(paths []string, results []core.RestorePathResult) error {
	if len(paths) != len(results) {
		return errors.New("checkpoint provider restore path count mismatch")
	}
	for i, result := range results {
		if err := result.Validate(); err != nil {
			return err
		}
		if result.Path != paths[i] {
			return errors.New("checkpoint provider restore path order mismatch")
		}
	}
	return nil
}

func restoreResultFromPaths(
	reservation RestoreReservation,
	paths []core.RestorePathResult,
) core.RestoreResult {
	complete := true
	for _, path := range paths {
		if path.Outcome != core.RestoreRestored && path.Outcome != core.RestoreNoop {
			complete = false
			break
		}
	}
	return core.RestoreResult{
		SchemaVersion: core.SchemaVersion,
		RestoreID:     reservation.RestoreID,
		CheckpointID:  reservation.CheckpointID,
		Complete:      complete,
		Paths:         append([]core.RestorePathResult(nil), paths...),
	}
}

func restoredAny(paths []core.RestorePathResult) bool {
	for _, path := range paths {
		if path.Outcome == core.RestoreRestored {
			return true
		}
	}
	return false
}

func checkpointRestoreConflict(restoreID string) error {
	return failure.New(
		failure.CheckpointRestoreRequestConflict,
		map[string]string{"restore_id": restoreID},
		nil,
	)
}
