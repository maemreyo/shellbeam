package daemon

import (
	"context"
	"fmt"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
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
		s.publishPersistentSpawnFailure(stored, activityID, workspaceObservation, receipt.SpawnEvidence{}, "persistent_spawn_failed")
		return View{}, failure.Normalize(err)
	}
	if launch.Handle == nil || !launch.Spawn.Attempted || !launch.Spawn.Succeeded {
		if launch.Handle != nil {
			_ = launch.Handle.Close()
		}
		s.publishPersistentSpawnFailure(stored, activityID, workspaceObservation, launch.Spawn, "persistent_spawn_readiness")
		return View{}, failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": sid, "reason": "readiness"}, fmt.Errorf("persistent runtime returned invalid readiness"))
	}
	if pidHandle, ok := launch.Handle.(pidHandle); !ok || pidHandle.PID() <= 0 || (launch.PID > 0 && pidHandle.PID() != launch.PID) {
		_ = launch.Handle.Close()
		s.publishPersistentSpawnFailure(stored, activityID, workspaceObservation, launch.Spawn, "persistent_spawn_identity")
		return View{}, failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": sid, "reason": "process_identity"}, fmt.Errorf("persistent runtime process identity mismatch"))
	}
	processObservation := s.prepareProcessStartedObservation(req.OperationID, sid)
	if processObservation.Err != nil {
		_ = launch.Handle.Close()
		s.publishPersistentSpawnFailure(stored, activityID, workspaceObservation, launch.Spawn, "persistent_spawn_observation")
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
		s.publishPersistentSpawnFailure(stored, activityID, workspaceObservation, launch.Spawn, "persistent_advance_failed")
		return View{}, failure.Normalize(got.Err)
	}
	s.resolveProcessStartedObservation(processObservation.ObservationSeq, true)
	s.startPersistentReconciliation(live)
	view, viewErr := s.waitView(ctx, stored, sid, 0, req.YieldMS, req.MaxOutputBytes)
	s.decorateNewStartView(&view, viewErr, stored, workspaceObservation, req.WorkspaceHint)
	return view, viewErr
}

// publishPersistentSpawnFailure closes out a persistent reservation that never
// reached a running, bound session. Every other start path -- ordinary
// commands via finishSpawnFailure, and restart recovery via AbandonUnresolved
// -- publishes a terminal receipt the moment it gives up on a reservation, so
// the session's admission slot is freed immediately. This path is the one
// place that used to be silent: a failure here left the reservation's session
// metadata parked at Starting forever, since no persistent binding was ever
// written for the recovery path to find and restart-time reconciliation
// deliberately defers to that path for anything marked Persistent. Without
// this, every failed persistent start -- not just a daemon crash -- leaked a
// capacity slot that no amount of restarting could recover.
func (s *Service) publishPersistentSpawnFailure(reservation operation.Reservation, activityID string, workspace workspaceObservation, spawn receipt.SpawnEvidence, failureReason string) {
	live := &liveSession{operationID: string(reservation.OperationID), activityID: activityID, sessionID: string(reservation.SessionID), reservation: reservation}
	rec := s.receiptFor(live, session.Failed, session.Failure)
	rec.FailureReason = failureReason
	rec.Spawn = spawn
	s.attachWorkspaceProvenance(&rec, workspace)
	s.publishUntilDurable(rec)
	s.scheduleStructuredTerminal(rec, reservation.StructuredAdapter)
	s.scheduleTelemetryTerminal(rec)
	s.scheduleEvidenceTerminal(rec, reservation)
	s.scheduleInputTraceTerminal(rec, reservation)
}
