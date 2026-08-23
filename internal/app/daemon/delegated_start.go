package daemon

import (
	"context"
	"fmt"
	"time"

	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/inputtrace"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func (s *Service) validateDelegatedStart(ctx context.Context, req StartRequest) (bool, error) {
	if req.SessionMode == "" {
		return false, nil
	}
	if req.ProtocolVersion != 2 || req.SessionMode != delegated.ModeDelegatedInteractive {
		return true, failure.New(failure.FeatureUnavailable, map[string]string{"feature": "delegated_interactive", "session_mode": req.SessionMode, "required_version": "2"}, nil)
	}
	if s.options.DelegatedRuntime == nil {
		return true, failure.New(failure.FeatureUnavailable, map[string]string{"feature": "delegated_interactive", "reason": "provider_unavailable"}, nil)
	}
	if _, ok := s.store.(DelegatedSessionStore); !ok {
		return true, failure.New(failure.PersistenceUnavailable, nil, fmt.Errorf("delegated session store unavailable"))
	}
	if req.TTY {
		return true, failure.New(failure.InvalidInput, map[string]string{"field": "tty"}, fmt.Errorf("delegated interactive uses provider PTY semantics"))
	}
	if req.Persistent {
		return true, failure.New(failure.InvalidInput, map[string]string{"field": "persistent"}, fmt.Errorf("delegated interactive is its own session mode"))
	}
	if req.Evidence != nil {
		return true, failure.New(failure.InvalidInput, map[string]string{"field": "evidence"}, fmt.Errorf("delegated lifecycle is not ordinary verification evidence"))
	}
	if req.ResourceLimits != nil {
		return true, failure.New(failure.ResourceLimitUnsupported, map[string]string{"metric": "resource_limits", "reason": "delegated_interactive"}, nil)
	}
	if req.Hermetic != nil {
		return true, failure.New(failure.InvalidInput, map[string]string{"field": "hermetic"}, fmt.Errorf("delegated interactive does not qualify hermetic v1"))
	}
	if req.VerificationAttempt != nil {
		return true, failure.New(failure.InvalidInput, map[string]string{"field": "verification_attempt"}, fmt.Errorf("delegated lifecycle is not ordinary verification evidence"))
	}
	if err := req.StdinMode.Validate(); err != nil {
		return true, failure.New(failure.InvalidInput, map[string]string{"field": "stdin_mode"}, err)
	}
	if req.StdinMode == operation.StdinModeClosed {
		return true, failure.New(failure.InvalidInput, map[string]string{"field": "stdin_mode"}, fmt.Errorf("delegated interactive requires stream stdin in H1"))
	}
	if err := req.TimeoutMode.Validate(); err != nil {
		return true, failure.New(failure.InvalidInput, map[string]string{"field": "timeout_mode"}, err)
	}
	if req.TimeoutMS != 0 || req.TimeoutMode == operation.TimeoutModeFinite || req.TimeoutMode == operation.TimeoutModeDefault {
		return true, failure.New(failure.InvalidInput, map[string]string{"field": "timeout_mode"}, fmt.Errorf("bounded delegated timeout is not qualified in H1"))
	}
	traceMode, err := inputtrace.NormalizeMode(req.TraceMode)
	if err != nil {
		return true, failure.New(failure.InvalidInput, map[string]string{"field": "trace_mode"}, err)
	}
	if traceMode != inputtrace.ModeOff {
		return true, failure.New(failure.InputTraceUnsupported, map[string]string{"reason": "delegated_interactive"}, nil)
	}
	if err := s.options.DelegatedRuntime.Probe(ctx); err != nil {
		return true, err
	}
	return true, nil
}

type delegatedSessionSink struct {
	service *Service
	id      string
}

func (sink delegatedSessionSink) Append(b []byte) error {
	_, result := sink.service.store.AppendOutput(context.Background(), operation.SessionID(sink.id), b)
	if result.Err != nil {
		return result.Err
	}
	if live := sink.service.get(sink.id); live != nil {
		live.mu.Lock()
		live.outputBytes += int64(len(b))
		live.notify()
		live.mu.Unlock()
	}
	return nil
}

