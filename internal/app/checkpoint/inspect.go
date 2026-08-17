package checkpoint

import (
	"context"
	"fmt"

	core "github.com/maemreyo/shellbeam/internal/core/checkpoint"
)

type CheckpointInspection struct {
	Checkpoint core.Checkpoint          `json:"checkpoint"`
	Provider   ProviderCheckpointStatus `json:"provider"`
}

func (s *Service) Inspect(ctx context.Context, checkpointID string) (CheckpointInspection, error) {
	if s == nil || s.repository == nil {
		return CheckpointInspection{}, checkpointProviderUnavailable("repository_unavailable", "")
	}
	checkpoint, err := s.repository.LoadCheckpoint(ctx, checkpointID)
	if err != nil {
		return CheckpointInspection{}, err
	}
	identity, err := s.currentProviderIdentity()
	if err != nil {
		return CheckpointInspection{}, err
	}
	if identity != checkpoint.Provider {
		return CheckpointInspection{
			Checkpoint: checkpoint,
			Provider: ProviderCheckpointStatus{
				CheckpointID:   checkpoint.CheckpointID,
				RetentionState: checkpoint.RetentionState,
				Available:      false,
			},
		}, nil
	}
	status, err := s.provider.Inspect(ctx, checkpoint.CheckpointID)
	if err != nil {
		return CheckpointInspection{}, err
	}
	if status.CheckpointID != checkpoint.CheckpointID {
		return CheckpointInspection{}, fmt.Errorf("checkpoint provider inspect id mismatch")
	}
	if status.RetentionState != core.RetentionAvailable && status.RetentionState != core.RetentionPartiallyCompacted && status.RetentionState != core.RetentionExpired {
		return CheckpointInspection{}, fmt.Errorf("checkpoint provider inspect retention state invalid")
	}
	if status.RetentionState == core.RetentionExpired && status.Available {
		return CheckpointInspection{}, fmt.Errorf("expired checkpoint provider state cannot be available")
	}
	return CheckpointInspection{Checkpoint: checkpoint, Provider: status}, nil
}
