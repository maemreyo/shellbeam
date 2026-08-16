package daemon

import (
	"context"
	"time"

	persistentapp "github.com/maemreyo/shellbeam/internal/app/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	persistentcore "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

const persistentReconcileChunkBytes = 32 * 1024

func (s *Service) startPersistentReconciliation(live *liveSession) {
	control, ok := live.handle.(persistentapp.RecoveryAttachment)
	if !ok {
		return
	}
	outputStore, ok := s.store.(persistentOutputStore)
	if !ok {
		return
	}
	bindingStore, ok := s.store.(PersistentSessionStore)
	if !ok {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	live.mu.Lock()
	live.persistentCancel = cancel
	live.persistentReconcileDone = done
	live.mu.Unlock()
	go func() {
		defer close(done)
		_ = s.reconcilePersistentSession(ctx, live, control, outputStore, bindingStore)
	}()
}

func (s *Service) reconcilePersistentSession(ctx context.Context, live *liveSession, control persistentapp.RecoveryAttachment, outputStore persistentOutputStore, bindingStore PersistentSessionStore) error {
	binding, err := bindingStore.LoadPersistentBinding(ctx, operation.SessionID(live.sessionID))
	if err != nil {
		return err
	}
	status, err := control.Status(ctx)
	if err != nil {
		terminal, terminalErr := control.Terminal(ctx)
		if terminalErr != nil {
			return err
		}
		return s.finishPersistentTerminal(ctx, live, control, outputStore, bindingStore, binding, terminal)
	}
	if err := validatePersistentStatus(live, binding, status); err != nil {
		return err
	}
	for {
		if err := s.reconcilePersistentOutput(ctx, live, control, outputStore, status.OutputAcknowledged, status.OutputBytes); err != nil {
			return err
		}
		s.mirrorPersistentStatus(live, status)
		if status.State.Terminal() {
			terminal, err := control.Terminal(ctx)
			if err != nil {
				return err
			}
			return s.finishPersistentTerminal(ctx, live, control, outputStore, bindingStore, binding, terminal)
		}
		next, err := control.WaitStatus(ctx, status.Change, 1000)
		if err != nil {
			terminal, terminalErr := control.Terminal(ctx)
			if terminalErr != nil {
				return err
			}
			return s.finishPersistentTerminal(ctx, live, control, outputStore, bindingStore, binding, terminal)
		}
		if err := validatePersistentStatus(live, binding, next); err != nil {
			return err
		}
		status = next
	}
}

func (s *Service) reconcilePersistentOutput(ctx context.Context, live *liveSession, control persistentapp.ControlAttachment, outputStore persistentOutputStore, start, target int64) error {
	if start < 0 || target < start {
		return failure.New(failure.PersistentRecoveryOutputConflict, map[string]string{"session_id": live.sessionID, "reason": "invalid_extent"}, nil)
	}
	offset := start
	for offset < target {
		maxBytes := persistentReconcileChunkBytes
		if remaining := target - offset; remaining < int64(maxBytes) {
			maxBytes = int(remaining)
		}
		data, next, extent, err := control.ReadOutput(ctx, offset, maxBytes)
		if err != nil {
			return err
		}
		if next <= offset || next > target || extent < target || int64(len(data)) != next-offset {
			return failure.New(failure.PersistentRecoveryOutputConflict, map[string]string{"session_id": live.sessionID, "reason": "invalid_spool_range"}, nil)
		}
		result, stored := outputStore.ReconcilePersistentOutput(ctx, operation.SessionID(live.sessionID), offset, data)
		if stored.Err != nil {
			return stored.Err
		}
		if result.CanonicalExtent < next {
			return failure.New(failure.PersistentRecoveryOutputConflict, map[string]string{"session_id": live.sessionID, "reason": "canonical_extent"}, nil)
		}
		offset = next
	}
	if err := control.AcknowledgeOutput(ctx, target); err != nil {
		return err
	}
	live.mu.Lock()
	if live.outputBytes < target {
		live.outputBytes = target
	}
	live.notify()
	live.mu.Unlock()
	return nil
}

func (s *Service) finishPersistentTerminal(ctx context.Context, live *liveSession, control persistentapp.RecoveryAttachment, outputStore persistentOutputStore, bindingStore PersistentSessionStore, binding persistentcore.Binding, terminal persistentapp.TerminalEvidence) error {
	if err := validatePersistentTerminal(live, binding, terminal); err != nil {
		return err
	}
	ack, extent, err := control.RecoveryState(ctx)
	if err != nil {
		return err
	}
	if extent != terminal.OutputBytes || ack > extent {
		return failure.New(failure.PersistentRecoveryOutputConflict, map[string]string{"session_id": live.sessionID, "reason": "terminal_extent"}, nil)
	}
	if err := s.reconcilePersistentOutput(ctx, live, control, outputStore, ack, terminal.OutputBytes); err != nil {
		return err
	}
	rec := s.persistentTerminalReceipt(live, terminal)
	s.attachWorkspaceProvenance(&rec, live.workspace)
	if err := rec.Validate(); err != nil {
		return failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": live.sessionID, "reason": "terminal_receipt"}, err)
	}
	if err := s.publishPersistentTerminal(ctx, rec); err != nil {
		return err
	}
	s.scheduleStructuredTerminal(rec, live.reservation.StructuredAdapter)
	s.scheduleTelemetryTerminal(rec)
	s.scheduleEvidenceTerminal(rec, live.reservation)
	previousUpdate := binding.UpdatedAt
	binding.Lifecycle = persistentcore.LifecycleTerminal
	binding.UpdatedAt = time.Now().UTC()
	if !binding.UpdatedAt.After(previousUpdate) {
		binding.UpdatedAt = previousUpdate.Add(time.Nanosecond)
	}
	if result := bindingStore.AdvancePersistentBinding(ctx, binding); result.Err != nil {
		return result.Err
	}
	s.endManagedShell(live)
	_ = control.Cleanup(ctx)
	live.mu.Lock()
	live.state = terminal.State
	live.outcome = terminal.Outcome
	live.exit = terminal.Exit
	live.signal = terminal.Signal
	live.accepted = terminal.InputAcceptedBytes
	live.delivered = terminal.InputDeliveredBytes
	live.eof = terminal.StdinClosed
	live.outputBytes = terminal.OutputBytes
	live.notify()
	live.doneOnce.Do(func() { close(live.done) })
	live.mu.Unlock()
	return nil
}

func (s *Service) persistentTerminalReceipt(live *liveSession, terminal persistentapp.TerminalEvidence) receipt.Receipt {
	rec := s.receiptFor(live, terminal.State, terminal.Outcome)
	rec.ExecutionMode = string(live.spec.Mode)
	rec.Executable = live.spec.Executable
	if live.spec.Mode == operation.ExecutionModeShell {
		rec.Shell = live.spec.Shell
	}
	rec.CWD = live.spec.CWD
	rec.TTY = live.spec.TTY
	rec.TimeoutMS = live.spec.TimeoutMS
	rec.OutputBytes = terminal.OutputBytes
	rec.OutputComplete = terminal.OutputComplete
	rec.InputAcceptedBytes = terminal.InputAcceptedBytes
	rec.InputDeliveredBytes = terminal.InputDeliveredBytes
	rec.StdinClosed = terminal.StdinClosed
	// Persistent sessions build their receipt here rather than through the
	// direct path, so the policy provenance has to be carried on both.
	rec.StdinMode = string(live.spec.StdinMode)
	rec.TimeoutSource = live.timeoutSource
	rec.StdinModeSource = live.stdinSource
	rec.FailureReason = terminal.FailureReason
	rec.Spawn = terminal.Spawn
	rec.Exit = terminal.Exit
	rec.Signal = terminal.Signal
	return rec
}

func (s *Service) publishPersistentTerminal(ctx context.Context, rec receipt.Receipt) error {
	delay := 25 * time.Millisecond
	for {
		result := s.store.PublishTerminal(ctx, rec)
		if result.Err == nil {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
		if delay < time.Second {
			delay *= 2
			if delay > time.Second {
				delay = time.Second
			}
		}
	}
}

func validatePersistentStatus(live *liveSession, binding persistentcore.Binding, status persistentapp.Status) error {
	if status.SessionID != live.sessionID || status.GenerationID != binding.SupervisorGenerationID || status.OutputAcknowledged < 0 || status.OutputBytes < status.OutputAcknowledged {
		return failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": live.sessionID, "reason": "status_identity"}, nil)
	}
	return nil
}

func validatePersistentTerminal(live *liveSession, binding persistentcore.Binding, terminal persistentapp.TerminalEvidence) error {
	if terminal.SessionID != live.sessionID || terminal.GenerationID != binding.SupervisorGenerationID || !terminal.State.Terminal() || terminal.OutputBytes < 0 || terminal.InputAcceptedBytes < terminal.InputDeliveredBytes {
		return failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": live.sessionID, "reason": "terminal_identity"}, nil)
	}
	if terminal.State == session.Completed && terminal.Outcome != session.Success {
		return failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": live.sessionID, "reason": "terminal_outcome"}, nil)
	}
	return nil
}

func (s *Service) mirrorPersistentStatus(live *liveSession, status persistentapp.Status) {
	live.mu.Lock()
	live.accepted = status.InputAcceptedBytes
	live.delivered = status.InputDeliveredBytes
	live.eof = status.StdinClosed
	live.outputBytes = status.OutputBytes
	live.spawn = status.Spawn
	live.exit = status.Exit
	live.signal = status.Signal
	live.notify()
	live.mu.Unlock()
}
