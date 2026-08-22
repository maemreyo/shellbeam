package daemon

import (
	"context"
	"fmt"

	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	trace "github.com/maemreyo/shellbeam/internal/core/inputtrace"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	persistentsession "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/session"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func invalidIntentFailure(err error) error {
	field := "command"
	switch err.Error() {
	case "cwd must be absolute":
		field = "cwd"
	case "timeout must be non-negative":
		field = "timeout_ms"
	}
	return failure.New(failure.InvalidInput, map[string]string{"field": field}, err)
}

func (s *Service) lookupV2Replay(ctx context.Context, req StartRequest, id operation.ID, intent operation.Intent) (View, bool, error) {
	if req.ProtocolVersion != 2 {
		return View{}, false, nil
	}
	requestFingerprint, err := intent.RequestFingerprint()
	if err != nil {
		return View{}, true, invalidIntentFailure(err)
	}
	stored, found, err := s.store.FindOperation(ctx, id)
	if err != nil {
		return View{}, true, failure.Normalize(err)
	}
	if !found {
		return View{}, false, nil
	}
	if stored.EffectiveRequestFingerprint() != requestFingerprint {
		return View{}, true, failure.New(failure.OperationConflict, map[string]string{"operation_id": string(id)}, nil)
	}
	structuredAdapter, err := normalizedStructuredAdapter(req)
	if err != nil {
		return View{}, true, err
	}
	observationFingerprint, err := (operation.ObservationBinding{ActivityID: req.ActivityID, Intent: req.Intent, StructuredAdapter: structuredAdapter, Evidence: req.Evidence}).Fingerprint()
	if err != nil {
		return View{}, true, invalidIntentFailure(err)
	}
	if stored.ObservationBindingFingerprint != observationFingerprint {
		return View{}, true, failure.New(failure.OperationMetadataConflict, map[string]string{"operation_id": string(id)}, nil)
	}
	if stored.SessionMode == delegated.ModeDelegatedInteractive {
		if binding, bindErr := s.delegatedStore().LoadDelegatedBinding(ctx, stored.SessionID); bindErr == nil {
			resume := binding.Lifecycle == delegated.LifecycleProvisioning
			if live := s.get(string(stored.SessionID)); live != nil {
				live.mu.Lock()
				resume = resume || (binding.Lifecycle == delegated.LifecycleLive && live.state == session.Starting)
				live.mu.Unlock()
			}
			if resume {
				view, resumeErr := s.resumeDelegatedReservedStart(ctx, req, stored)
				return view, true, resumeErr
			}
		}
	}
	view, err := s.waitView(ctx, stored, string(stored.SessionID), 0, req.YieldMS, req.MaxOutputBytes)
	if err == nil {
		s.enrichReplayWorkspaceContext(ctx, &view, stored.CWD, req.WorkspaceHint)
		s.enrichStructuredAdapterAvailability(&view, stored)
	}
	return view, true, err
}

func (s *Service) resolveStartIntent(ctx context.Context, req StartRequest) (operation.Intent, error) {
	intent := operation.Intent{Command: req.Command, Argv: append([]string(nil), req.Argv...), WorkspaceID: req.WorkspaceID, CWD: req.CWD, TTY: req.TTY, TimeoutMS: req.TimeoutMS, Persistent: req.Persistent, SessionName: req.SessionName, StdinMode: req.StdinMode, TimeoutMode: req.TimeoutMode, TraceMode: req.TraceMode, ResourceLimits: req.ResourceLimits.Clone(), SessionMode: req.SessionMode}
	if req.ProtocolVersion != 2 || req.WorkspaceID == "" {
		intent.ResolvedCWD = req.CWD
		return intent, nil
	}
	address := workspace.Address{WorkspaceID: workspace.WorkspaceID(req.WorkspaceID), CWD: req.CWD}
	if err := address.Validate(); err != nil {
		return operation.Intent{}, invalidIntentFailure(err)
	}
	if s.resolver == nil {
		return operation.Intent{}, failure.New(failure.FeatureUnavailable, map[string]string{"feature": "workspace_addressing"}, nil)
	}
	resolved, err := s.resolver.ResolveAddress(ctx, address)
	if err != nil {
		return operation.Intent{}, failure.Normalize(err)
	}
	if resolved.WorkspaceID != address.WorkspaceID || resolved.CWD == "" {
		return operation.Intent{}, failure.New(failure.Internal, nil, fmt.Errorf("workspace resolver identity mismatch"))
	}
	intent.CWD = resolved.LogicalCWD
	intent.ResolvedCWD = resolved.CWD
	return intent, nil
}

func validateResourceLimits(catalog capability.Catalog, req StartRequest) error {
	if req.ResourceLimits == nil {
		return nil
	}
	if err := req.ResourceLimits.Validate(); err != nil {
		return failure.New(failure.InvalidInput, map[string]string{"field": "limits"}, err)
	}
	if req.ProtocolVersion != 2 {
		return failure.New(failure.ResourceLimitUnsupported, map[string]string{"metric": "resource_limits", "reason": "protocol_v2_required"}, nil)
	}
	if req.Persistent {
		return failure.New(failure.ResourceLimitUnsupported, map[string]string{"metric": "resource_limits", "reason": "persistent"}, nil)
	}
	support := catalog.ResourceEnforcement
	if catalog.Features[capability.FeatureResourceEnforcement] != capability.Available || support == nil || !support.ValidV1() {
		return failure.New(failure.ResourceLimitUnsupported, map[string]string{"metric": "resource_limits", "reason": "provider_unavailable"}, nil)
	}
	if req.ResourceLimits.MemoryBytes > 0 && support.MemoryBytes != capability.EnforcementHard {
		return failure.New(failure.ResourceLimitUnsupported, map[string]string{"metric": "memory_bytes", "reason": "hard_unsupported"}, nil)
	}
	if req.ResourceLimits.Processes > 0 && support.Processes != capability.EnforcementHard {
		return failure.New(failure.ResourceLimitUnsupported, map[string]string{"metric": "processes", "reason": "hard_unsupported"}, nil)
	}
	if req.ResourceLimits.CPUTimeMS > 0 && support.CPUTimeMS != capability.EnforcementHard {
		return failure.New(failure.ResourceLimitUnsupported, map[string]string{"metric": "cpu_time_ms", "reason": "hard_unsupported"}, nil)
	}
	return nil
}

func validateStartMetadata(req StartRequest) error {
	traceMode, err := trace.NormalizeMode(req.TraceMode)
	if err != nil {
		return failure.New(failure.InvalidInput, map[string]string{"field": "trace_mode"}, err)
	}
	if traceMode != trace.ModeOff {
		if req.ProtocolVersion != 2 {
			return failure.New(failure.InputTraceUnsupported, map[string]string{"reason": "protocol_v2_required"}, nil)
		}
		if req.TTY {
			return failure.New(failure.InputTraceUnsupported, map[string]string{"reason": "tty"}, nil)
		}
		if req.Persistent {
			return failure.New(failure.InputTraceUnsupported, map[string]string{"reason": "persistent"}, nil)
		}
	}
	if req.SessionMode == "" {
		if (req.Persistent || req.SessionName != "") && req.ProtocolVersion != 2 {
			return failure.New(failure.FeatureUnavailable, map[string]string{"feature": "named_sessions", "required_version": "2"}, nil)
		}
		if req.SessionName != "" && !req.Persistent {
			return failure.New(failure.InvalidInput, map[string]string{"field": "session_name"}, fmt.Errorf("session name requires persistent execution"))
		}
		if req.Persistent && req.TTY {
			return failure.New(failure.FeatureUnavailable, map[string]string{"feature": "persistent_tty"}, nil)
		}
	}
	if req.SessionName != "" {
		if err := persistentsession.ValidateSessionName(req.SessionName); err != nil {
			return failure.New(failure.InvalidInput, map[string]string{"field": "session_name"}, err)
		}
	}
	if req.WorkspaceHint != nil {
		if err := req.WorkspaceHint.Validate(); err != nil {
			return failure.New(failure.InvalidInput, map[string]string{"field": "workspace_hint"}, err)
		}
	}
	if req.Intent != nil {
		if err := req.Intent.Validate(); err != nil {
			return failure.New(failure.InvalidInput, map[string]string{"field": "intent"}, err)
		}
	}
	if req.Evidence != nil {
		if req.ProtocolVersion != 2 {
			return failure.New(failure.FeatureUnavailable, map[string]string{"feature": "evidence", "required_version": "2"}, nil)
		}
		normalized, err := req.Evidence.Normalize()
		if err != nil {
			return failure.New(failure.InvalidInput, map[string]string{"field": "evidence"}, err)
		}
		if len(normalized.ExpectedOutputs) > 0 && req.WorkspaceID == "" {
			return failure.New(failure.InvalidInput, map[string]string{"field": "workspace_id"}, fmt.Errorf("evidence expected outputs require workspace"))
		}
		if wantsProjectCommand(req) {
			return failure.New(failure.InvalidInput, map[string]string{"field": "evidence"}, fmt.Errorf("typed project commands use frozen project evidence metadata"))
		}
	}
	return nil
}

func (s *Service) prepareStartReservation(ctx context.Context, req StartRequest, id operation.ID) (operation.Reservation, operation.ExecutionSpec, error) {
	intent, err := s.resolveStartIntent(ctx, req)
	if err != nil {
		return operation.Reservation{}, operation.ExecutionSpec{}, err
	}
	mode, err := intent.ExecutionMode()
	if err != nil {
		return operation.Reservation{}, operation.ExecutionSpec{}, invalidIntentFailure(err)
	}
	executionCWD := intent.ResolvedCWD
	if executionCWD == "" {
		executionCWD = req.CWD
	}
	resolved, err := s.resolveExecutionPolicy(req)
	if err != nil {
		return operation.Reservation{}, operation.ExecutionSpec{}, invalidIntentFailure(err)
	}
	intent.Resolved, intent.TimeoutSource = &resolved, timeoutSourceOf(resolved)
	spec := bindExecution(s.owner, operation.ExecutionSpec{Mode: mode, Shell: s.options.Shell, Command: req.Command, Argv: append([]string(nil), req.Argv...), CWD: executionCWD, TTY: req.TTY, TimeoutMS: resolved.TimeoutMS, StdinMode: resolved.StdinMode, ResourceLimits: req.ResourceLimits.Clone()})
	reservation, err := s.reservationForStart(req, id, intent, spec)
	if err != nil {
		return operation.Reservation{}, operation.ExecutionSpec{}, invalidIntentFailure(err)
	}
	return reservation, spec, nil
}

func normalizedStructuredAdapter(req StartRequest) (string, error) {
	return normalizedStructuredAdapterForArgv(req, req.Argv)
}

func normalizedStructuredAdapterForArgv(req StartRequest, argv []string) (string, error) {
	if req.StructuredAdapter != "" {
		if !operation.ValidStructuredAdapterID(req.StructuredAdapter) {
			return "", failure.New(failure.InvalidInput, map[string]string{"field": "structured_adapter"}, fmt.Errorf("invalid structured adapter"))
		}
		return req.StructuredAdapter, nil
	}
	selection := structuredapp.SelectAdapter("", argv)
	if selection.Status == structuredapp.SelectionSelected {
		return selection.AdapterID, nil
	}
	return "", nil
}

func (s *Service) waitReservedStart(ctx context.Context, req StartRequest, stored operation.Reservation, sessionID string) (View, error) {
	view, err := s.waitView(ctx, stored, sessionID, 0, req.YieldMS, req.MaxOutputBytes)
	if err == nil {
		s.enrichReplayWorkspaceContext(ctx, &view, stored.CWD, req.WorkspaceHint)
		s.enrichStructuredAdapterAvailability(&view, stored)
	}
	return view, err
}
