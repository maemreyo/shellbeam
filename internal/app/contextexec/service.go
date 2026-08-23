package contextexec

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	shellapp "github.com/maemreyo/shellbeam/internal/app/shellintegration"
	core "github.com/maemreyo/shellbeam/internal/core/contextexec"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

type Service struct {
	store               ContextExecStore
	authority           ContextAuthority
	helper              HelperRuntime
	terminalScheduler   TerminalScheduler
	now                 func() time.Time
	newOpaqueLaunchID   func() string
	newHelperGeneration func() string
	helperExecutable    string
	daemonIncarnation   string
}

func NewService(options Options) *Service {
	now := options.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	service := &Service{
		store: options.Store, authority: options.Authority, helper: options.Helper, terminalScheduler: options.TerminalScheduler, now: now,
		newOpaqueLaunchID: options.NewOpaqueLaunchID, newHelperGeneration: options.NewHelperGeneration,
		helperExecutable: options.HelperExecutable, daemonIncarnation: options.DaemonIncarnation,
	}
	if binder, ok := options.Helper.(RuntimeCallbackBinder); ok {
		binder.BindContextExecCallbacks(RuntimeCallbacks{
			BindClaim: service.BindClaim, AuthorizePrepared: service.AuthorizePrepared, RecordSpawn: service.RecordSpawn,
			RecordTerminal: service.RecordTerminal, CanonicalizeNoChildFailure: service.CanonicalizeNoChildFailure,
		})
	}
	return service
}

func (s *Service) Execute(ctx context.Context, req core.Request) (operation.ContextExecState, error) {
	fingerprint, err := validateRequest(req)
	if err != nil {
		return operation.ContextExecState{}, err
	}
	if s == nil || s.store == nil {
		return operation.ContextExecState{}, admissionFailure(req, failure.ContextExecUnavailable, "context_exec_store_unavailable", nil)
	}
	var reservedReplay *operation.ContextExecState
	if existing, found, err := s.store.LookupContextExec(ctx, req.ContextExecID); err != nil {
		return operation.ContextExecState{}, err
	} else if found {
		replayed, replayErr := replayContextExec(req, fingerprint, existing)
		if replayErr != nil || replayed.Lifecycle != core.LifecycleReserved {
			return replayed, replayErr
		}
		copy := replayed.Clone()
		reservedReplay = &copy
	}
	if s.helper == nil || !s.helper.Qualified() {
		return operation.ContextExecState{}, admissionFailure(req, failure.ContextExecUnavailable, "helper_runtime_unavailable", nil)
	}
	if s.authority == nil {
		return operation.ContextExecState{}, admissionFailure(req, failure.ContextExecUnavailable, "context_authority_unavailable", nil)
	}
	first, err := s.authority.Snapshot(ctx, req)
	if err != nil {
		return operation.ContextExecState{}, err
	}
	if err := validateAdmission(req, first); err != nil {
		return operation.ContextExecState{}, err
	}
	if _, found, err := s.store.FindContextExecLease(ctx, operation.SessionID(req.SessionID), req.AuthorityEpoch); err != nil {
		return operation.ContextExecState{}, err
	} else if found {
		return operation.ContextExecState{}, admissionFailure(req, failure.ContextExecAmbiguous, "active_context_exec_lease", nil)
	}
	if reservedReplay != nil {
		if err := validateRevalidation(req, first, first, reservedReplay.Expectation); err != nil {
			return reservedReplay.Clone(), err
		}
		return s.armReserved(ctx, req, fingerprint, reservedReplay.Clone(), first)
	}
	reserved, created, err := s.reserveExpectation(ctx, req, fingerprint, first)
	if err != nil || !created {
		return reserved, err
	}
	return s.armReserved(ctx, req, fingerprint, reserved, first)
}

func validateRequest(req core.Request) (string, error) {
	if err := req.Validate(); err != nil {
		return "", failure.New(failure.InvalidInput, map[string]string{"field": "context_exec"}, err)
	}
	fingerprint, err := req.Fingerprint()
	if err != nil {
		return "", failure.New(failure.InvalidInput, map[string]string{"field": "context_exec"}, err)
	}
	return fingerprint, nil
}

func replayContextExec(req core.Request, fingerprint string, existing operation.ContextExecState) (operation.ContextExecState, error) {
	if existing.RequestFingerprint != fingerprint {
		return operation.ContextExecState{}, failure.New(failure.OperationConflict, map[string]string{"operation_id": req.ContextExecID}, nil)
	}
	return existing.Clone(), nil
}

