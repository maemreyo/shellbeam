//go:build linux || darwin

package supervisor

import (
	"context"
	"errors"
	"os"
	"time"

	persistentapp "github.com/maemreyo/shellbeam/internal/app/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func (l *Launcher) Reattach(ctx context.Context, binding core.Binding) (persistentapp.Attachment, persistentapp.Status, error) {
	if err := ctx.Err(); err != nil {
		return nil, persistentapp.Status{}, err
	}
	if err := binding.Validate(); err != nil || binding.Lifecycle == core.LifecycleTerminal || binding.Lifecycle == core.LifecycleLost {
		return nil, persistentapp.Status{}, failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": binding.SessionID, "reason": "binding"}, err)
	}
	layout, err := OpenPrivateState(l.options.RuntimeRoot, binding.SessionID, binding.SupervisorGenerationID)
	if err != nil {
		return nil, persistentapp.Status{}, err
	}
	capability, err := LoadCapability(layout)
	if err != nil {
		return nil, persistentapp.Status{}, err
	}
	deadline := time.Now().Add(l.options.HandshakeTimeout)
	var last error
	for {
		if err := ctx.Err(); err != nil {
			return nil, persistentapp.Status{}, err
		}
		attemptCtx, cancel := context.WithDeadline(ctx, deadline)
		attachment, status, attachErr := l.attach(attemptCtx, layout, capability, binding.SessionID, binding.SupervisorGenerationID)
		cancel()
		if attachErr == nil {
			if err := validateReattachedStatus(binding, status); err != nil {
				if attachment != nil {
					_ = attachment.Close()
				}
				return nil, persistentapp.Status{}, err
			}
			return attachment, status, nil
		}
		if attachment != nil {
			_ = attachment.Close()
		}
		last = attachErr
		if !errors.Is(attachErr, failure.SupervisorUnavailable) {
			return nil, persistentapp.Status{}, attachErr
		}
		if offline, status, recoveryErr := openTerminalRecoveryClient(layout, capability, binding); recoveryErr == nil {
			return offline, status, nil
		} else if terminalPrivateRecordExists(layout) {
			return nil, persistentapp.Status{}, recoveryErr
		}
		if !time.Now().Before(deadline) {
			return nil, persistentapp.Status{}, last
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, persistentapp.Status{}, ctx.Err()
		case <-timer.C:
		}
	}
}

func openTerminalRecoveryClient(layout Layout, capability Capability, binding core.Binding) (*Client, persistentapp.Status, error) {
	record, err := LoadTerminalRecord(layout, capability, binding.SessionID, binding.SupervisorGenerationID)
	if err != nil {
		return nil, persistentapp.Status{}, err
	}
	spool, _, err := openVerifiedTerminalSpool(layout, capability, binding.SessionID, binding.SupervisorGenerationID)
	if err != nil {
		return nil, persistentapp.Status{}, err
	}
	acknowledged := spool.Acknowledged()
	_ = spool.Close()
	status := persistentapp.Status{
		SessionID: binding.SessionID, GenerationID: binding.SupervisorGenerationID,
		State: record.State, Outcome: record.Outcome, PID: 0,
		OutputBytes: record.OutputBytes, OutputAcknowledged: acknowledged,
		InputAcceptedBytes: record.InputAcceptedBytes, InputDeliveredBytes: record.InputDeliveredBytes,
		NextInputOffset: record.InputAcceptedBytes, StdinClosed: record.StdinClosed,
		Spawn: record.Spawn, Exit: record.Exit, Signal: record.Signal, FailureReason: record.FailureReason,
	}
	client := &Client{
		layout: layout, capability: capability, sessionID: binding.SessionID,
		generation: binding.SupervisorGenerationID, status: status,
	}
	return client, status, nil
}

func validateReattachedStatus(binding core.Binding, status persistentapp.Status) error {
	if status.SessionID != binding.SessionID || status.GenerationID != binding.SupervisorGenerationID {
		return failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": binding.SessionID, "reason": "reattach_identity"}, nil)
	}
	if status.State == session.Running {
		if status.PID <= 0 || !status.Spawn.Attempted || !status.Spawn.Succeeded {
			return failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": binding.SessionID, "reason": "reattach_running_proof"}, nil)
		}
		return nil
	}
	if status.State.Terminal() && status.State != session.Abandoned && status.PID == 0 {
		return nil
	}
	return failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": binding.SessionID, "reason": "reattach_state"}, nil)
}

func terminalPrivateRecordExists(layout Layout) bool {
	info, err := os.Lstat(layout.TerminalPath)
	return err == nil && info.Mode()&os.ModeSymlink == 0 && info.Mode().IsRegular()
}

var _ persistentapp.Reattacher = (*Launcher)(nil)