func (s *Service) admitPreparedDelegatedStart(ctx context.Context, req StartRequest, reservation operation.Reservation, spec operation.ExecutionSpec, commit reservationCommitter, typedReplay bool) (View, error) {
	sid := newSessionID()
	now := time.Now().UTC()
	reservation.SessionID = operation.SessionID(sid)
	reservation.CreatedAt = now
	reservation.SchemaVersion = 5
	reservation.SessionMode = delegated.ModeDelegatedInteractive
	reservation.AuthorityEpoch = 1
	s.freezeEnvironmentBinding(&reservation)
	stored, created, result := commit(ctx, reservation)
	if result.Err != nil {
		return View{}, failure.Normalize(result.Err)
	}
	sid = string(stored.SessionID)
	if !created {
		if typedReplay {
			return s.waitTypedReservedStart(ctx, req, stored, sid)
		}
		return s.waitReservedStart(ctx, req, stored, sid)
	}

	runtime := s.options.DelegatedRuntime
	if runtime == nil {
		return View{}, failure.New(failure.FeatureUnavailable, map[string]string{"feature": "delegated_interactive", "reason": "provider_unavailable"}, nil)
	}
	ref, err := runtime.ProviderRefForSession(sid, now)
	if err != nil {
		return View{}, failure.Normalize(err)
	}
	provider := runtime.Identity()
	binding := delegated.Binding{SchemaVersion: delegated.BindingSchemaVersion, SessionID: sid, OperationID: req.OperationID, SessionName: req.SessionName, SessionMode: delegated.ModeDelegatedInteractive, AuthorityEpoch: 1, DesiredOwner: delegated.OwnerAgent, ProviderID: provider.ID, ProviderVersion: provider.Version, Lifecycle: delegated.LifecycleProvisioning, CreatedAt: now, UpdatedAt: now}
	binding, _, bindResult := s.delegatedStore().ReserveDelegatedBinding(ctx, binding, ref)
	if bindResult.Err != nil {
		return View{}, failure.Normalize(bindResult.Err)
	}

	return s.startDelegatedProvider(ctx, req, stored, spec, binding, ref)
}

func (s *Service) resumeDelegatedReservedStart(ctx context.Context, req StartRequest, stored operation.Reservation) (View, error) {
	sid := string(stored.SessionID)
	store := s.delegatedStore()
	if store == nil {
		return View{}, failure.New(failure.PersistenceUnavailable, nil, fmt.Errorf("delegated session store unavailable"))
	}
	binding, err := store.LoadDelegatedBinding(ctx, stored.SessionID)
	if err != nil {
		return View{}, failure.Normalize(err)
	}
	if binding.Lifecycle != delegated.LifecycleProvisioning && binding.Lifecycle != delegated.LifecycleLive {
		return s.waitView(ctx, stored, sid, 0, req.YieldMS, req.MaxOutputBytes)
	}
	ref, err := store.LoadDelegatedProviderRef(ctx, stored.SessionID)
	if err != nil {
		return View{}, failure.Normalize(err)
	}
	spec, err := s.delegatedSpecFromStored(req, stored)
	if err != nil {
		return View{}, err
	}
	return s.startDelegatedProvider(ctx, req, stored, spec, binding, ref)
}

func (s *Service) delegatedSpecFromStored(req StartRequest, stored operation.Reservation) (operation.ExecutionSpec, error) {
	resolved, err := s.resolveExecutionPolicy(req)
	if err != nil {
		return operation.ExecutionSpec{}, invalidIntentFailure(err)
	}
	return operation.ExecutionSpec{
		Mode: stored.ExecutionMode, Shell: stored.Shell, Executable: stored.Executable,
		Command: stored.Command, Argv: append([]string(nil), stored.Argv...), CWD: stored.CWD,
		TTY: false, TimeoutMS: stored.TimeoutMS, StdinMode: resolved.StdinMode,
	}, nil
}

