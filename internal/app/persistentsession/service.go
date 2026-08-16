package persistentsession

import (
	"context"
	"crypto/rand"
	"fmt"
	"slices"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	core "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/session"
	"github.com/oklog/ulid/v2"
)

type Service struct {
	store    BindingStore
	launcher Launcher
	options  Options
}

func NewService(store BindingStore, launcher Launcher, options Options) *Service {
	if options.Now == nil {
		options.Now = func() time.Time { return time.Now().UTC() }
	}
	if options.NewGeneration == nil {
		options.NewGeneration = func() string { return "supgen_" + ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String() }
	}
	if options.NewEndpointRef == nil {
		options.NewEndpointRef = func() string { return "supref_" + ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String() }
	}
	return &Service{store: store, launcher: launcher, options: options}
}

func (s *Service) Ensure(ctx context.Context, reservation operation.Reservation, spec operation.ExecutionSpec) (Result, error) {
	if err := s.validate(reservation, spec); err != nil {
		return Result{}, err
	}
	if s.store == nil || s.launcher == nil {
		return Result{}, failure.New(failure.FeatureUnavailable, map[string]string{"feature": "named_sessions"}, nil)
	}
	binding, found, err := s.store.Find(ctx, reservation.SessionID)
	if err != nil {
		return Result{}, err
	}
	if !found {
		binding = s.bindingFor(reservation)
		stored, _, reserveErr := s.store.Reserve(ctx, binding)
		if reserveErr != nil {
			if replay, replayFound, replayErr := s.store.Find(ctx, reservation.SessionID); replayErr == nil && replayFound {
				binding = replay
			} else {
				return Result{}, reserveErr
			}
		} else {
			binding = stored
		}
	}
	if err := validateBindingAgainstReservation(binding, reservation); err != nil {
		return Result{}, err
	}
	switch binding.Lifecycle {
	case core.LifecycleTerminal, core.LifecycleLost:
		return Result{}, failure.New(failure.PersistentSessionOwnershipLost, map[string]string{"session_id": binding.SessionID, "session_name": binding.SessionName, "reason": string(binding.Lifecycle)}, nil)
	case core.LifecycleProvisioning, core.LifecycleLive:
	default:
		return Result{}, failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": binding.SessionID, "reason": "lifecycle"}, nil)
	}
	attachment, status, err := s.launcher.Ensure(ctx, LaunchRequest{Binding: binding, Spec: spec, Limits: s.options.Limits})
	if err != nil {
		return Result{}, err
	}
	if attachment == nil || status.SessionID != binding.SessionID || status.GenerationID != binding.SupervisorGenerationID {
		if attachment != nil {
			_ = attachment.Close()
		}
		return Result{}, failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": binding.SessionID, "reason": "readiness_identity"}, nil)
	}
	if status.State != session.Running || !status.Spawn.Attempted || !status.Spawn.Succeeded {
		_ = attachment.Close()
		return Result{}, failure.New(failure.SupervisorUnavailable, map[string]string{"session_id": binding.SessionID, "reason": "not_ready"}, nil)
	}
	if binding.Lifecycle == core.LifecycleProvisioning {
		live := binding
		live.Lifecycle = core.LifecycleLive
		live.UpdatedAt = s.nextUpdate(binding.UpdatedAt)
		if err := s.store.Advance(ctx, live); err != nil {
			_ = attachment.Close()
			return Result{}, err
		}
		binding = live
	}
	return Result{Binding: binding, Attachment: attachment, Status: status}, nil
}

func (s *Service) validate(reservation operation.Reservation, spec operation.ExecutionSpec) error {
	if reservation.SchemaVersion != 4 || !reservation.Persistent || reservation.TTY || spec.TTY {
		return failure.New(failure.FeatureUnavailable, map[string]string{"feature": "persistent_non_tty"}, nil)
	}
	if reservation.SessionID == "" || reservation.OperationID == "" || reservation.CreatedAt.IsZero() {
		return failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": string(reservation.SessionID), "reason": "reservation"}, nil)
	}
	if err := s.options.Limits.Validate(); err != nil {
		return err
	}
	if reservation.ExecutionMode != spec.Mode || reservation.Executable != spec.Executable || reservation.CWD != spec.CWD || reservation.TTY != spec.TTY || reservation.TimeoutMS != spec.TimeoutMS {
		return failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": string(reservation.SessionID), "reason": "execution_binding"}, nil)
	}
	switch spec.Mode {
	case operation.ExecutionModeShell:
		if reservation.Command != spec.Command || reservation.Shell != spec.Shell || len(reservation.Argv) != 0 || len(spec.Argv) != 0 {
			return failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": string(reservation.SessionID), "reason": "execution_binding"}, nil)
		}
	case operation.ExecutionModeArgv:
		if !slices.Equal(reservation.Argv, spec.Argv) || reservation.Command != "" || spec.Command != "" || reservation.Shell != "" || spec.Shell != "" {
			return failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": string(reservation.SessionID), "reason": "execution_binding"}, nil)
		}
	default:
		return failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": string(reservation.SessionID), "reason": "execution_mode"}, nil)
	}
	return nil
}

func (l Limits) Validate() error {
	if l.MaxOutputBytes < 1 || l.MaxQueuedInputBytes < 1 || l.MaxInputRecords < 1 || l.MaxInputRecords > 4096 || l.MaxInputMetadataBytes < 256 || l.MaxInputMetadataBytes > 1<<20 || l.MaxKillRecords < 1 || l.MaxKillRecords > 256 || l.TerminationGrace < 0 {
		return fmt.Errorf("invalid persistent session limits")
	}
	return nil
}

func (s *Service) bindingFor(reservation operation.Reservation) core.Binding {
	return core.Binding{
		SchemaVersion: core.SchemaVersion, SessionID: string(reservation.SessionID), OperationID: string(reservation.OperationID),
		ActivityID: reservation.ActivityID, WorkspaceID: reservation.WorkspaceID, SessionName: reservation.SessionName,
		Persistent: true, Supervision: core.SupervisionPerSession, Continuity: core.ContinuityDaemonRestart,
		SupervisorGenerationID: s.options.NewGeneration(), SupervisorEndpointRef: s.options.NewEndpointRef(),
		Lifecycle: core.LifecycleProvisioning, CreatedAt: reservation.CreatedAt.UTC(), UpdatedAt: reservation.CreatedAt.UTC(),
	}
}

func (s *Service) nextUpdate(previous time.Time) time.Time {
	now := s.options.Now().UTC()
	if !now.After(previous) {
		return previous.Add(time.Nanosecond)
	}
	return now
}

func validateBindingAgainstReservation(binding core.Binding, reservation operation.Reservation) error {
	if err := binding.Validate(); err != nil {
		return failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": string(reservation.SessionID), "reason": "binding"}, err)
	}
	if binding.SessionID != string(reservation.SessionID) || binding.OperationID != string(reservation.OperationID) || binding.ActivityID != reservation.ActivityID || binding.WorkspaceID != reservation.WorkspaceID || binding.SessionName != reservation.SessionName || !binding.CreatedAt.Equal(reservation.CreatedAt.UTC()) {
		return failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": string(reservation.SessionID), "reason": "reservation_binding"}, nil)
	}
	return nil
}