func (s *Service) reserveExpectation(ctx context.Context, req core.Request, fingerprint string, snapshot AuthoritySnapshot) (operation.ContextExecState, bool, error) {
	expectation, err := expectationFromAuthority(req, snapshot)
	if err != nil {
		return operation.ContextExecState{}, false, err
	}
	now := s.now().UTC()
	if now.IsZero() {
		return operation.ContextExecState{}, false, admissionFailure(req, failure.ContextExecUnavailable, "context_clock_unavailable", nil)
	}
	want := operation.ContextExecState{
		SchemaVersion: operation.ContextExecStateSchemaVersion, Request: req.Clone(), RequestFingerprint: fingerprint,
		Expectation: expectation, Lifecycle: core.LifecycleReserved, CreatedAt: now, UpdatedAt: now,
	}
	stored, created, result := s.store.ReserveContextExec(ctx, want)
	if result.Err != nil {
		return stored.Clone(), false, storeMutationError(req, result, "reserve_ambiguous")
	}
	if !created {
		got, err := replayContextExec(req, fingerprint, stored)
		return got, false, err
	}
	return stored.Clone(), true, nil
}

func (s *Service) armReserved(ctx context.Context, req core.Request, fingerprint string, reserved operation.ContextExecState, first AuthoritySnapshot) (operation.ContextExecState, error) {
	second, err := s.authority.Snapshot(ctx, req)
	if err != nil {
		return reserved.Clone(), err
	}
	if err := validateAdmission(req, second); err != nil {
		return reserved.Clone(), err
	}
	if err := validateRevalidation(req, first, second, reserved.Expectation); err != nil {
		return reserved.Clone(), err
	}
	helper, err := s.newHelperBinding(req, fingerprint)
	if err != nil {
		return reserved.Clone(), err
	}
	lease, created, result := s.store.AcquireContextExecLease(ctx, operation.SessionID(req.SessionID), req.AuthorityEpoch, req.ContextExecID, fingerprint)
	if result.Err != nil {
		return reserved.Clone(), storeMutationError(req, result, "lease_acquire_ambiguous")
	}
	if !created {
		return reserved.Clone(), admissionFailure(req, failure.ContextExecAmbiguous, "context_exec_lease_conflict", nil)
	}
	requested, result := s.store.AdvanceContextExec(ctx, req.ContextExecID, operation.ContextExecTransition{Lifecycle: core.LifecycleHelperRequested, Helper: &helper})
	if result.Err != nil {
		if result.Durability == NoDurableChange {
			if release := s.store.ReleaseContextExecLease(ctx, lease); release.Err != nil || release.Durability == AmbiguousChange {
				return reserved.Clone(), admissionFailure(req, failure.ContextExecAmbiguous, "lease_release_ambiguous", release.Err)
			}
		}
		return requested.Clone(), storeMutationError(req, result, "helper_request_ambiguous")
	}
	armReq := helperArmRequest(req, second, helper, reserved.Expectation)
	arm, armErr := s.helper.ArmContextHelper(ctx, armReq)
	if armErr != nil || !armMatches(arm, armReq.Shell) {
		ambiguous, transition := s.store.AdvanceContextExec(ctx, req.ContextExecID, operation.ContextExecTransition{Lifecycle: core.LifecycleAmbiguous})
		if transition.Err != nil {
			return requested.Clone(), admissionFailure(req, failure.ContextExecAmbiguous, "helper_arm_and_ambiguity_persist_failed", transition.Err)
		}
		return ambiguous.Clone(), admissionFailure(req, failure.ContextExecAmbiguous, "helper_arm_delivery_ambiguous", armErr)
	}
	return requested.Clone(), nil
}

func (s *Service) newHelperBinding(req core.Request, fingerprint string) (core.HelperBinding, error) {
	if s.newOpaqueLaunchID == nil || s.newHelperGeneration == nil || s.helperExecutable == "" || !filepath.IsAbs(s.helperExecutable) {
		return core.HelperBinding{}, admissionFailure(req, failure.ContextExecUnavailable, "helper_identity_allocator_unavailable", nil)
	}
	helper := core.HelperBinding{
		OpaqueLaunchID: s.newOpaqueLaunchID(), Generation: s.newHelperGeneration(), RequestFingerprint: fingerprint,
		ExecutablePath: filepath.Clean(s.helperExecutable),
	}
	if err := helper.Validate(); err != nil {
		return core.HelperBinding{}, admissionFailure(req, failure.ContextExecUnavailable, "helper_identity_invalid", err)
	}
	return helper, nil
}

