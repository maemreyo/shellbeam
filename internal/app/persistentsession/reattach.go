package persistentsession

import (
	"context"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	core "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func (s *Service) Reattach(ctx context.Context, candidate core.Binding) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if s.store == nil || s.launcher == nil {
		return Result{}, failure.New(failure.FeatureUnavailable, map[string]string{"feature": "named_sessions"}, nil)
	}
	if err := candidate.Validate(); err != nil {
		return Result{}, failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": candidate.SessionID, "reason": "binding"}, err)
	}
	current, found, err := s.store.Find(ctx, operation.SessionID(candidate.SessionID))
	if err != nil {
		return Result{}, err
	}
	if !found || !sameReattachBindingIdentity(current, candidate) {
		return Result{}, failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": candidate.SessionID, "reason": "binding_changed"}, nil)
	}
	if current.Lifecycle == core.LifecycleTerminal || current.Lifecycle == core.LifecycleLost {
		return Result{}, failure.New(failure.PersistentSessionOwnershipLost, map[string]string{"session_id": current.SessionID, "session_name": current.SessionName, "reason": string(current.Lifecycle)}, nil)
	}
	reattacher, ok := s.launcher.(Reattacher)
	if !ok {
		return Result{}, failure.New(failure.FeatureUnavailable, map[string]string{"feature": "named_sessions"}, nil)
	}
	attachment, status, err := reattacher.Reattach(ctx, current)
	if err != nil {
		return Result{}, err
	}
	if attachment == nil || status.SessionID != current.SessionID || status.GenerationID != current.SupervisorGenerationID {
		if attachment != nil {
			_ = attachment.Close()
		}
		return Result{}, failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": current.SessionID, "reason": "reattach_identity"}, nil)
	}
	if status.State == session.Running {
		if status.PID <= 0 || !status.Spawn.Attempted || !status.Spawn.Succeeded {
			_ = attachment.Close()
			return Result{}, failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": current.SessionID, "reason": "reattach_running_proof"}, nil)
		}
		if current.Lifecycle == core.LifecycleProvisioning {
			live := current
			live.Lifecycle = core.LifecycleLive
			live.UpdatedAt = s.nextUpdate(current.UpdatedAt)
			if err := s.store.Advance(ctx, live); err != nil {
				_ = attachment.Close()
				return Result{}, err
			}
			current = live
		}
	} else if !status.State.Terminal() || status.State == session.Abandoned || status.PID != 0 {
		_ = attachment.Close()
		return Result{}, failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": current.SessionID, "reason": "reattach_state"}, nil)
	}
	return Result{Binding: current, Attachment: attachment, Status: status}, nil
}

func sameReattachBindingIdentity(a, b core.Binding) bool {
	return a.SchemaVersion == b.SchemaVersion &&
		a.SessionID == b.SessionID &&
		a.OperationID == b.OperationID &&
		a.ActivityID == b.ActivityID &&
		a.WorkspaceID == b.WorkspaceID &&
		a.SessionName == b.SessionName &&
		a.Persistent == b.Persistent &&
		a.Supervision == b.Supervision &&
		a.Continuity == b.Continuity &&
		a.SupervisorGenerationID == b.SupervisorGenerationID &&
		a.SupervisorEndpointRef == b.SupervisorEndpointRef &&
		a.CreatedAt.Equal(b.CreatedAt)
}
