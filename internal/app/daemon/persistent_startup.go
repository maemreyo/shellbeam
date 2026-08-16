package daemon

import (
	"context"
	"errors"
	"sync"
	"time"

	persistentapp "github.com/maemreyo/shellbeam/internal/app/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	persistentcore "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

type PersistentStartupOptions struct {
	PerSession     time.Duration
	MaxConcurrency int
	TotalBudget    time.Duration
}

func (s *Service) ReconcilePersistentStartup(ctx context.Context, bindings []persistentcore.Binding, options PersistentStartupOptions) error {
	if len(bindings) == 0 {
		return nil
	}
	store, ok := s.store.(PersistentSessionStore)
	if !ok {
		return failure.New(failure.FeatureUnavailable, map[string]string{"feature": "named_sessions"}, nil)
	}
	options = normalizePersistentStartupOptions(options)
	totalCtx, cancel := context.WithTimeout(ctx, options.TotalBudget)
	defer cancel()

	jobs := make(chan persistentcore.Binding)
	errs := make(chan error, len(bindings))
	var workers sync.WaitGroup
	count := options.MaxConcurrency
	if count > len(bindings) {
		count = len(bindings)
	}
	for i := 0; i < count; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for binding := range jobs {
				if totalCtx.Err() != nil {
					errs <- s.markPersistentStartupLost(ctx, store, binding, "startup_budget_expired")
					continue
				}
				sessionCtx, cancelSession := context.WithTimeout(totalCtx, options.PerSession)
				err := s.reconcilePersistentStartupCandidate(sessionCtx, binding)
				cancelSession()
				if err == nil {
					errs <- nil
					continue
				}
				if reason, classifiable := persistentStartupLossReason(err); classifiable {
					errs <- s.markPersistentStartupLost(ctx, store, binding, reason)
					continue
				}
				errs <- err
			}
		}()
	}
	for _, binding := range bindings {
		jobs <- binding
	}
	close(jobs)
	workers.Wait()
	close(errs)

	var first error
	for err := range errs {
		if err != nil && first == nil {
			first = err
		}
	}
	return first
}

func normalizePersistentStartupOptions(options PersistentStartupOptions) PersistentStartupOptions {
	if options.PerSession <= 0 {
		options.PerSession = time.Duration(persistentcore.ReattachHandshakeTimeoutMS) * time.Millisecond
	}
	if options.MaxConcurrency <= 0 {
		options.MaxConcurrency = persistentcore.StartupReattachConcurrency
	}
	if options.TotalBudget <= 0 {
		options.TotalBudget = time.Duration(persistentcore.StartupReattachBudgetMS) * time.Millisecond
	}
	return options
}