func expectationFromAuthority(req core.Request, snapshot AuthoritySnapshot) (core.ContextExpectation, error) {
	shellIdentity, err := shellapp.ContextShellIdentity(snapshot.Shell)
	if err != nil {
		return core.ContextExpectation{}, admissionFailure(req, failure.ContextExecBoundaryUnproven, "shell_identity_unqualified", err)
	}
	expectation := core.ContextExpectation{
		SessionID: req.SessionID, AuthorityEpoch: req.AuthorityEpoch, ProviderGeneration: snapshot.Observation.ProviderGeneration,
		ShellIdentity: shellIdentity, CWDObserved: filepath.Clean(snapshot.Observation.CWD), PrivacyState: "standard",
	}
	if err := expectation.Validate(); err != nil {
		return core.ContextExpectation{}, admissionFailure(req, failure.ContextExecBoundaryUnproven, "context_expectation_invalid", err)
	}
	return expectation, nil
}

func validateRevalidation(req core.Request, first, second AuthoritySnapshot, expectation core.ContextExpectation) error {
	if second.Observation.ProviderGeneration != expectation.ProviderGeneration || second.Observation.ProviderGeneration != first.Observation.ProviderGeneration {
		return admissionFailure(req, failure.ContextExecStaleGeneration, "provider_generation_changed", nil)
	}
	if second.Binding.AuthorityEpoch != first.Binding.AuthorityEpoch || second.Authority.Epoch != first.Authority.Epoch {
		return admissionFailure(req, failure.ContextExecStaleGeneration, "authority_epoch_changed", nil)
	}
	secondShell, err := shellapp.ContextShellIdentity(second.Shell)
	if err != nil || secondShell != expectation.ShellIdentity {
		return admissionFailure(req, failure.ContextExecBoundaryUnproven, "shell_identity_changed", err)
	}
	if second.Observation.PanePID != first.Observation.PanePID || filepath.Clean(second.Observation.PaneTTY) != filepath.Clean(first.Observation.PaneTTY) {
		return admissionFailure(req, failure.ContextExecBoundaryUnproven, "pane_identity_changed", nil)
	}
	if !sameContextDirectoryIdentity(second.Observation.CWD, expectation.CWDObserved) {
		return admissionFailure(req, failure.ContextExecBoundaryUnproven, "cwd_changed", nil)
	}
	return nil
}

func helperArmRequest(req core.Request, snapshot AuthoritySnapshot, helper core.HelperBinding, expectation core.ContextExpectation) HelperArmRequest {
	obs := snapshot.Observation
	shellReq := shellapp.ContextHelperArmRequest{
		ContextExecID: req.ContextExecID, SessionID: req.SessionID, Authority: snapshot.Authority,
		Facts: shellapp.ProviderProcessFacts{
			SessionID: req.SessionID, ProviderID: obs.Provider.ID, ProviderVersion: obs.Provider.Version,
			ProviderGeneration: obs.ProviderGeneration, PanePID: obs.PanePID, CurrentCommand: obs.CurrentCommand,
			PaneTTY: filepath.Clean(obs.PaneTTY), CWD: filepath.Clean(obs.CWD),
		},
		ExpectedShell: snapshot.Shell, OpaqueLaunchID: helper.OpaqueLaunchID,
	}
	return HelperArmRequest{ProviderRef: snapshot.ProviderRef, Helper: helper, Expectation: expectation, Shell: shellReq}
}

func armMatches(arm shellapp.ContextHelperArm, req shellapp.ContextHelperArmRequest) bool {
	return arm.ContextExecID == req.ContextExecID && arm.SessionID == req.SessionID && arm.AuthorityEpoch == req.Authority.Epoch &&
		arm.ProviderGeneration == req.Facts.ProviderGeneration && arm.Shell == req.ExpectedShell && arm.PaneShellPID == req.Facts.PanePID &&
		filepath.Clean(arm.PaneTTY) == filepath.Clean(req.Facts.PaneTTY) && arm.OpaqueLaunchID == req.OpaqueLaunchID && !arm.ArmedAt.IsZero()
}

