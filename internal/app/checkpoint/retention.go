package checkpoint

import (
	"context"
	"fmt"

	core "github.com/maemreyo/shellbeam/internal/core/checkpoint"
)

func (s *Service) SweepRetention(ctx context.Context) (SweepResult, error) {
	if s == nil || s.repository == nil || s.provider == nil {
		return SweepResult{}, checkpointProviderUnavailable("retention_unavailable", "")
	}
	result, err := s.provider.Sweep(ctx, SweepRequest{
		Now:            s.now(),
		MaxCheckpoints: core.MaxRetainedCheckpoints,
		MaxBytes:       core.MaxPrivateProviderBytes,
		MaxAge:         core.MaxRetentionAge,
	})
	if err != nil {
		return SweepResult{}, err
	}
	if err := validateSweepResult(result); err != nil {
		return SweepResult{}, err
	}
	for _, checkpointID := range result.ExpiredCheckpointIDs {
		if _, err := s.repository.MarkCheckpointRetention(ctx, checkpointID, core.RetentionExpired); err != nil {
			return SweepResult{}, err
		}
	}
	return result, nil
}

func validateSweepResult(result SweepResult) error {
	if result.FreedBytes < 0 || len(result.ExpiredCheckpointIDs) > core.MaxRetainedCheckpoints {
		return fmt.Errorf("invalid checkpoint sweep result")
	}
	seen := make(map[string]struct{}, len(result.ExpiredCheckpointIDs))
	for _, checkpointID := range result.ExpiredCheckpointIDs {
		if checkpointID == "" {
			return fmt.Errorf("invalid checkpoint sweep result id")
		}
		if _, ok := seen[checkpointID]; ok {
			return fmt.Errorf("duplicate checkpoint sweep result id")
		}
		seen[checkpointID] = struct{}{}
	}
	return nil
}