func (s *Service) startDelegatedProvider(ctx context.Context, req StartRequest, stored operation.Reservation, spec operation.ExecutionSpec, binding delegated.Binding, ref delegated.ProviderRef) (View, error) {
	sid := string(stored.SessionID)
	runtime := s.options.DelegatedRuntime
	if runtime == nil {
		return View{}, failure.New(failure.FeatureUnavailable, map[string]string{"feature": "delegated_interactive", "reason": "provider_unavailable"}, nil)
	}
	executionCWD := stored.CWD
	workspaceObservation := s.captureWorkspace(ctx, executionCWD)
	activityReq := req
	activityReq.CWD = executionCWD
	spec.CWD = executionCWD
	live := s.getOrCreateDelegatedLive(req, stored, spec, binding, ref, workspaceObservation)
	live.delegatedStartMu.Lock()
	defer live.delegatedStartMu.Unlock()
	var alreadyRunning bool
	workspaceObservation, spec, alreadyRunning, err := validateDelegatedProvisioningLive(live, req, binding, ref)
	if err != nil {
		return View{}, err
	}
	if alreadyRunning {
		return s.waitView(ctx, stored, sid, 0, req.YieldMS, req.MaxOutputBytes)
	}

	createdSession, createErr := runtime.Create(context.Background(), delegatedapp.CreateRequest{ProviderRef: ref, SessionID: sid, OperationID: req.OperationID, SessionName: stored.SessionName, Spec: spec, Output: delegatedSessionSink{service: s, id: sid}})
	if createErr != nil {
		// Keep the same provisioning live object. A provider may have accepted
		// Create and emitted output before its response was lost; the durable sink
		// and in-memory byte accounting must survive the idempotent retry.
		return View{}, createErr
	}
	if err := validateDelegatedCreateResult(sid, binding, ref, createdSession); err != nil {
		s.remove(sid)
		return View{}, err
	}
	obs := createdSession.Observation
	authority := delegated.ReconcileAuthority(delegated.ReconcileInput{Epoch: binding.AuthorityEpoch, DesiredOwner: binding.DesiredOwner, ObservedOwner: obs.Owner, ProviderIdentity: obs.Provider, ProviderCurrent: obs.ProviderCurrent})
	if authority.Fenced || authority.Owner != delegated.OwnerAgent {
		s.remove(sid)
		return View{}, failure.New(failure.DelegatedReconcileBlocked, map[string]string{"session_id": sid, "provider_id": binding.ProviderID, "current_epoch": fmt.Sprint(binding.AuthorityEpoch), "reason": "provider_authority_unproven"}, nil)
	}
	if binding.Lifecycle == delegated.LifecycleProvisioning {
		binding.Lifecycle = delegated.LifecycleLive
		binding.UpdatedAt = time.Now().UTC()
		if !binding.UpdatedAt.After(binding.CreatedAt) {
			binding.UpdatedAt = binding.CreatedAt.Add(time.Nanosecond)
		}
		if result := s.delegatedStore().AdvanceDelegatedBinding(context.Background(), binding); result.Err != nil {
			return View{}, failure.Normalize(result.Err)
		}
	}
	live.mu.Lock()
	live.delegatedBinding = binding
	live.spawn.Succeeded = true
	live.mu.Unlock()
	if got := s.store.AdvanceSession(context.Background(), session.Snapshot{SchemaVersion: 1, OperationID: req.OperationID, SessionID: sid, DaemonIncarnation: s.options.Incarnation, State: session.Running, OutputAvailable: true, UpdatedAt: time.Now().UTC()}); got.Err != nil {
		return View{}, failure.Normalize(got.Err)
	}
	live.mu.Lock()
	live.state = session.Running
	live.notify()
	live.mu.Unlock()
	s.beginDelegatedManagedShell(live)
	if activityID := s.admitActivity(ctx, activityReq, sid, workspaceObservation.binding); activityID != "" {
		live.mu.Lock()
		live.activityID = activityID
		live.mu.Unlock()
	}
	s.startDelegatedWait(live)
	view, viewErr := s.waitView(ctx, stored, sid, 0, req.YieldMS, req.MaxOutputBytes)
	s.decorateNewStartView(&view, viewErr, stored, workspaceObservation, req.WorkspaceHint)
	return view, viewErr
}

func (s *Service) getOrCreateDelegatedLive(req StartRequest, stored operation.Reservation, spec operation.ExecutionSpec, binding delegated.Binding, ref delegated.ProviderRef, workspaceObservation workspaceObservation) *liveSession {
	candidate := &liveSession{operationID: req.OperationID, activityID: req.ActivityID, sessionID: string(stored.SessionID), reservation: stored, spec: spec, workspace: workspaceObservation, state: session.Starting, spawn: receipt.SpawnEvidence{Attempted: true}, changed: make(chan struct{}), done: make(chan struct{}), delegated: true, delegatedRef: ref, delegatedBinding: binding}
	s.mu.Lock()
	live := s.live[string(stored.SessionID)]
	if live == nil {
		s.live[string(stored.SessionID)] = candidate
		live = candidate
	}
	s.mu.Unlock()
	return live
}

