package contextexec

import (
	"context"

	core "github.com/maemreyo/shellbeam/internal/core/contextexec"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

type RecoveryDisposition string

const (
	RecoveryResumeAdmission         RecoveryDisposition = "resume_admission"
	RecoveryAmbiguousDelivery       RecoveryDisposition = "ambiguous_delivery"
	RecoveryReconnectSameGeneration RecoveryDisposition = "reconnect_same_generation"
	RecoveryPreparedClosed          RecoveryDisposition = "prepared_closed"
	RecoverySpawnUnknown            RecoveryDisposition = "spawn_unknown"
	RecoveryHelperLost              RecoveryDisposition = "helper_lost"
	RecoveryTerminalPending         RecoveryDisposition = "terminal_pending"
	RecoveryFinal                   RecoveryDisposition = "final"
)

type RecoveryDecision struct {
	State       operation.ContextExecState
	Disposition RecoveryDisposition
	RetainLease bool
}

func (s *Service) Reconcile(ctx context.Context) ([]RecoveryDecision, error) {
	if s == nil || s.store == nil {
		return nil, failure.New(failure.ContextExecUnavailable, map[string]string{"reason": "context_exec_store_unavailable"}, nil)
	}
	candidates, err := s.store.ListContextExecRecoveryCandidates(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]RecoveryDecision, 0, len(candidates))
	for _, candidate := range candidates {
		decision, err := s.reconcileCandidate(ctx, candidate)
		if err != nil {
			return nil, err
		}
		out = append(out, decision)
	}
	return out, nil
}

func (s *Service) reconcileCandidate(ctx context.Context, state operation.ContextExecState) (RecoveryDecision, error) {
	if err := state.Validate(); err != nil {
		return RecoveryDecision{}, admissionFailure(state.Request, failure.ContextExecAmbiguous, "recovery_state_invalid", err)
	}
	switch state.Lifecycle {
	case core.LifecycleReserved:
		return RecoveryDecision{State: state.Clone(), Disposition: RecoveryResumeAdmission}, nil
	case core.LifecycleHelperRequested:
		return s.persistRecoveryTerminal(ctx, state, core.LifecycleAmbiguous, RecoveryAmbiguousDelivery)
	case core.LifecycleHelperAuthenticated:
		return RecoveryDecision{State: state.Clone(), Disposition: RecoveryReconnectSameGeneration, RetainLease: true}, nil
	case core.LifecycleChildReserved:
		if !state.ExecutionAuthorized {
			return RecoveryDecision{State: state.Clone(), Disposition: RecoveryPreparedClosed, RetainLease: true}, nil
		}
		return s.persistRecoveryTerminal(ctx, state, core.LifecycleAmbiguous, RecoverySpawnUnknown)
	case core.LifecycleChildSpawned:
		return s.persistRecoveryTerminal(ctx, state, core.LifecycleHelperLost, RecoveryHelperLost)
	case core.LifecycleChildTerminal:
		return s.recoverChildTerminal(ctx, state)
	case core.LifecycleCanonicalized:
		return s.recoverCanonicalized(ctx, state)
	case core.LifecycleHelperLost, core.LifecycleAmbiguous:
		return RecoveryDecision{State: state.Clone(), Disposition: RecoveryFinal, RetainLease: true}, nil
	default:
		return RecoveryDecision{}, admissionFailure(state.Request, failure.ContextExecAmbiguous, "recovery_lifecycle_unknown", nil)
	}
}

func (s *Service) persistRecoveryTerminal(ctx context.Context, state operation.ContextExecState, lifecycle core.Lifecycle, disposition RecoveryDisposition) (RecoveryDecision, error) {
	next, result := s.store.AdvanceContextExec(ctx, state.Request.ContextExecID, operation.ContextExecTransition{Lifecycle: lifecycle})
	if result.Err != nil {
		return RecoveryDecision{}, storeMutationError(state.Request, result, "recovery_transition_ambiguous")
	}
	if err := next.Validate(); err != nil || next.Lifecycle != lifecycle {
		return RecoveryDecision{}, admissionFailure(state.Request, failure.ContextExecAmbiguous, "recovery_durable_mismatch", err)
	}
	return RecoveryDecision{State: next.Clone(), Disposition: disposition, RetainLease: true}, nil
}

func (s *Service) recoverChildTerminal(ctx context.Context, state operation.ContextExecState) (RecoveryDecision, error) {
	if state.Result == nil || state.Result.Lifecycle != core.LifecycleChildTerminal {
		return RecoveryDecision{}, admissionFailure(state.Request, failure.ContextExecAmbiguous, "recovery_terminal_missing", nil)
	}
	reservation, err := s.verifiedChildReservation(ctx, state, state.Result.Executable.ResolvedPath)
	if err != nil {
		return RecoveryDecision{}, err
	}
	if err := s.verifyPersistedTerminalOutput(ctx, state); err != nil {
		return RecoveryDecision{}, err
	}
	finalized, err := s.promoteCanonicalTerminal(ctx, state, *state.Result)
	if err != nil {
		return RecoveryDecision{}, err
	}
	if err := s.publishCanonicalTerminal(ctx, finalized, reservation); err != nil {
		return RecoveryDecision{}, err
	}
	if err := s.releaseExecutionLease(ctx, finalized); err != nil {
		return RecoveryDecision{}, err
	}
	return RecoveryDecision{State: finalized.Clone(), Disposition: RecoveryFinal}, nil
}

func (s *Service) recoverCanonicalized(ctx context.Context, state operation.ContextExecState) (RecoveryDecision, error) {
	if state.Result == nil || state.Result.Lifecycle != core.LifecycleCanonicalized {
		return RecoveryDecision{}, admissionFailure(state.Request, failure.ContextExecAmbiguous, "recovery_canonical_missing", nil)
	}
	if state.Result.EvidenceAuthority == core.EvidenceAuthorityContextExecChildOwnedV1 {
		reservation, err := s.verifiedChildReservation(ctx, state, state.Result.Executable.ResolvedPath)
		if err != nil {
			return RecoveryDecision{}, err
		}
		if err := s.verifyPersistedTerminalOutput(ctx, state); err != nil {
			return RecoveryDecision{}, err
		}
		if err := s.publishCanonicalTerminal(ctx, state, reservation); err != nil {
			return RecoveryDecision{}, err
		}
	} else if state.Result.FailureCode == "" || state.Result.Spawn.Succeeded {
		return RecoveryDecision{}, admissionFailure(state.Request, failure.ContextExecAmbiguous, "recovery_canonical_authority_invalid", nil)
	}
	if err := s.releaseExecutionLease(ctx, state); err != nil {
		return RecoveryDecision{}, err
	}
	return RecoveryDecision{State: state.Clone(), Disposition: RecoveryFinal}, nil
}