func (s *Service) BindClaim(
	ctx context.Context,
	contextExecID string,
	helper core.HelperBinding,
	finalContext core.ContextBinding,
	continuity core.ShellContinuityExpectation,
	proof core.ShellContinuityProof,
	boundaryObservedAt time.Time,
	verifierDigest string,
) (operation.ContextExecState, error) {
	if s == nil || s.store == nil || s.authority == nil {
		return operation.ContextExecState{}, failure.New(failure.ContextExecUnavailable, map[string]string{"context_exec_id": contextExecID, "reason": "claim_service_unavailable"}, nil)
	}
	state, found, err := s.store.LookupContextExec(ctx, contextExecID)
	if err != nil {
		return operation.ContextExecState{}, err
	}
	if !found {
		return operation.ContextExecState{}, failure.New(failure.ContextHelperAuthFailed, map[string]string{"context_exec_id": contextExecID, "reason": "reservation_missing"}, nil)
	}
	req := state.Request
	if state.Lifecycle != core.LifecycleHelperRequested || state.Helper == nil || *state.Helper != helper || helper.RequestFingerprint != state.RequestFingerprint {
		return state.Clone(), admissionFailure(req, failure.ContextHelperAuthFailed, "helper_generation_mismatch", nil)
	}
	if boundaryObservedAt.IsZero() {
		return state.Clone(), admissionFailure(req, failure.ContextExecBoundaryUnproven, "boundary_time_missing", nil)
	}
	wantContext := core.ContextBinding{
		SessionID: state.Expectation.SessionID, AuthorityEpoch: state.Expectation.AuthorityEpoch,
		ShellIdentity: state.Expectation.ShellIdentity, BoundaryQuality: "shell_prompt",
		CWDObserved: state.Expectation.CWDObserved, PrivacyState: state.Expectation.PrivacyState,
	}
	if err := finalContext.Validate(); err != nil || finalContext != wantContext {
		return state.Clone(), admissionFailure(req, failure.ContextExecBoundaryUnproven, "final_context_mismatch", err)
	}
	claim, err := s.authority.ClaimSnapshot(ctx, req)
	if err != nil {
		return state.Clone(), err
	}
	if err := validateClaimAdmission(req, claim); err != nil {
		return state.Clone(), err
	}
	if err := validateClaimContinuity(req, state, helper, continuity, proof, claim); err != nil {
		return state.Clone(), err
	}
	bound, result := s.store.BindHelperGeneration(ctx, contextExecID, helper, finalContext, boundaryObservedAt.UTC(), verifierDigest)
	if result.Err != nil {
		return bound.Clone(), storeMutationError(req, result, "helper_claim_binding_ambiguous")
	}
	if err := bound.Validate(); err != nil || bound.Lifecycle != core.LifecycleHelperAuthenticated || bound.Helper == nil || *bound.Helper != helper || bound.Context == nil || *bound.Context != finalContext || !bound.BoundaryObservedAt.Equal(boundaryObservedAt.UTC()) {
		return bound.Clone(), admissionFailure(req, failure.ContextExecAmbiguous, "helper_claim_durable_mismatch", err)
	}
	return bound.Clone(), nil
}

func validateClaimContinuity(
	req core.Request,
	state operation.ContextExecState,
	helper core.HelperBinding,
	continuity core.ShellContinuityExpectation,
	proof core.ShellContinuityProof,
	claim ClaimAuthoritySnapshot,
) error {
	if err := continuity.Validate(); err != nil {
		return admissionFailure(req, failure.ContextExecBoundaryUnproven, "shell_continuity_expectation_invalid", err)
	}
	if err := proof.ValidateFor(continuity); err != nil {
		return admissionFailure(req, failure.ContextExecBoundaryUnproven, "shell_continuity_unproven", err)
	}
	if claim.Observation.ProviderGeneration != state.Expectation.ProviderGeneration || claim.PrivacyProviderGeneration != state.Expectation.ProviderGeneration {
		return admissionFailure(req, failure.ContextExecStaleGeneration, "provider_generation_changed", nil)
	}
	if continuity.SessionID != req.SessionID || continuity.AuthorityEpoch != req.AuthorityEpoch || continuity.ProviderGeneration != state.Expectation.ProviderGeneration {
		return admissionFailure(req, failure.ContextExecStaleGeneration, "shell_continuity_generation_changed", nil)
	}
	if continuity.ShellRuntimeIdentity != state.Expectation.ShellIdentity || filepath.Clean(continuity.HelperExecutableIdentity) != filepath.Clean(helper.ExecutablePath) {
		return admissionFailure(req, failure.ContextExecBoundaryUnproven, "shell_continuity_reservation_mismatch", nil)
	}
	if continuity.PaneShellPID != claim.Observation.PanePID || filepath.Clean(continuity.PaneTTY) != filepath.Clean(claim.Observation.PaneTTY) {
		return admissionFailure(req, failure.ContextExecBoundaryUnproven, "pane_identity_changed", nil)
	}
	if !sameContextDirectoryIdentity(claim.Observation.CWD, state.Expectation.CWDObserved) {
		return admissionFailure(req, failure.ContextExecBoundaryUnproven, "cwd_changed", nil)
	}
	return nil
}