func validateDelegatedProvisioningLive(live *liveSession, req StartRequest, binding delegated.Binding, ref delegated.ProviderRef) (workspaceObservation, operation.ExecutionSpec, bool, error) {
	live.mu.Lock()
	defer live.mu.Unlock()
	if !live.delegated || live.delegatedRef != ref || live.operationID != req.OperationID {
		return workspaceObservation{}, operation.ExecutionSpec{}, false, failure.New(failure.DelegatedReconcileBlocked, map[string]string{"session_id": live.sessionID, "provider_id": binding.ProviderID, "current_epoch": fmt.Sprint(binding.AuthorityEpoch), "reason": "live_provisioning_identity_mismatch"}, nil)
	}
	if live.state == session.Running {
		return live.workspace, live.spec, true, nil
	}
	if live.state != session.Starting {
		return workspaceObservation{}, operation.ExecutionSpec{}, false, failure.New(failure.DelegatedReconcileBlocked, map[string]string{"session_id": live.sessionID, "provider_id": binding.ProviderID, "current_epoch": fmt.Sprint(binding.AuthorityEpoch), "reason": "unexpected_live_state_" + string(live.state)}, nil)
	}
	return live.workspace, live.spec, false, nil
}

func validateDelegatedCreateResult(sid string, binding delegated.Binding, ref delegated.ProviderRef, created delegatedapp.CreateResult) error {
	if created.ProviderRef != ref {
		return failure.New(failure.DelegatedProviderMismatch, map[string]string{"session_id": sid, "provider_id": created.ProviderRef.ProviderID, "provider_version": fmt.Sprint(created.ProviderRef.ProviderVersion), "expected_provider_id": ref.ProviderID, "expected_provider_version": fmt.Sprint(ref.ProviderVersion)}, nil)
	}
	if created.Observation.Provider != binding.ProviderIdentity() {
		return failure.New(failure.DelegatedProviderMismatch, map[string]string{"session_id": sid, "provider_id": created.Observation.Provider.ID, "provider_version": fmt.Sprint(created.Observation.Provider.Version), "expected_provider_id": binding.ProviderID, "expected_provider_version": fmt.Sprint(binding.ProviderVersion)}, nil)
	}
	return nil
}

func (s *Service) beginDelegatedManagedShell(live *liveSession) {
	if s.coherence == nil {
		return
	}
	lease := s.coherence.BeginManagedShell()
	live.mu.Lock()
	if live.coherenceLease == nil {
		live.coherenceLease = lease
		lease = nil
	}
	live.mu.Unlock()
	if lease != nil {
		lease.End()
	}
}

func (s *Service) delegatedStore() DelegatedSessionStore {
	store, _ := s.store.(DelegatedSessionStore)
	return store
}

type delegatedTerminalSnapshot struct {
	accepted     int64
	delivered    int64
	signal       receipt.SignalEvidence
	target       session.State
	outputBytes  int64
	observerBase int64
	captureGap   bool
	captureTruth receipt.CaptureTruth
	binding      delegated.Binding
}

type delegatedTerminalDecision struct {
	state            session.State
	outcome          session.Outcome
	failureReason    string
	captureQuality   receipt.CaptureQuality
	captureReasons   []receipt.CaptureReason
	outputComplete   bool
	exit             receipt.ExitEvidence
	bindingLifecycle delegated.Lifecycle
}

func (s *Service) startDelegatedWait(live *liveSession) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	live.mu.Lock()
	live.delegatedCancel = cancel
	live.delegatedWaitDone = done
	live.mu.Unlock()
	go s.delegatedWaitLoop(ctx, live, done)
}

func (s *Service) delegatedWaitLoop(ctx context.Context, live *liveSession, done chan struct{}) {
	defer close(done)
	obs, waitErr := s.options.DelegatedRuntime.Wait(ctx, live.delegatedRef)
	if ctx.Err() != nil {
		return
	}
	snapshot := s.snapshotDelegatedFinalizing(live)
	snapshot.binding = s.loadDelegatedBindingUntilAvailable(live.sessionID)
	live.mu.Lock()
	live.delegatedBinding = snapshot.binding
	live.mu.Unlock()
	decision := classifyDelegatedTerminal(snapshot, obs, waitErr)
	rec := s.delegatedTerminalReceipt(live, snapshot, decision)
	s.publishDelegatedTerminal(live, snapshot.binding, decision, rec)
}

