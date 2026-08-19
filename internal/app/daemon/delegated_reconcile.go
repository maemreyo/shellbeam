package daemon

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	persistentcore "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

type DelegatedStartupOptions struct {
	PerSession     time.Duration
	MaxConcurrency int
	TotalBudget    time.Duration
}

func (s *Service) ReconcileDelegatedStartup(ctx context.Context, bindings []delegated.Binding, options DelegatedStartupOptions) error {
	if len(bindings) == 0 {
		return nil
	}
	if s.options.DelegatedRuntime == nil {
		return delegatedStartupBlocked(bindings[0], "provider_unavailable", nil)
	}
	if _, ok := s.store.(DelegatedSessionStore); !ok {
		return failure.New(failure.PersistenceUnavailable, nil, fmt.Errorf("delegated session store unavailable"))
	}
	options = normalizeDelegatedStartupOptions(options)
	totalCtx, cancel := context.WithTimeout(ctx, options.TotalBudget)
	defer cancel()
	jobs := make(chan delegated.Binding)
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
					errs <- delegatedStartupBlocked(binding, "startup_budget_expired", totalCtx.Err())
					continue
				}
				sessionCtx, cancelSession := context.WithTimeout(totalCtx, options.PerSession)
				err := s.reconcileDelegatedStartupCandidate(sessionCtx, binding)
				cancelSession()
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

