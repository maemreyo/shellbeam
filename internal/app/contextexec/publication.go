package contextexec

import (
	"bytes"
	"context"
	"fmt"

	core "github.com/maemreyo/shellbeam/internal/core/contextexec"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func (s *Service) validateTerminalTruth(state operation.ContextExecState, truth TerminalTruth) (core.Result, error) {
	if s == nil || s.store == nil {
		return core.Result{}, admissionFailure(state.Request, failure.ContextExecUnavailable, "terminal_recorder_unavailable", nil)
	}
	if err := state.Validate(); err != nil || state.Lifecycle != core.LifecycleChildSpawned || !state.ExecutionAuthorized || state.Context == nil || state.Helper == nil {
		return core.Result{}, admissionFailure(state.Request, failure.ContextExecAmbiguous, "terminal_state_invalid", err)
	}
	terminal := truth.Result
	if err := terminal.Validate(); err != nil || terminal.Lifecycle != core.LifecycleChildTerminal || terminal.EvidenceAuthority != "" || terminal.FailureCode != "" {
		return core.Result{}, admissionFailure(state.Request, failure.ContextExecAmbiguous, "helper_terminal_invalid", err)
	}
	if terminal.ContextExecID != state.Request.ContextExecID || terminal.RequestFingerprint != state.RequestFingerprint || terminal.Context != *state.Context || terminal.Helper == nil || *terminal.Helper != *state.Helper || terminal.Executable.Requested != state.Request.Argv[0] {
		return core.Result{}, admissionFailure(state.Request, failure.ContextExecAmbiguous, "helper_terminal_identity_mismatch", nil)
	}
	if truth.StdoutBytes < 0 || truth.StderrBytes < 0 || terminal.Output.StdoutBytes != truth.StdoutBytes || terminal.Output.StderrBytes != truth.StderrBytes || int64(len(truth.CombinedOutput)) != truth.StdoutBytes+truth.StderrBytes {
		return core.Result{}, admissionFailure(state.Request, failure.ContextExecAmbiguous, "helper_terminal_output_count_mismatch", nil)
	}
	return terminal, nil
}

func (s *Service) persistTerminalOutput(ctx context.Context, state operation.ContextExecState, truth TerminalTruth) error {
	want := truth.CombinedOutput
	existing, next, err := s.store.ReadOutput(ctx, state.ChildSessionID, 0, len(want)+1)
	if err != nil {
		return admissionFailure(state.Request, failure.ContextExecAmbiguous, "child_output_read_failed", err)
	}
	if next != 0 {
		if next != int64(len(want)) || !bytes.Equal(existing, want) {
			return admissionFailure(state.Request, failure.ContextExecAmbiguous, "child_output_conflict", nil)
		}
		return nil
	}
	if len(want) == 0 {
		return nil
	}
	n, result := s.store.AppendOutput(ctx, state.ChildSessionID, want)
	if result.Err != nil {
		return storeMutationError(state.Request, result, "child_output_ambiguous")
	}
	if n != len(want) {
		return admissionFailure(state.Request, failure.ContextExecAmbiguous, "child_output_short_write", nil)
	}
	return nil
}

func (s *Service) verifyPersistedTerminalOutput(ctx context.Context, state operation.ContextExecState) error {
	if state.Result == nil {
		return admissionFailure(state.Request, failure.ContextExecAmbiguous, "persisted_output_terminal_missing", nil)
	}
	expected := state.Result.Output.StdoutBytes + state.Result.Output.StderrBytes
	if expected < 0 || expected > int64(^uint(0)>>1)-1 {
		return admissionFailure(state.Request, failure.ContextExecAmbiguous, "persisted_output_bound_invalid", nil)
	}
	data, next, err := s.store.ReadOutput(ctx, state.ChildSessionID, 0, int(expected)+1)
	if err != nil {
		return admissionFailure(state.Request, failure.ContextExecAmbiguous, "persisted_output_read_failed", err)
	}
	if next != expected || int64(len(data)) != expected {
		return admissionFailure(state.Request, failure.ContextExecAmbiguous, "persisted_output_extent_mismatch", nil)
	}
	return nil
}

func (s *Service) persistChildTerminal(ctx context.Context, state operation.ContextExecState, terminal core.Result) (operation.ContextExecState, error) {
	persisted, result := s.store.AdvanceContextExec(ctx, state.Request.ContextExecID, operation.ContextExecTransition{Lifecycle: core.LifecycleChildTerminal, Result: &terminal})
	if result.Err != nil {
		return persisted.Clone(), storeMutationError(state.Request, result, "child_terminal_ambiguous")
	}
	if err := persisted.Validate(); err != nil || persisted.Lifecycle != core.LifecycleChildTerminal || persisted.Result == nil || persisted.Result.EvidenceAuthority != "" {
		return persisted.Clone(), admissionFailure(state.Request, failure.ContextExecAmbiguous, "child_terminal_durable_mismatch", err)
	}
	return persisted.Clone(), nil
}

func (s *Service) promoteCanonicalTerminal(ctx context.Context, persisted operation.ContextExecState, terminal core.Result) (operation.ContextExecState, error) {
	canonical := terminal
	canonical.Lifecycle = core.LifecycleCanonicalized
	canonical.EvidenceAuthority = core.EvidenceAuthorityContextExecChildOwnedV1
	if err := canonical.Validate(); err != nil {
		return persisted.Clone(), admissionFailure(persisted.Request, failure.ContextExecAmbiguous, "canonical_terminal_invalid", err)
	}
	finalized, result := s.store.AdvanceContextExec(ctx, persisted.Request.ContextExecID, operation.ContextExecTransition{Lifecycle: core.LifecycleCanonicalized, Result: &canonical})
	if result.Err != nil {
		return finalized.Clone(), storeMutationError(persisted.Request, result, "canonical_terminal_ambiguous")
	}
	if err := finalized.Validate(); err != nil || finalized.Lifecycle != core.LifecycleCanonicalized || finalized.Result == nil || finalized.Result.EvidenceAuthority != core.EvidenceAuthorityContextExecChildOwnedV1 {
		return finalized.Clone(), admissionFailure(persisted.Request, failure.ContextExecAmbiguous, "canonical_terminal_durable_mismatch", err)
	}
	return finalized.Clone(), nil
}

func (s *Service) publishCanonicalTerminal(ctx context.Context, state operation.ContextExecState, reservation operation.Reservation) error {
	rec, err := canonicalReceipt(state, reservation, s.daemonIncarnation)
	if err != nil {
		return admissionFailure(state.Request, failure.ContextExecAmbiguous, "canonical_receipt_invalid", err)
	}
	result := s.store.PublishTerminal(ctx, rec)
	if result.Err != nil {
		return storeMutationError(state.Request, result, "canonical_receipt_ambiguous")
	}
	if s.terminalScheduler != nil {
		if err := s.terminalScheduler.ScheduleContextTerminal(ctx, rec, reservation); err != nil {
			return admissionFailure(state.Request, failure.ContextExecUnavailable, "terminal_schedule_failed", err)
		}
	}
	return nil
}

func canonicalReceipt(state operation.ContextExecState, reservation operation.Reservation, incarnation string) (receipt.Receipt, error) {
	if state.Result == nil || state.Context == nil || reservation.ContextExec == nil || state.Lifecycle != core.LifecycleCanonicalized || state.Result.EvidenceAuthority != core.EvidenceAuthorityContextExecChildOwnedV1 {
		return receipt.Receipt{}, fmt.Errorf("canonical context state unavailable")
	}
	terminalState, outcome := contextTerminalOutcome(*state.Result)
	rec := receipt.Receipt{
		SchemaVersion: 6, OperationID: string(state.ChildOperationID), SessionID: string(state.ChildSessionID),
		RequestFingerprint: state.RequestFingerprint, ExecutionFingerprint: reservation.ExecutionFingerprint, DaemonIncarnation: incarnation,
		ExecutionMode: string(operation.ExecutionModeArgv), Executable: state.Result.Executable.ResolvedPath,
		State: terminalState, Outcome: outcome, CWD: state.Context.CWDObserved, TimeoutMS: state.Request.TimeoutMS,
		AuthorityEpoch: state.Request.AuthorityEpoch, EvidenceAuthority: receipt.EvidenceAuthorityContextExecChildOwnedV1,
		OutputBytes: state.Result.Output.StdoutBytes + state.Result.Output.StderrBytes, OutputComplete: state.Result.Output.OutputComplete,
		StdinClosed: true, StdinMode: "closed", TimeoutSource: "requested", StdinModeSource: "default",
		Spawn: state.Result.Spawn, Exit: state.Result.Exit, Signal: state.Result.Signal,
		ContextExec: &receipt.ContextExecProvenance{ContextExecID: state.Request.ContextExecID, ParentSessionID: state.Request.SessionID, AuthorityEpoch: state.Request.AuthorityEpoch, RequestedExecutable: state.Result.Executable.Requested, ResolvedExecutable: state.Result.Executable.ResolvedPath},
	}
	return rec, rec.Validate()
}

func contextTerminalOutcome(result core.Result) (session.State, session.Outcome) {
	if result.TimedOut {
		return session.TimedOut, session.Timeout
	}
	if !result.Output.OutputComplete {
		return session.Failed, session.Failure
	}
	if result.Exit.Code != nil && *result.Exit.Code == 0 {
		return session.Completed, session.Success
	}
	return session.Failed, session.Failure
}
