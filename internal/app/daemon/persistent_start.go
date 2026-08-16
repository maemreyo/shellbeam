package daemon

import (
	"context"
	"fmt"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func (s *Service) spawnPreparedPersistentStart(ctx context.Context, req StartRequest, stored operation.Reservation, spec operation.ExecutionSpec, sid string) (View, error) {
	if s.options.PersistentRuntime == nil {
		return View{}, failure.New(failure.FeatureUnavailable, map[string]string{"feature": "named_sessions"}, nil)
	}
	executionCWD := stored.CWD
	workspaceObservation := s.captureWorkspace(ctx, executionCWD)
	activityReq := req
	activityReq.CWD = executionCWD
	activityID := s.admitActivity(ctx, activityReq, sid, workspaceObservation.binding)
	spec.CWD = executionCWD
	launch, err := s.options.PersistentRuntime.Ensure(ctx, stored, spec)
	if err != nil {
		return View{}, failure.Normalize(err)
	}
	if launch.Handle == nil || !launch.Spawn.Attempted || !launch.Spawn.Succeeded {
		if launch.Handle != nil {
			_ = launch.Handle.Close()
		}
		return View{}, failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": sid, "reason": "readiness"}, fmt.Errorf("persistent runtime returned invalid readiness"))
	}
	if pidHandle, ok := launch.Handle.(pidHandle); !ok || pidHandle.PID() <= 0 || (launch.PID > 0 && pidHandle.PID() != launch.PID) {
		_ = launch.Handle.Close()
		return View{}, failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": sid, "reason": "process_identity"}, fmt.Errorf("persistent runtime process identity mismatch"))
	}
	processObservation := s.prepareProcessStartedObservation(req.OperationID, sid)
	if processObservation.Err != nil {
		_ = launch.Handle.Close()
		return View{}, failure.Normalize(processObservation.Err)
	}
	// Resolution is a pure function of the request, so labelling the receipt
	// here cannot disagree with the spec bound from the same request.
	persistentSource, persistentStdinSource := timeoutSourceUnlimited, ""
	if resolvedPolicy, resolveErr := s.resolveExecutionPolicy(req); resolveErr == nil {
		persistentSource, persistentStdinSource = timeoutSourceOf(resolvedPolicy), resolvedPolicy.StdinSource()
	}
	live := &liveSession{
		timeoutSource: persistentSource,
		stdinSource:   persistentStdinSource,
		operationID:   req.OperationID, activityID: activityID, sessionID: sid, reservation: stored, spec: spec, workspace: workspaceObservation,
		state: session.Running, handle: launch.Handle, spawn: launch.Spawn, persistent: true,
		input: session.NewInputLedger(s.options.MaxQueuedInputBytes, false), kills: session.NewKillLedger(), changed: make(chan struct{}), done: make(chan struct{}),
	}
	s.activateLiveSession(live)
	snapshot := session.Snapshot{SchemaVersion: 1, OperationID: req.OperationID, SessionID: sid, DaemonIncarnation: s.options.Incarnation, State: session.Running, OutputAvailable: true, UpdatedAt: time.Now().UTC()}
	if got := s.store.AdvanceSession(context.Background(), snapshot); got.Err != nil {
		s.resolveProcessStartedObservation(processObservation.ObservationSeq, true)
		s.remove(sid)
		s.endManagedShell(live)
		_ = launch.Handle.Close()
		return View{}, failure.Normalize(got.Err)
	}
	s.resolveProcessStartedObservation(processObservation.ObservationSeq, true)
	s.startPersistentReconciliation(live)
	view, viewErr := s.waitView(ctx, stored, sid, 0, req.YieldMS, req.MaxOutputBytes)
	s.decorateNewStartView(&view, viewErr, stored, workspaceObservation, req.WorkspaceHint)
	return view, viewErr
}