func normalizeDelegatedStartupOptions(options DelegatedStartupOptions) DelegatedStartupOptions {
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

func (s *Service) reconcileDelegatedStartupCandidate(ctx context.Context, binding delegated.Binding) error {
	store := s.delegatedStore()
	if store == nil {
		return failure.New(failure.PersistenceUnavailable, nil, fmt.Errorf("delegated session store unavailable"))
	}
	reservation, err := s.store.LoadOperation(ctx, operation.ID(binding.OperationID))
	if err != nil {
		return delegatedStartupBlocked(binding, "reservation_unavailable", err)
	}
	if err := validateDelegatedStartupReservation(binding, reservation); err != nil {
		return err
	}
	ref, err := store.LoadDelegatedProviderRef(ctx, operation.SessionID(binding.SessionID))
	if err != nil {
		return delegatedStartupBlocked(binding, "provider_ref_unavailable", err)
	}
	recovery, err := store.LoadDelegatedRecoveryState(ctx, operation.SessionID(binding.SessionID))
	if err != nil {
		if errors.Is(err, failure.DelegatedReconcileBlocked) {
			return err
		}
		return delegatedStartupBlocked(binding, "mutation_recovery_unavailable", err)
	}
	baseOutput, err := store.DelegatedOutputBytes(ctx, operation.SessionID(binding.SessionID))
	if err != nil {
		return delegatedStartupBlocked(binding, "output_extent_unavailable", err)
	}
	live := &liveSession{
		operationID: string(reservation.OperationID), activityID: reservation.ActivityID, sessionID: binding.SessionID,
		reservation: reservation, spec: delegatedExecutionSpecFromReservation(reservation),
		workspace: workspaceObservation{pre: receipt.WorkspaceObservationRef{Kind: receipt.WorkspaceUnreconciled}},
		state:     session.Starting, spawn: receipt.SpawnEvidence{Attempted: binding.Lifecycle == delegated.LifecycleLive, Succeeded: binding.Lifecycle == delegated.LifecycleLive},
		accepted: recovery.NextInputOffset, delivered: recovery.NextInputOffset, outputBytes: baseOutput,
		changed: make(chan struct{}), done: make(chan struct{}), delegated: true, delegatedRef: ref, delegatedBinding: binding,
		delegatedObserverBase: baseOutput, delegatedCaptureGap: true,
	}
	result, err := delegatedapp.New(s.options.DelegatedRuntime).ReattachBound(ctx, delegatedapp.ReattachRequest{Binding: binding, ProviderRef: ref, Output: delegatedRecoverySink{service: s, live: live}})
	if err != nil {
		if errors.Is(err, failure.DelegatedProviderLost) {
			return s.publishDelegatedStartupLost(live, binding, err)
		}
		_ = s.detachDelegatedRuntime(context.Background(), ref)
		if errors.Is(err, failure.DelegatedReconcileBlocked) {
			return err
		}
		return delegatedStartupBlocked(binding, "provider_reconcile_unproven", err)
	}
	live.spawn = receipt.SpawnEvidence{Attempted: true, Succeeded: true}
	if result.Observation.Terminal {
		snapshot := delegatedTerminalSnapshot{accepted: live.accepted, delivered: live.delivered, outputBytes: live.outputBytes, observerBase: live.delegatedObserverBase, captureGap: true, binding: binding}
		decision := classifyDelegatedTerminal(snapshot, result.Observation, nil)
		rec := s.delegatedTerminalReceipt(live, snapshot, decision)
		s.publishDelegatedTerminal(live, binding, decision, rec)
		return nil
	}
	if binding.Lifecycle == delegated.LifecycleProvisioning {
		binding.Lifecycle = delegated.LifecycleLive
		binding.UpdatedAt = time.Now().UTC()
		if !binding.UpdatedAt.After(binding.CreatedAt) {
			binding.UpdatedAt = binding.CreatedAt.Add(time.Nanosecond)
		}
		if got := store.AdvanceDelegatedBinding(ctx, binding); got.Err != nil {
			_ = s.detachDelegatedRuntime(context.Background(), ref)
			return got.Err
		}
		live.delegatedBinding = binding
	}
	live.state = session.Running
	s.activateLiveSession(live)
	if got := s.store.AdvanceSession(ctx, session.Snapshot{SchemaVersion: 1, OperationID: live.operationID, SessionID: live.sessionID, DaemonIncarnation: s.options.Incarnation, State: session.Running, OutputBytes: live.outputBytes, OutputAvailable: true, UpdatedAt: time.Now().UTC()}); got.Err != nil {
		s.remove(live.sessionID)
		s.endManagedShell(live)
		_ = s.detachDelegatedRuntime(context.Background(), ref)
		return got.Err
	}
	s.startDelegatedWait(live)
	return nil
}

type delegatedRecoverySink struct {
	service *Service
	live    *liveSession
}

func (sink delegatedRecoverySink) Append(b []byte) error {
	if sink.service == nil || sink.live == nil {
		return fmt.Errorf("delegated recovery sink unavailable")
	}
	n, result := sink.service.store.AppendOutput(context.Background(), operation.SessionID(sink.live.sessionID), b)
	if result.Err != nil {
		return result.Err
	}
	sink.live.mu.Lock()
	sink.live.outputBytes += int64(n)
	sink.live.notify()
	sink.live.mu.Unlock()
	return nil
}

func (s *Service) publishDelegatedStartupLost(live *liveSession, binding delegated.Binding, cause error) error {
	snapshot := delegatedTerminalSnapshot{accepted: live.accepted, delivered: live.delivered, outputBytes: live.outputBytes, observerBase: live.delegatedObserverBase, captureGap: true, binding: binding}
	decision := classifyDelegatedTerminal(snapshot, delegatedapp.Observation{}, cause)
	rec := s.delegatedTerminalReceipt(live, snapshot, decision)
	s.publishDelegatedTerminal(live, binding, decision, rec)
	return nil
}

func validateDelegatedStartupReservation(binding delegated.Binding, reservation operation.Reservation) error {
	if reservation.SchemaVersion != 5 || reservation.SessionMode != delegated.ModeDelegatedInteractive || string(reservation.SessionID) != binding.SessionID || string(reservation.OperationID) != binding.OperationID || reservation.AuthorityEpoch != 1 || binding.AuthorityEpoch < 1 {
		return delegatedStartupBlocked(binding, "reattach_reservation", nil)
	}
	return nil
}

func delegatedExecutionSpecFromReservation(reservation operation.Reservation) operation.ExecutionSpec {
	return operation.ExecutionSpec{Mode: reservation.ExecutionMode, Shell: reservation.Shell, Executable: reservation.Executable, Command: reservation.Command, Argv: append([]string(nil), reservation.Argv...), CWD: reservation.CWD, TTY: false, TimeoutMS: reservation.TimeoutMS, StdinMode: operation.StdinModeStream}
}

func delegatedStartupBlocked(binding delegated.Binding, reason string, cause error) error {
	return failure.New(failure.DelegatedReconcileBlocked, map[string]string{"session_id": binding.SessionID, "provider_id": binding.ProviderID, "current_epoch": fmt.Sprint(binding.AuthorityEpoch), "reason": reason}, cause)
}

func (s *Service) detachDelegatedRuntime(ctx context.Context, ref delegated.ProviderRef) error {
	runtime, ok := s.options.DelegatedRuntime.(delegatedapp.Detacher)
	if !ok {
		return failure.New(failure.FeatureUnavailable, map[string]string{"feature": "delegated_interactive", "reason": "detach_unavailable"}, nil)
	}
	return runtime.Detach(ctx, ref)
}