func sameContextDirectoryIdentity(observed, expected string) bool {
	observed = filepath.Clean(observed)
	expected = filepath.Clean(expected)
	if observed == expected {
		return true
	}
	observedInfo, err := os.Stat(observed)
	if err != nil || !observedInfo.IsDir() {
		return false
	}
	expectedInfo, err := os.Stat(expected)
	if err != nil || !expectedInfo.IsDir() {
		return false
	}
	return os.SameFile(observedInfo, expectedInfo)
}

func validateClaimAdmission(req core.Request, snapshot ClaimAuthoritySnapshot) error {
	if err := snapshot.Binding.Validate(); err != nil || snapshot.Binding.SessionID != req.SessionID || snapshot.Binding.Lifecycle != delegated.LifecycleLive {
		return admissionFailure(req, failure.ContextExecUnavailable, "delegated_session_not_live", err)
	}
	if err := snapshot.ProviderRef.Validate(); err != nil || snapshot.ProviderRef.SessionID != req.SessionID || snapshot.ProviderRef.ProviderID != snapshot.Binding.ProviderID || snapshot.ProviderRef.ProviderVersion != snapshot.Binding.ProviderVersion {
		return admissionFailure(req, failure.ContextExecStaleGeneration, "provider_ref_changed", err)
	}
	obs := snapshot.Observation
	if obs.Provider != snapshot.Binding.ProviderIdentity() || !obs.ProviderCurrent || obs.ProviderGeneration == "" {
		return admissionFailure(req, failure.ContextExecStaleGeneration, "provider_generation_unproven", nil)
	}
	if snapshot.PrivacyProviderGeneration == "" || snapshot.PrivacyProviderGeneration != obs.ProviderGeneration {
		return admissionFailure(req, failure.ContextExecStaleGeneration, "privacy_generation_changed", nil)
	}
	if snapshot.Binding.AuthorityEpoch != req.AuthorityEpoch || snapshot.Authority.Epoch != req.AuthorityEpoch {
		return admissionFailure(req, failure.ContextExecStaleGeneration, "authority_epoch_changed", nil)
	}
	if snapshot.Binding.DesiredOwner != delegated.OwnerAgent || obs.Owner != delegated.OwnerAgent || snapshot.Authority.Owner != delegated.OwnerAgent || snapshot.Authority.Fenced || !snapshot.AgentIngressWritable || snapshot.OwnershipTransferActive {
		return admissionFailure(req, failure.ContextExecNotAgentOwned, "agent_authority_unproven", nil)
	}
	if snapshot.PrivacyActive || snapshot.PrivacyReleasePending {
		return admissionFailure(req, failure.ContextExecPrivacyBlocked, "privacy_active_or_pending", nil)
	}
	facts := shellapp.ProviderProcessFacts{
		SessionID: req.SessionID, ProviderID: obs.Provider.ID, ProviderVersion: obs.Provider.Version,
		ProviderGeneration: obs.ProviderGeneration, PanePID: obs.PanePID, CurrentCommand: obs.CurrentCommand,
		PaneTTY: obs.PaneTTY, CWD: obs.CWD,
	}
	if err := facts.Validate(); err != nil || !filepath.IsAbs(obs.PaneTTY) || !strings.HasPrefix(filepath.Clean(obs.PaneTTY), "/dev/") || !filepath.IsAbs(obs.CWD) {
		return admissionFailure(req, failure.ContextExecBoundaryUnproven, "context_facts_unproven", err)
	}
	return nil
}

func validateAdmission(req core.Request, snapshot AuthoritySnapshot) error {
	if err := validateClaimAdmission(req, snapshot.ClaimAuthoritySnapshot); err != nil {
		return err
	}
	if _, err := shellapp.ContextShellIdentity(snapshot.Shell); err != nil {
		return admissionFailure(req, failure.ContextExecBoundaryUnproven, "shell_identity_unqualified", err)
	}
	return nil
}

func storeMutationError(req core.Request, result MutationResult, ambiguousReason string) error {
	if result.Durability == AmbiguousChange {
		return admissionFailure(req, failure.ContextExecAmbiguous, ambiguousReason, result.Err)
	}
	return result.Err
}

func admissionFailure(req core.Request, code failure.Code, reason string, cause error) error {
	if reason == "" {
		reason = "admission_failed"
	}
	details := map[string]string{"context_exec_id": req.ContextExecID, "session_id": req.SessionID, "reason": reason}
	if req.AuthorityEpoch > 0 {
		details["authority_epoch"] = fmt.Sprint(req.AuthorityEpoch)
	}
	return failure.New(code, details, cause)
}