func (s *Service) snapshotDelegatedFinalizing(live *liveSession) delegatedTerminalSnapshot {
	live.mu.Lock()
	// Fence new unseen mutations before any terminal evidence becomes durable.
	// Known retries still replay from the durable mutation ledger first.
	live.state = session.Finalizing
	live.notify()
	snapshot := delegatedTerminalSnapshot{
		accepted: live.accepted, delivered: live.delivered, signal: live.signal,
		target: live.terminalTarget, outputBytes: live.outputBytes,
		observerBase: live.delegatedObserverBase, captureGap: live.delegatedCaptureGap,
	}
	live.mu.Unlock()
	snapshot.captureTruth, snapshot.captureGap = s.delegatedCaptureTruth(live.sessionID, snapshot.captureGap)
	return snapshot
}

func (s *Service) delegatedTerminalReceipt(live *liveSession, snapshot delegatedTerminalSnapshot, decision delegatedTerminalDecision) receipt.Receipt {
	rec := s.receiptFor(live, decision.state, decision.outcome)
	rec.ExecutionMode = string(live.spec.Mode)
	rec.Executable = live.spec.Executable
	if live.spec.Mode == operation.ExecutionModeShell {
		rec.Shell = live.spec.Shell
	}
	rec.CWD = live.spec.CWD
	rec.TTY = false
	rec.TimeoutMS = live.spec.TimeoutMS
	rec.OutputBytes = snapshot.outputBytes
	rec.OutputComplete = decision.outputComplete
	rec.CaptureQuality = decision.captureQuality
	rec.CaptureReasons = decision.captureReasons
	rec.InputAcceptedBytes = snapshot.accepted
	rec.InputDeliveredBytes = snapshot.delivered
	rec.StdinClosed = false
	rec.FailureReason = decision.failureReason
	rec.Spawn = live.spawn
	rec.Exit = decision.exit
	rec.Signal = snapshot.signal
	if rec.SchemaVersion == 5 {
		rec.InputAuthorityProvenance = s.delegatedTerminalInputAuthorityProvenance(live.sessionID)
	}
	s.attachWorkspaceProvenance(&rec, live.workspace)
	return rec
}

func (s *Service) publishDelegatedTerminal(live *liveSession, binding delegated.Binding, decision delegatedTerminalDecision, rec receipt.Receipt) {
	// Receipt/session terminal truth is durable before the recovery marker is
	// withdrawn by the terminal/lost binding transition.
	s.publishUntilDurable(rec)
	binding.Lifecycle = decision.bindingLifecycle
	binding.UpdatedAt = time.Now().UTC()
	if !binding.UpdatedAt.After(binding.CreatedAt) {
		binding.UpdatedAt = binding.CreatedAt.Add(time.Nanosecond)
	}
	s.advanceDelegatedBindingUntilDurable(binding)
	s.scheduleStructuredTerminal(rec, live.reservation.StructuredAdapter)
	s.scheduleTelemetryTerminal(rec, nil)
	s.scheduleEvidenceTerminal(rec, live.reservation)
	s.scheduleInputTraceTerminal(rec, live.reservation)
	s.endManagedShell(live)
	live.mu.Lock()
	live.delegatedBinding = binding
	live.exit = decision.exit
	live.state, live.outcome = decision.state, decision.outcome
	live.notify()
	live.doneOnce.Do(func() { close(live.done) })
	live.mu.Unlock()
	s.evictTerminated(live)
	_ = s.options.DelegatedRuntime.Close(context.Background(), live.delegatedRef)
}

func (s *Service) loadDelegatedBindingUntilAvailable(sessionID string) delegated.Binding {
	delay := 100 * time.Millisecond
	for {
		binding, err := s.delegatedStore().LoadDelegatedBinding(context.Background(), operation.SessionID(sessionID))
		if err == nil {
			return binding
		}
		time.Sleep(delay)
		if delay < 5*time.Second {
			delay *= 2
			if delay > 5*time.Second {
				delay = 5 * time.Second
			}
		}
	}
}

func (s *Service) advanceDelegatedBindingUntilDurable(binding delegated.Binding) {
	delay := 100 * time.Millisecond
	for {
		if result := s.delegatedStore().AdvanceDelegatedBinding(context.Background(), binding); result.Err == nil {
			return
		}
		time.Sleep(delay)
		if delay < 5*time.Second {
			delay *= 2
			if delay > 5*time.Second {
				delay = 5 * time.Second
			}
		}
	}
}
