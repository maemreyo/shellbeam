package checkpoint

import (
	"context"
	"fmt"
	"path/filepath"

	core "github.com/maemreyo/shellbeam/internal/core/checkpoint"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func (s *Service) Create(ctx context.Context, request core.CreateRequest) (core.Checkpoint, error) {
	normalized, err := request.Normalize()
	if err != nil {
		return core.Checkpoint{}, err
	}
	fingerprint, err := normalized.Fingerprint()
	if err != nil {
		return core.Checkpoint{}, err
	}
	if s == nil || s.repository == nil {
		return core.Checkpoint{}, checkpointProviderUnavailable("repository_unavailable", "")
	}

	existing, completed, found, err := s.repository.FindCheckpointByCreateID(ctx, normalized.CreateID)
	if err != nil {
		return core.Checkpoint{}, err
	}
	if found {
		if existing.RequestFingerprint != fingerprint {
			return core.Checkpoint{}, checkpointCreateConflict(normalized.CreateID)
		}
		if completed != nil {
			if err := validateCompletedReplay(existing, *completed); err != nil {
				return core.Checkpoint{}, err
			}
			return *completed, nil
		}
		return s.resumeCreate(ctx, normalized, existing, nil)
	}

	identity, err := s.currentProviderIdentity()
	if err != nil {
		return core.Checkpoint{}, err
	}
	checkpointID := s.newCheckpointID()
	reservation := CreateReservation{
		SchemaVersion:      ReservationSchemaVersion,
		CreateID:           normalized.CreateID,
		RequestFingerprint: fingerprint,
		CheckpointID:       checkpointID,
		Provider:           identity,
		WorkspaceID:        normalized.WorkspaceID,
		ActivityID:         normalized.ActivityID,
		Paths:              append([]string(nil), normalized.Paths...),
		CreatedAt:          s.now(),
	}
	winner, completed, _, err := s.repository.ReserveCheckpointCreate(ctx, reservation)
	if err != nil {
		return core.Checkpoint{}, err
	}
	if winner.RequestFingerprint != fingerprint {
		return core.Checkpoint{}, checkpointCreateConflict(normalized.CreateID)
	}
	if completed != nil {
		if err := validateCompletedReplay(winner, *completed); err != nil {
			return core.Checkpoint{}, err
		}
		return *completed, nil
	}
	return s.resumeCreate(ctx, normalized, winner, &identity)
}

func (s *Service) resumeCreate(ctx context.Context, request core.CreateRequest, reservation CreateReservation, frozenIdentity *core.ProviderIdentity) (core.Checkpoint, error) {
	identity := reservation.Provider
	var err error
	if frozenIdentity != nil {
		identity = *frozenIdentity
	} else {
		identity, err = s.currentProviderIdentity()
		if err != nil {
			return core.Checkpoint{}, err
		}
	}
	if identity != reservation.Provider {
		return core.Checkpoint{}, checkpointProviderUnavailable("provider_identity_changed", reservation.Provider.ID)
	}
	if s.workspace == nil {
		return core.Checkpoint{}, checkpointScopeInvalid("workspace_source_unavailable")
	}
	workspace, err := s.workspace.ResolveFresh(ctx, reservation.WorkspaceID)
	if err != nil {
		return core.Checkpoint{}, failure.New(failure.CheckpointScopeInvalid, map[string]string{"field": "workspace_id", "reason": "workspace_unavailable"}, err)
	}
	if err := validateWorkspaceContext(workspace, reservation.WorkspaceID); err != nil {
		return core.Checkpoint{}, checkpointScopeInvalid("fresh_source_generation_unavailable")
	}
	if reservation.SourceGeneration != "" && reservation.SourceGeneration != workspace.SourceGeneration {
		return core.Checkpoint{}, checkpointCreateConflict(reservation.CreateID)
	}
	if reservation.SourceGeneration == "" {
		reservation, err = s.repository.BindCheckpointSource(ctx, reservation.CreateID, workspace.SourceGeneration)
		if err != nil {
			return core.Checkpoint{}, err
		}
	}
	capture, err := s.provider.Capture(ctx, CaptureRequest{
		CheckpointID:     reservation.CheckpointID,
		WorkspaceID:      reservation.WorkspaceID,
		RepositoryID:     workspace.RepositoryID,
		ActivityID:       reservation.ActivityID,
		Root:             workspace.Root,
		SourceGeneration: reservation.SourceGeneration,
		Paths:            append([]string(nil), reservation.Paths...),
	})
	if err != nil {
		return core.Checkpoint{}, failure.New(failure.CheckpointProviderUnavailable, map[string]string{"provider": reservation.Provider.ID, "reason": "capture_failed"}, err)
	}
	checkpoint, err := checkpointFromCapture(reservation, capture)
	if err != nil {
		return core.Checkpoint{}, err
	}
	return s.repository.CompleteCheckpointCreate(ctx, reservation.CreateID, checkpoint)
}

func (s *Service) currentProviderIdentity() (core.ProviderIdentity, error) {
	if s == nil || s.provider == nil {
		return core.ProviderIdentity{}, checkpointProviderUnavailable("provider_unavailable", "")
	}
	identity := s.provider.Identity()
	if err := identity.Validate(); err != nil {
		return core.ProviderIdentity{}, checkpointProviderUnavailable("provider_identity_invalid", "")
	}
	return identity, nil
}

func validateWorkspaceContext(current WorkspaceContext, wantWorkspaceID string) error {
	if current.WorkspaceID != wantWorkspaceID || current.SourceGeneration == "" || !validGeneration(current.SourceGeneration) || !filepath.IsAbs(current.Root) {
		return fmt.Errorf("invalid fresh checkpoint workspace context")
	}
	if _, err := workspacecore.ParseWorkspaceID(current.WorkspaceID); err != nil {
		return err
	}
	if _, err := workspacecore.ParseRepositoryID(current.RepositoryID); err != nil {
		return err
	}
	return nil
}

func checkpointFromCapture(reservation CreateReservation, capture CaptureResult) (core.Checkpoint, error) {
	if capture.CapturedPathCount < 0 || capture.CapturedPathCount > core.MaxCapturedEntries {
		return core.Checkpoint{}, checkpointBudgetExceeded("captured_path_count", core.MaxCapturedEntries)
	}
	if capture.TotalBytes < 0 || capture.TotalBytes > core.MaxCheckpointBytes {
		return core.Checkpoint{}, checkpointBudgetExceeded("total_bytes", core.MaxCheckpointBytes)
	}
	if len(capture.OpaqueEntryRefs) > core.MaxPublicEntryRefs {
		return core.Checkpoint{}, checkpointBudgetExceeded("opaque_entry_refs", core.MaxPublicEntryRefs)
	}
	if len(capture.Excluded)+len(capture.Unsupported) > core.MaxPublicSummaries {
		return core.Checkpoint{}, checkpointBudgetExceeded("path_summaries", core.MaxPublicSummaries)
	}
	if len(capture.Unsupported) > 0 {
		return core.Checkpoint{}, failure.New(failure.CheckpointPathUnsupported, map[string]string{"path": capture.Unsupported[0].Path, "reason": capture.Unsupported[0].Reason}, nil)
	}
	checkpoint := core.Checkpoint{
		SchemaVersion:     core.SchemaVersion,
		CheckpointID:      reservation.CheckpointID,
		CreateID:          reservation.CreateID,
		Provider:          reservation.Provider,
		WorkspaceID:       reservation.WorkspaceID,
		ActivityID:        reservation.ActivityID,
		SourceGeneration:  reservation.SourceGeneration,
		CreatedAt:         reservation.CreatedAt,
		CapturedPathCount: capture.CapturedPathCount,
		Excluded:          append([]core.PathSummary(nil), capture.Excluded...),
		Unsupported:       append([]core.PathSummary(nil), capture.Unsupported...),
		TotalBytes:        capture.TotalBytes,
		CaptureQuality:    capture.CaptureQuality,
		RetentionState:    core.RetentionAvailable,
		OpaqueEntryRefs:   append([]string(nil), capture.OpaqueEntryRefs...),
	}
	if err := checkpoint.Validate(); err != nil {
		return core.Checkpoint{}, failure.New(failure.CheckpointProviderUnavailable, map[string]string{"provider": reservation.Provider.ID, "reason": "invalid_capture_result"}, err)
	}
	return checkpoint, nil
}

func validateCompletedReplay(reservation CreateReservation, checkpoint core.Checkpoint) error {
	if err := checkpoint.Validate(); err != nil {
		return err
	}
	if checkpoint.CheckpointID != reservation.CheckpointID || checkpoint.CreateID != reservation.CreateID || checkpoint.Provider != reservation.Provider || checkpoint.WorkspaceID != reservation.WorkspaceID || checkpoint.ActivityID != reservation.ActivityID || checkpoint.SourceGeneration != reservation.SourceGeneration || !checkpoint.CreatedAt.Equal(reservation.CreatedAt) {
		return checkpointCreateConflict(reservation.CreateID)
	}
	return nil
}

func checkpointProviderUnavailable(reason, provider string) error {
	details := map[string]string{"reason": reason}
	if provider != "" {
		details["provider"] = provider
	}
	return failure.New(failure.CheckpointProviderUnavailable, details, nil)
}

func checkpointScopeInvalid(reason string) error {
	return failure.New(failure.CheckpointScopeInvalid, map[string]string{"field": "workspace_id", "reason": reason}, nil)
}

func checkpointCreateConflict(createID string) error {
	return failure.New(failure.CheckpointCreateConflict, map[string]string{"checkpoint_create_id": createID}, nil)
}

func checkpointBudgetExceeded(field string, limit any) error {
	return failure.New(failure.CheckpointBudgetExceeded, map[string]string{"field": field, "reason": "provider_result", "limit": fmt.Sprint(limit)}, nil)
}
