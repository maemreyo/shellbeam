package ipc

import (
	"context"
	"fmt"

	activity "github.com/maemreyo/shellbeam/internal/core/activity"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	persistent "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/session"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func validateSessionInspectV2(v RequestV2) error {
	if v.SessionName != "" {
		if err := persistent.ValidateSessionName(v.SessionName); err != nil {
			return failure.New(failure.InvalidInput, map[string]string{"field": "session_name"}, err)
		}
	}
	if v.ActivityID != "" {
		if _, err := activity.ParseID(v.ActivityID); err != nil {
			return failure.New(failure.InvalidInput, map[string]string{"field": "activity_id"}, err)
		}
	}
	if v.WorkspaceID != "" {
		if _, err := workspace.ParseWorkspaceID(v.WorkspaceID); err != nil {
			return failure.New(failure.InvalidInput, map[string]string{"field": "workspace_id"}, err)
		}
	}
	if v.State != "" {
		switch session.State(v.State) {
		case session.Starting, session.Running, session.Finalizing, session.Completed, session.Failed, session.TimedOut, session.Killed, session.Abandoned:
		default:
			return failure.New(failure.InvalidInput, map[string]string{"field": "state"}, fmt.Errorf("invalid session state"))
		}
	}
	if v.MaxRecords < 0 || v.MaxRecords > persistent.MaxInspectRows {
		return failure.New(failure.InvalidInput, map[string]string{"field": "max_records"}, fmt.Errorf("invalid session inspect limit"))
	}
	if len(v.Continuation) > 2048 {
		return failure.New(failure.InvalidInput, map[string]string{"field": "continuation"}, fmt.Errorf("session continuation too large"))
	}
	return nil
}

func isSupportedV2Action(action string) bool {
	switch action {
	case "start", "poll", "write", "kill", "read_output", "checkpoint_create", "checkpoint_restore", "checkpoint_inspect", "inspect.server", "inspect.workspace", "inspect.activity", "inspect.sessions", "inspect.project", "inspect.readiness", "inspect.events", "inspect.structured", "inspect.telemetry", "inspect.trace", "inspect.evidence", "inspect.environment", "inspect.process", "repro.create", "inspect.repro", "inspect.code", "mutation_scope.set", "mutation_scope.release", "inspect.mutation_scopes", "capabilities.negotiate", "read_media", "handoff.request", "handoff.wait", "handoff.abort", "inspect.handoff":
		return true
	default:
		return false
	}
}

func isDeferredV2Action(string) bool { return false }

func (s *Server) inspectSessionsV2(ctx context.Context, req RequestV2, resp *ResponseV2) error {
	actions, ok := s.actions.(SessionInspectActions)
	if !ok {
		return failure.New(failure.FeatureUnavailable, map[string]string{"feature": req.Action}, nil)
	}
	result, err := actions.InspectSessions(ctx, persistent.InspectRequest{SessionName: req.SessionName, ActivityID: req.ActivityID, WorkspaceID: req.WorkspaceID, State: req.State, PersistentOnly: req.PersistentOnly, Limit: req.MaxRecords, Cursor: req.Continuation})
	resp.Sessions = &result
	return err
}
