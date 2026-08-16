package mcp

import (
	"fmt"

	activity "github.com/maemreyo/shellbeam/internal/core/activity"
	persistent "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/session"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func validateSessionInspectInput(v input) error {
	if v.SessionName != "" {
		if err := persistent.ValidateSessionName(v.SessionName); err != nil {
			return err
		}
	}
	if v.ActivityID != "" {
		if _, err := activity.ParseID(v.ActivityID); err != nil {
			return err
		}
	}
	if v.WorkspaceID != "" {
		if _, err := workspace.ParseWorkspaceID(v.WorkspaceID); err != nil {
			return err
		}
	}
	if v.State != "" {
		switch session.State(v.State) {
		case session.Starting, session.Running, session.Finalizing, session.Completed, session.Failed, session.TimedOut, session.Killed, session.Abandoned:
		default:
			return fmt.Errorf("invalid session state")
		}
	}
	if v.MaxRecords < 0 || v.MaxRecords > persistent.MaxInspectRows {
		return fmt.Errorf("invalid session inspect limit")
	}
	if len(v.Continuation) > 2048 {
		return fmt.Errorf("session continuation too large")
	}
	return nil
}
