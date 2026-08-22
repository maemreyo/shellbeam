package contextexec

import (
	"context"
	"fmt"
	"path/filepath"

	core "github.com/maemreyo/shellbeam/internal/core/contextexec"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func (s *Service) AuthorizePrepared(ctx context.Context, state operation.ContextExecState, resolvedExecutable string) (operation.ContextExecState, PreparedAuthorization, error) {
	if s == nil || s.store == nil {
		return state.Clone(), PreparedAuthorization{}, admissionFailure(state.Request, failure.ContextExecUnavailable, "prepared_authorizer_unavailable", nil)
	}
	if err := state.Validate(); err != nil || state.Lifecycle != core.LifecycleHelperAuthenticated || state.Context == nil {
		return state.Clone(), PreparedAuthorization{}, admissionFailure(state.Request, failure.ContextHelperAuthFailed, "prepared_state_invalid", err)
	}
	if resolvedExecutable == "" || !filepath.IsAbs(resolvedExecutable) {
		return state.Clone(), PreparedAuthorization{}, admissionFailure(state.Request, failure.ContextExecUnavailable, "prepared_executable_unproven", nil)
	}
	if s.daemonIncarnation == "" {
		return state.Clone(), PreparedAuthorization{}, admissionFailure(state.Request, failure.ContextExecUnavailable, "daemon_incarnation_unavailable", nil)
	}
	childOperationID, childSessionID, err := operation.DeriveContextChildIDs(state.RequestFingerprint)
	if err != nil {
		return state.Clone(), PreparedAuthorization{}, admissionFailure(state.Request, failure.ContextExecUnavailable, "child_identity_derivation_failed", err)
	}
	binding := &operation.ContextExecBinding{
		ContextExecID: state.Request.ContextExecID, ParentSessionID: operation.SessionID(state.Request.SessionID),
		AuthorityEpoch: state.Request.AuthorityEpoch, RequestFingerprint: state.RequestFingerprint,
	}
	executionFingerprint, err := binding.ExecutionFingerprint(state.Context.CWDObserved, resolvedExecutable)
	if err != nil {
		return state.Clone(), PreparedAuthorization{}, admissionFailure(state.Request, failure.ContextExecUnavailable, "execution_fingerprint_failed", err)
	}
	reservation := operation.Reservation{
		SchemaVersion: operation.ContextExecReservationSchemaVersion, OperationID: childOperationID, SessionID: childSessionID,
		RequestFingerprint: state.RequestFingerprint, ExecutionFingerprint: executionFingerprint,
		ExecutionMode: operation.ExecutionModeArgv, Executable: filepath.Clean(resolvedExecutable), Argv: append([]string(nil), state.Request.Argv...),
		CWD: state.Context.CWDObserved, TimeoutMS: state.Request.TimeoutMS, DaemonIncarnation: s.daemonIncarnation, ContextExec: binding,
		CreatedAt: s.now().UTC(),
	}
	storedReservation, _, result := s.store.ReserveOperation(ctx, reservation)
	if result.Err != nil {
		return state.Clone(), PreparedAuthorization{}, storeMutationError(state.Request, result, "child_reservation_ambiguous")
	}
	if !contextChildReservationMatches(storedReservation, reservation) {
		return state.Clone(), PreparedAuthorization{}, admissionFailure(state.Request, failure.ContextExecAmbiguous, "child_reservation_mismatch", nil)
	}
	reserved, result := s.store.AdvanceContextExec(ctx, state.Request.ContextExecID, operation.ContextExecTransition{
		Lifecycle: core.LifecycleChildReserved, ChildOperationID: childOperationID, ChildSessionID: childSessionID,
	})
	if result.Err != nil {
		return reserved.Clone(), PreparedAuthorization{}, storeMutationError(state.Request, result, "child_reserved_ambiguous")
	}
	if err := validateChildReservedState(reserved, state, childOperationID, childSessionID, false); err != nil {
		return reserved.Clone(), PreparedAuthorization{}, admissionFailure(state.Request, failure.ContextExecAmbiguous, "child_reserved_durable_mismatch", err)
	}
	authorized, result := s.store.AdvanceContextExec(ctx, state.Request.ContextExecID, operation.ContextExecTransition{
		Lifecycle: core.LifecycleChildReserved, ExecutionAuthorized: true,
	})
	if result.Err != nil {
		return authorized.Clone(), PreparedAuthorization{}, storeMutationError(state.Request, result, "execute_authorization_ambiguous")
	}
	if err := validateChildReservedState(authorized, state, childOperationID, childSessionID, true); err != nil {
		return authorized.Clone(), PreparedAuthorization{}, admissionFailure(state.Request, failure.ContextExecAmbiguous, "execute_authorization_durable_mismatch", err)
	}
	return authorized.Clone(), PreparedAuthorization{ChildOperationID: childOperationID, ChildSessionID: childSessionID, ResolvedExecutable: filepath.Clean(resolvedExecutable)}, nil
}

func (s *Service) RecordSpawn(ctx context.Context, state operation.ContextExecState, truth SpawnTruth) (operation.ContextExecState, error) {
	if s == nil || s.store == nil {
		return state.Clone(), admissionFailure(state.Request, failure.ContextExecUnavailable, "spawn_recorder_unavailable", nil)
	}
	if err := state.Validate(); err != nil || state.Lifecycle != core.LifecycleChildReserved || !state.ExecutionAuthorized || state.Context == nil {
		return state.Clone(), admissionFailure(state.Request, failure.ContextExecAmbiguous, "spawn_state_invalid", err)
	}
	if !truth.Spawn.Attempted || !truth.Spawn.Succeeded || truth.Spawn.ErrorCode != "" {
		return state.Clone(), admissionFailure(state.Request, failure.ContextExecUnavailable, "spawn_success_unproven", nil)
	}
	if truth.ChildOperationID != state.ChildOperationID || truth.ChildSessionID != state.ChildSessionID || truth.ResolvedExecutable == "" || !filepath.IsAbs(truth.ResolvedExecutable) {
		return state.Clone(), admissionFailure(state.Request, failure.ContextExecAmbiguous, "spawn_identity_mismatch", nil)
	}
	binding := &operation.ContextExecBinding{
		ContextExecID: state.Request.ContextExecID, ParentSessionID: operation.SessionID(state.Request.SessionID),
		AuthorityEpoch: state.Request.AuthorityEpoch, RequestFingerprint: state.RequestFingerprint,
	}
	executionFingerprint, err := binding.ExecutionFingerprint(state.Context.CWDObserved, filepath.Clean(truth.ResolvedExecutable))
	if err != nil {
		return state.Clone(), admissionFailure(state.Request, failure.ContextExecAmbiguous, "spawn_execution_fingerprint_invalid", err)
	}
	reservation := operation.Reservation{
		SchemaVersion: operation.ContextExecReservationSchemaVersion, OperationID: state.ChildOperationID, SessionID: state.ChildSessionID,
		RequestFingerprint: state.RequestFingerprint, ExecutionFingerprint: executionFingerprint,
		ExecutionMode: operation.ExecutionModeArgv, Executable: filepath.Clean(truth.ResolvedExecutable), Argv: append([]string(nil), state.Request.Argv...),
		CWD: state.Context.CWDObserved, TimeoutMS: state.Request.TimeoutMS, DaemonIncarnation: s.daemonIncarnation, ContextExec: binding,
		CreatedAt: s.now().UTC(),
	}
	storedReservation, _, result := s.store.ReserveOperation(ctx, reservation)
	if result.Err != nil {
		return state.Clone(), storeMutationError(state.Request, result, "spawn_reservation_replay_ambiguous")
	}
	if !contextChildReservationMatches(storedReservation, reservation) {
		return state.Clone(), admissionFailure(state.Request, failure.ContextExecAmbiguous, "spawn_reservation_mismatch", nil)
	}
	spawned, result := s.store.AdvanceContextExec(ctx, state.Request.ContextExecID, operation.ContextExecTransition{Lifecycle: core.LifecycleChildSpawned})
	if result.Err != nil {
		return spawned.Clone(), storeMutationError(state.Request, result, "child_spawned_ambiguous")
	}
	if err := spawned.Validate(); err != nil || spawned.Lifecycle != core.LifecycleChildSpawned || !spawned.ExecutionAuthorized || spawned.ChildOperationID != state.ChildOperationID || spawned.ChildSessionID != state.ChildSessionID {
		return spawned.Clone(), admissionFailure(state.Request, failure.ContextExecAmbiguous, "child_spawned_durable_mismatch", err)
	}
	return spawned.Clone(), nil
}

func (s *Service) RecordTerminal(ctx context.Context, state operation.ContextExecState, truth TerminalTruth) (operation.ContextExecState, error) {
	terminal, err := s.validateTerminalTruth(state, truth)
	if err != nil {
		return state.Clone(), err
	}
	reservation, err := s.verifiedChildReservation(ctx, state, terminal.Executable.ResolvedPath)
	if err != nil {
		return state.Clone(), err
	}
	if err := s.persistTerminalOutput(ctx, state, truth); err != nil {
		return state.Clone(), err
	}
	persisted, err := s.persistChildTerminal(ctx, state, terminal)
	if err != nil {
		return persisted.Clone(), err
	}
	finalized, err := s.promoteCanonicalTerminal(ctx, persisted, terminal)
	if err != nil {
		return finalized.Clone(), err
	}
	if err := s.publishCanonicalTerminal(ctx, finalized, reservation); err != nil {
		return finalized.Clone(), err
	}
	if err := s.releaseExecutionLease(ctx, finalized); err != nil {
		return finalized.Clone(), err
	}
	return finalized.Clone(), nil
}

func (s *Service) CanonicalizeNoChildFailure(ctx context.Context, state operation.ContextExecState, truth NoChildFailureTruth) (operation.ContextExecState, error) {
	if s == nil || s.store == nil || state.Context == nil || state.Helper == nil {
		return state.Clone(), admissionFailure(state.Request, failure.ContextExecUnavailable, "no_child_canonicalizer_unavailable", nil)
	}
	if truth.FailureCode == "" || truth.Spawn.Succeeded {
		return state.Clone(), admissionFailure(state.Request, failure.ContextExecAmbiguous, "no_child_failure_invalid", nil)
	}
	if truth.Spawn.Attempted {
		if state.Lifecycle != core.LifecycleChildReserved || !state.ExecutionAuthorized || truth.ResolvedExecutable == "" || !filepath.IsAbs(truth.ResolvedExecutable) {
			return state.Clone(), admissionFailure(state.Request, failure.ContextExecAmbiguous, "failed_spawn_state_invalid", nil)
		}
		if err := s.verifyChildReservation(ctx, state, truth.ResolvedExecutable); err != nil {
			return state.Clone(), err
		}
	} else if state.Lifecycle != core.LifecycleHelperAuthenticated || state.ChildOperationID != "" || state.ExecutionAuthorized || truth.ResolvedExecutable != "" || truth.Spawn.ErrorCode != "" {
		return state.Clone(), admissionFailure(state.Request, failure.ContextExecAmbiguous, "prepare_failure_state_invalid", nil)
	}
	canonical := core.Result{
		SchemaVersion: core.SchemaVersion, ContextExecID: state.Request.ContextExecID, RequestFingerprint: state.RequestFingerprint,
		Lifecycle: core.LifecycleCanonicalized, Context: *state.Context, Helper: state.Helper,
		Spawn: truth.Spawn, EvidenceQuality: core.EvidenceQualityUnproven, FailureCode: truth.FailureCode,
	}
	if truth.Spawn.Attempted {
		canonical.Executable = core.ExecutableIdentity{Requested: state.Request.Argv[0], ResolvedPath: filepath.Clean(truth.ResolvedExecutable)}
	}
	if err := canonical.Validate(); err != nil {
		return state.Clone(), admissionFailure(state.Request, failure.ContextExecAmbiguous, "canonical_no_child_invalid", err)
	}
	finalized, result := s.store.AdvanceContextExec(ctx, state.Request.ContextExecID, operation.ContextExecTransition{Lifecycle: core.LifecycleCanonicalized, Result: &canonical})
	if result.Err != nil {
		return finalized.Clone(), storeMutationError(state.Request, result, "canonical_no_child_ambiguous")
	}
	if err := finalized.Validate(); err != nil || finalized.Lifecycle != core.LifecycleCanonicalized || finalized.Result == nil || finalized.Result.EvidenceAuthority != "" {
		return finalized.Clone(), admissionFailure(state.Request, failure.ContextExecAmbiguous, "canonical_no_child_durable_mismatch", err)
	}
	if err := s.releaseExecutionLease(ctx, finalized); err != nil {
		return finalized.Clone(), err
	}
	return finalized.Clone(), nil
}

func (s *Service) verifyChildReservation(ctx context.Context, state operation.ContextExecState, resolvedExecutable string) error {
	_, err := s.verifiedChildReservation(ctx, state, resolvedExecutable)
	return err
}

func (s *Service) verifiedChildReservation(ctx context.Context, state operation.ContextExecState, resolvedExecutable string) (operation.Reservation, error) {
	if state.Context == nil || state.ChildOperationID == "" || state.ChildSessionID == "" || resolvedExecutable == "" || !filepath.IsAbs(resolvedExecutable) {
		return operation.Reservation{}, admissionFailure(state.Request, failure.ContextExecAmbiguous, "child_reservation_verification_input_invalid", nil)
	}
	binding := &operation.ContextExecBinding{ContextExecID: state.Request.ContextExecID, ParentSessionID: operation.SessionID(state.Request.SessionID), AuthorityEpoch: state.Request.AuthorityEpoch, RequestFingerprint: state.RequestFingerprint}
	executionFingerprint, err := binding.ExecutionFingerprint(state.Context.CWDObserved, filepath.Clean(resolvedExecutable))
	if err != nil {
		return operation.Reservation{}, admissionFailure(state.Request, failure.ContextExecAmbiguous, "child_execution_fingerprint_invalid", err)
	}
	want := operation.Reservation{
		SchemaVersion: operation.ContextExecReservationSchemaVersion, OperationID: state.ChildOperationID, SessionID: state.ChildSessionID,
		RequestFingerprint: state.RequestFingerprint, ExecutionFingerprint: executionFingerprint, ExecutionMode: operation.ExecutionModeArgv,
		Executable: filepath.Clean(resolvedExecutable), Argv: append([]string(nil), state.Request.Argv...), CWD: state.Context.CWDObserved,
		TimeoutMS: state.Request.TimeoutMS, DaemonIncarnation: s.daemonIncarnation, ContextExec: binding, CreatedAt: s.now().UTC(),
	}
	stored, _, result := s.store.ReserveOperation(ctx, want)
	if result.Err != nil {
		return operation.Reservation{}, storeMutationError(state.Request, result, "child_reservation_verification_ambiguous")
	}
	if !contextChildReservationMatches(stored, want) {
		return operation.Reservation{}, admissionFailure(state.Request, failure.ContextExecAmbiguous, "child_reservation_verification_mismatch", nil)
	}
	return stored, nil
}

func (s *Service) releaseExecutionLease(ctx context.Context, state operation.ContextExecState) error {
	lease := operation.ContextExecLease{SessionID: operation.SessionID(state.Request.SessionID), AuthorityEpoch: state.Request.AuthorityEpoch, ContextExecID: state.Request.ContextExecID, RequestFingerprint: state.RequestFingerprint}
	if err := lease.Validate(); err != nil {
		return admissionFailure(state.Request, failure.ContextExecAmbiguous, "context_exec_lease_invalid", err)
	}
	result := s.store.ReleaseContextExecLease(ctx, lease)
	if result.Err != nil {
		return storeMutationError(state.Request, result, "context_exec_lease_release_ambiguous")
	}
	return nil
}

func contextChildReservationMatches(got, want operation.Reservation) bool {
	if got.SchemaVersion != want.SchemaVersion || got.OperationID != want.OperationID || got.SessionID != want.SessionID || got.RequestFingerprint != want.RequestFingerprint || got.ExecutionFingerprint != want.ExecutionFingerprint || got.ExecutionMode != want.ExecutionMode || got.Executable != want.Executable || got.CWD != want.CWD || got.TimeoutMS != want.TimeoutMS || got.DaemonIncarnation != want.DaemonIncarnation || len(got.Argv) != len(want.Argv) {
		return false
	}
	for i := range want.Argv {
		if got.Argv[i] != want.Argv[i] {
			return false
		}
	}
	if got.ContextExec == nil || want.ContextExec == nil {
		return got.ContextExec == nil && want.ContextExec == nil
	}
	return *got.ContextExec == *want.ContextExec
}

func validateChildReservedState(got, prior operation.ContextExecState, childOperationID operation.ID, childSessionID operation.SessionID, authorized bool) error {
	if err := got.Validate(); err != nil {
		return err
	}
	if got.Lifecycle != core.LifecycleChildReserved || got.RequestFingerprint != prior.RequestFingerprint || got.Context == nil || prior.Context == nil || *got.Context != *prior.Context || got.Helper == nil || prior.Helper == nil || *got.Helper != *prior.Helper || got.ChildOperationID != childOperationID || got.ChildSessionID != childSessionID || got.ExecutionAuthorized != authorized {
		return fmt.Errorf("context child reserved state mismatch")
	}
	return nil
}
