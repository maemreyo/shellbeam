package daemon

import (
	"context"
	"fmt"
	"time"

	inputtraceapp "github.com/maemreyo/shellbeam/internal/app/inputtrace"
	projectapp "github.com/maemreyo/shellbeam/internal/app/project"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	project "github.com/maemreyo/shellbeam/internal/core/project"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

type ProjectCommandBinder interface {
	Bind(context.Context, projectapp.BindRequest) (project.CommandBinding, error)
}

type reservationCommitter func(context.Context, operation.Reservation) (operation.Reservation, bool, StoreResult)

func wantsProjectCommand(req StartRequest) bool {
	return req.ProjectCommandID != "" || req.Params != nil
}

func (s *Service) startProjectCommand(ctx context.Context, req StartRequest, id operation.ID) (View, error) {
	intent, fingerprint, err := typedRequestIntent(req)
	if err != nil {
		return View{}, err
	}
	if s.store == nil {
		return View{}, failure.New(failure.PersistenceUnavailable, nil, fmt.Errorf("store unavailable"))
	}
	if view, handled, lookupErr := s.lookupProjectCommandReplay(ctx, req, id, fingerprint); handled {
		return view, lookupErr
	}
	if s.options.ProjectCommandBinder == nil {
		return View{}, failure.New(failure.FeatureUnavailable, map[string]string{"feature": "project_commands"}, nil)
	}
	claim := operation.TypedIntentClaim{
		SchemaVersion: operation.TypedIntentClaimSchemaVersion, OperationID: id,
		RequestFingerprint: fingerprint, Intent: intent, CreatedAt: time.Now().UTC(),
	}
	if _, _, result := s.store.ReserveTypedIntent(ctx, claim); result.Err != nil {
		return View{}, failure.Normalize(result.Err)
	}
	binding, err := s.options.ProjectCommandBinder.Bind(ctx, projectapp.BindRequest{
		WorkspaceID: req.WorkspaceID, CommandID: req.ProjectCommandID, Params: cloneStringMap(req.Params),
		TimeoutMS: req.TimeoutMS, TTY: req.TTY,
	})
	if err != nil {
		return View{}, err
	}
	reservation, spec, err := s.reservationForProjectCommand(req, id, fingerprint, binding)
	if err != nil {
		return View{}, err
	}
	commit := func(commitCtx context.Context, value operation.Reservation) (operation.Reservation, bool, StoreResult) {
		return s.store.CommitTypedBinding(commitCtx, id, value)
	}
	return s.admitPreparedStart(ctx, req, reservation, spec, commit, true)
}

func typedRequestIntent(req StartRequest) (operation.TypedRequestIntent, string, error) {
	if req.ProtocolVersion != 2 {
		return operation.TypedRequestIntent{}, "", failure.New(failure.FeatureUnavailable, map[string]string{"feature": "project_commands", "required_version": "2"}, nil)
	}
	if req.ProjectCommandID == "" || req.WorkspaceID == "" {
		return operation.TypedRequestIntent{}, "", failure.New(failure.InvalidInput, map[string]string{"field": "project_command_id"}, fmt.Errorf("typed project command requires workspace and command id"))
	}
	if req.Command != "" || len(req.Argv) != 0 || req.CWD != "" || req.Evidence != nil {
		return operation.TypedRequestIntent{}, "", failure.New(failure.InvalidInput, map[string]string{"field": "project_command_id"}, fmt.Errorf("typed project command conflicts with raw execution fields"))
	}
	intent := operation.TypedRequestIntent{
		WorkspaceID: req.WorkspaceID, ProjectCommandID: req.ProjectCommandID, Params: cloneStringMap(req.Params),
		TTY: req.TTY, TimeoutMS: req.TimeoutMS, Persistent: req.Persistent, SessionName: req.SessionName, TraceMode: req.TraceMode,
	}
	fingerprint, err := intent.Fingerprint()
	if err != nil {
		return operation.TypedRequestIntent{}, "", failure.New(failure.InvalidInput, map[string]string{"field": "project_command_id"}, err)
	}
	return intent, fingerprint, nil
}

func (s *Service) lookupProjectCommandReplay(ctx context.Context, req StartRequest, id operation.ID, requestFingerprint string) (View, bool, error) {
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
	if ((req.Persistent && stored.SchemaVersion != 4) || (!req.Persistent && stored.SchemaVersion != 3)) || stored.ProjectCommand == nil || stored.WorkspaceID != req.WorkspaceID || stored.ProjectCommand.CommandID != req.ProjectCommandID || stored.Persistent != req.Persistent || stored.SessionName != req.SessionName {
		return View{}, true, failure.New(failure.ProjectCommandBindingConflict, map[string]string{"operation_id": string(id)}, nil)
	}
	if err := stored.ProjectCommand.Validate(); err != nil {
		return View{}, true, failure.New(failure.ProjectCommandBindingConflict, map[string]string{"operation_id": string(id)}, err)
	}
	structuredAdapter, err := normalizedStructuredAdapterForArgv(req, stored.ProjectCommand.ResolvedArgv)
	if err != nil {
		return View{}, true, err
	}
	observationFingerprint, err := (operation.ObservationBinding{ActivityID: req.ActivityID, Intent: req.Intent, StructuredAdapter: structuredAdapter}).Fingerprint()
	if err != nil {
		return View{}, true, invalidIntentFailure(err)
	}
	if stored.ObservationBindingFingerprint != observationFingerprint {
		return View{}, true, failure.New(failure.OperationMetadataConflict, map[string]string{"operation_id": string(id)}, nil)
	}
	view, err := s.waitView(ctx, stored, string(stored.SessionID), 0, req.YieldMS, req.MaxOutputBytes)
	if err == nil {
		s.enrichStructuredAdapterAvailability(&view, stored)
	}
	return view, true, err
}

func (s *Service) reservationForProjectCommand(req StartRequest, id operation.ID, requestFingerprint string, bound project.CommandBinding) (operation.Reservation, operation.ExecutionSpec, error) {
	binding := cloneProjectCommandBinding(bound)
	if err := binding.Validate(); err != nil {
		return operation.Reservation{}, operation.ExecutionSpec{}, err
	}
	if binding.CommandID != req.ProjectCommandID {
		return operation.Reservation{}, operation.ExecutionSpec{}, failure.New(failure.ProjectCommandBindingConflict, map[string]string{"operation_id": string(id), "command": req.ProjectCommandID}, fmt.Errorf("binder command mismatch"))
	}
	structuredAdapter, err := normalizedStructuredAdapterForArgv(req, binding.ResolvedArgv)
	if err != nil {
		return operation.Reservation{}, operation.ExecutionSpec{}, err
	}
	spec := bindExecution(s.owner, operation.ExecutionSpec{
		Mode: operation.ExecutionModeArgv, Argv: append([]string(nil), binding.ResolvedArgv...), CWD: binding.ResolvedCWD,
		TTY: req.TTY, TimeoutMS: req.TimeoutMS,
	})
	resolvedIntent := operation.Intent{
		Argv: append([]string(nil), binding.ResolvedArgv...), WorkspaceID: req.WorkspaceID,
		CWD: binding.LogicalCWD, ResolvedCWD: binding.ResolvedCWD, TTY: req.TTY, TimeoutMS: req.TimeoutMS, Persistent: req.Persistent, SessionName: req.SessionName,
	}
	executionFingerprint, err := resolvedIntent.ExecutionFingerprint(spec.Executable)
	if err != nil {
		return operation.Reservation{}, operation.ExecutionSpec{}, invalidIntentFailure(err)
	}
	observationFingerprint, err := (operation.ObservationBinding{ActivityID: req.ActivityID, Intent: req.Intent, StructuredAdapter: structuredAdapter}).Fingerprint()
	if err != nil {
		return operation.Reservation{}, operation.ExecutionSpec{}, invalidIntentFailure(err)
	}
	reservation := operation.Reservation{
		SchemaVersion: func() int {
			if req.Persistent {
				return 4
			}
			return 3
		}(), OperationID: id, ActivityID: req.ActivityID, WorkspaceID: req.WorkspaceID,
		LogicalCWD: binding.LogicalCWD, StructuredAdapter: structuredAdapter,
		RequestFingerprint: requestFingerprint, ExecutionFingerprint: executionFingerprint,
		ObservationBindingFingerprint: observationFingerprint,
		ExecutionMode:                 operation.ExecutionModeArgv, Executable: spec.Executable,
		Argv: append([]string(nil), binding.ResolvedArgv...), CWD: binding.ResolvedCWD,
		TTY: req.TTY, TimeoutMS: req.TimeoutMS, Persistent: req.Persistent, SessionName: req.SessionName, DaemonIncarnation: s.options.Incarnation,
		ProjectCommand: &binding,
	}
	return reservation, spec, nil
}

func (s *Service) admitPreparedStart(ctx context.Context, req StartRequest, reservation operation.Reservation, spec operation.ExecutionSpec, commit reservationCommitter, typedReplay bool) (View, error) {
	reservation, spec, preparedTrace, err := s.prepareInputTrace(ctx, req, reservation, spec)
	if err != nil {
		return View{}, err
	}
	sid := newSessionID()
	reservation.SessionID = operation.SessionID(sid)
	reservation.CreatedAt = time.Now().UTC()
	if reservation.SchemaVersion >= 2 {
		s.freezeEnvironmentBinding(&reservation)
	}
	stored, created, result := commit(ctx, reservation)
	if result.Err != nil {
		abortPreparedTrace(preparedTrace)
		return View{}, failure.Normalize(result.Err)
	}
	sid = string(stored.SessionID)
	if !created {
		abortPreparedTrace(preparedTrace)
		if typedReplay {
			return s.waitTypedReservedStart(ctx, req, stored, sid)
		}
		return s.waitReservedStart(ctx, req, stored, sid)
	}
	return s.spawnPreparedStart(ctx, req, stored, spec, sid, preparedTrace)
}

func (s *Service) waitTypedReservedStart(ctx context.Context, req StartRequest, stored operation.Reservation, sessionID string) (View, error) {
	view, err := s.waitView(ctx, stored, sessionID, 0, req.YieldMS, req.MaxOutputBytes)
	if err == nil {
		s.enrichStructuredAdapterAvailability(&view, stored)
	}
	return view, err
}

func (s *Service) spawnPreparedStart(ctx context.Context, req StartRequest, stored operation.Reservation, spec operation.ExecutionSpec, sid string, preparedTrace inputtraceapp.Prepared) (View, error) {
	if stored.Persistent {
		abortPreparedTrace(preparedTrace)
		return s.spawnPreparedPersistentStart(ctx, req, stored, spec, sid)
	}
	executionCWD := stored.CWD
	workspaceObservation := s.captureWorkspace(ctx, executionCWD)
	activityReq := req
	activityReq.CWD = executionCWD
	activityID := s.admitActivity(ctx, activityReq, sid, workspaceObservation.binding)
	spec.CWD = executionCWD
	// Resolution is a pure function of the request, so recomputing it here to
	// label the receipt cannot disagree with the spec that was bound from it.
	policySource, stdinSource := timeoutSourceUnlimited, ""
	if resolved, resolveErr := s.resolveExecutionPolicy(req); resolveErr == nil {
		policySource, stdinSource = timeoutSourceOf(resolved), resolved.StdinSource()
	}
	live := &liveSession{operationID: req.OperationID, activityID: activityID, sessionID: sid, reservation: stored, spec: spec, workspace: workspaceObservation, state: session.Starting, timeoutSource: policySource, stdinSource: stdinSource, input: session.NewInputLedger(s.options.MaxQueuedInputBytes, req.TTY), kills: session.NewKillLedger(), changed: make(chan struct{}), jobs: make(chan inputJob, s.options.MaxQueuedInputBytes+1), writerDone: make(chan struct{}), done: make(chan struct{})}
	processObservation := s.prepareProcessStartedObservation(req.OperationID, sid)
	if processObservation.Err != nil {
		abortPreparedTrace(preparedTrace)
		s.resolveProcessStartedObservation(processObservation.ObservationSeq, false)
		s.finalizeAdmittedStartFailure(live, "process_observation_failed")
		return View{}, failure.Normalize(processObservation.Err)
	}
	s.activateLiveSession(live)
	h, spawn, spawnErr := s.owner.Start(context.Background(), live.spec, sessionSink{service: s, id: sid})
	live.mu.Lock()
	live.spawn = spawn
	if spawnErr == nil {
		live.handle = h
		live.state = session.Running
	}
	live.notify()
	live.mu.Unlock()
	if spawnErr != nil {
		abortPreparedTrace(preparedTrace)
		s.resolveProcessStartedObservation(processObservation.ObservationSeq, false)
		s.finishSpawnFailure(live)
		view, viewErr := s.waitView(ctx, stored, sid, 0, 0, req.MaxOutputBytes)
		if viewErr == nil {
			s.enrichWorkspaceContext(&view, workspaceObservation.context, req.WorkspaceHint)
			s.enrichStructuredAdapterAvailability(&view, stored)
		}
		return view, viewErr
	}
	s.resolveProcessStartedObservation(processObservation.ObservationSeq, true)
	snap := session.Snapshot{SchemaVersion: 1, OperationID: req.OperationID, SessionID: sid, DaemonIncarnation: s.options.Incarnation, State: session.Running, OutputAvailable: true, UpdatedAt: time.Now().UTC()}
	if got := s.store.AdvanceSession(context.Background(), snap); got.Err != nil {
		h.Signal("TERM")
	}
	go s.writeLoop(live)
	go s.waitLoop(live)
	// The bound comes from the resolved spec: a caller that named nothing has
	// been given the ordinary default by now, and only work that declared
	// itself long-running reaches here unbounded.
	if live.spec.TimeoutMS > 0 {
		go s.timeoutLoop(live, time.Duration(live.spec.TimeoutMS)*time.Millisecond)
	}
	view, viewErr := s.waitView(ctx, stored, sid, 0, req.YieldMS, req.MaxOutputBytes)
	s.decorateNewStartView(&view, viewErr, stored, workspaceObservation, req.WorkspaceHint)
	return view, viewErr
}

func cloneProjectCommandBinding(value project.CommandBinding) project.CommandBinding {
	value.Parameters = append([]project.ParameterBinding(nil), value.Parameters...)
	value.ResolvedArgv = append([]string(nil), value.ResolvedArgv...)
	value.ExpectedOutputs = append([]project.Output(nil), value.ExpectedOutputs...)
	return value
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