func (s *Service) reconcilePersistentStartupCandidate(ctx context.Context, binding persistentcore.Binding) error {
	runtime, ok := s.options.PersistentRuntime.(PersistentReattachRuntime)
	if !ok {
		return failure.New(failure.FeatureUnavailable, map[string]string{"feature": "named_sessions"}, nil)
	}
	result, err := runtime.Reattach(ctx, binding)
	if err != nil {
		return err
	}
	if result.Handle == nil {
		return failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": binding.SessionID, "reason": "reattach_handle"}, nil)
	}
	closeOnError := true
	defer func() {
		if closeOnError {
			_ = result.Handle.Close()
		}
	}()
	control, ok := result.Handle.(persistentapp.RecoveryAttachment)
	if !ok {
		return failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": binding.SessionID, "reason": "reattach_control"}, nil)
	}
	reservation, err := s.store.LoadOperation(ctx, operation.ID(binding.OperationID))
	if err != nil {
		return err
	}
	if err := validatePersistentStartupReservation(binding, reservation); err != nil {
		return err
	}
	spec := persistentExecutionSpec(reservation)
	live := &liveSession{
		operationID: string(reservation.OperationID), activityID: reservation.ActivityID, sessionID: binding.SessionID,
		reservation: reservation, spec: spec, workspace: workspaceObservation{pre: receipt.WorkspaceObservationRef{Kind: receipt.WorkspaceUnreconciled}}, state: session.Running, handle: result.Handle, spawn: result.Spawn, persistent: true,
		input: session.NewInputLedger(s.options.MaxQueuedInputBytes, false), kills: session.NewKillLedger(),
		changed: make(chan struct{}), done: make(chan struct{}),
	}

	if result.State == session.Running {
		pidOwner, hasPID := result.Handle.(pidHandle)
		if !hasPID || result.PID <= 0 || pidOwner.PID() != result.PID || !result.Spawn.Attempted || !result.Spawn.Succeeded {
			return failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": binding.SessionID, "reason": "reattach_running_proof"}, nil)
		}
		s.activateLiveSession(live)
		snapshot := session.Snapshot{
			SchemaVersion: 1, OperationID: string(reservation.OperationID), SessionID: binding.SessionID,
			DaemonIncarnation: s.options.Incarnation, State: session.Running, OutputAvailable: true, UpdatedAt: time.Now().UTC(),
		}
		if stored := s.store.AdvanceSession(ctx, snapshot); stored.Err != nil {
			s.remove(binding.SessionID)
			s.endManagedShell(live)
			return stored.Err
		}
		s.startPersistentReconciliation(live)
		closeOnError = false
		return nil
	}
	if result.State.Terminal() && result.State != session.Abandoned && result.PID == 0 {
		outputStore, outputOK := s.store.(persistentOutputStore)
		bindingStore, bindingOK := s.store.(PersistentSessionStore)
		if !outputOK || !bindingOK {
			return failure.New(failure.FeatureUnavailable, map[string]string{"feature": "named_sessions"}, nil)
		}
		err := s.reconcilePersistentSession(ctx, live, control, outputStore, bindingStore)
		closeOnError = false
		_ = result.Handle.Close()
		return err
	}
	return failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": binding.SessionID, "reason": "reattach_state"}, nil)
}

func validatePersistentStartupReservation(binding persistentcore.Binding, reservation operation.Reservation) error {

	if !reservation.Persistent || reservation.SchemaVersion != 4 ||
		string(reservation.SessionID) != binding.SessionID || string(reservation.OperationID) != binding.OperationID ||
		reservation.SessionName != binding.SessionName {
		return failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": binding.SessionID, "reason": "reattach_reservation"}, nil)
	}
	return nil
}

func persistentExecutionSpec(reservation operation.Reservation) operation.ExecutionSpec {
	return operation.ExecutionSpec{
		Mode: reservation.ExecutionMode, Shell: reservation.Shell, Executable: reservation.Executable,
		Command: reservation.Command, Argv: append([]string(nil), reservation.Argv...), CWD: reservation.CWD,
		TTY: reservation.TTY, TimeoutMS: reservation.TimeoutMS,
	}
}

func (s *Service) markPersistentStartupLost(ctx context.Context, store PersistentSessionStore, binding persistentcore.Binding, reason string) error {
	result := store.AbandonPersistentSession(ctx, binding, s.options.Incarnation, reason)
	return result.Err
}

func persistentStartupLossReason(err error) (string, bool) {
	switch {
	case errors.Is(err, failure.SupervisorAuthFailed):
		return "supervisor_auth_failed", true
	case errors.Is(err, failure.SupervisorProtocolMismatch):
		return "supervisor_protocol_mismatch", true
	case errors.Is(err, failure.SupervisorStateConflict):
		public := failure.Public(err)
		if reason := public.Details["reason"]; reason != "" {
			return "supervisor_state_conflict_" + reason, true
		}
		return "supervisor_state_conflict", true
	case errors.Is(err, failure.PersistentSessionOwnershipLost):
		return "persistent_session_ownership_lost", true
	case errors.Is(err, failure.PersistentRecoveryOutputConflict):
		return "persistent_recovery_output_conflict", true
	case errors.Is(err, failure.SupervisorUnavailable):
		return "supervisor_unavailable", true
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return "supervisor_timeout", true
	case errors.Is(err, failure.FeatureUnavailable):
		return "supervisor_unavailable", true
	default:
		return "", false
	}
}
