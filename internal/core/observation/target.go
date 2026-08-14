package observation

import (
	"fmt"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type TargetKind string

const (
	TargetOperation  TargetKind = "operation"
	TargetSession    TargetKind = "session"
	TargetActivity   TargetKind = "activity"
	TargetWorkspace  TargetKind = "workspace"
	TargetRepository TargetKind = "repository"
)

type Target struct {
	Kind         TargetKind `json:"kind"`
	OperationID  string     `json:"operation_id,omitempty"`
	SessionID    string     `json:"session_id,omitempty"`
	ActivityID   string     `json:"activity_id,omitempty"`
	WorkspaceID  string     `json:"workspace_id,omitempty"`
	RepositoryID string     `json:"repository_id,omitempty"`
}

func (t Target) Validate() error {
	count := 0
	for _, value := range []string{t.OperationID, t.SessionID, t.ActivityID, t.WorkspaceID, t.RepositoryID} {
		if value != "" {
			count++
		}
	}
	if count != 1 {
		return fmt.Errorf("observation target requires one selector")
	}
	switch t.Kind {
	case TargetOperation:
		if t.OperationID == "" {
			return fmt.Errorf("operation target missing id")
		}
		_, err := operation.ParseID(t.OperationID)
		return err
	case TargetSession:
		if t.SessionID == "" {
			return fmt.Errorf("session target missing id")
		}
		_, err := operation.ParseSessionID(t.SessionID)
		return err
	case TargetActivity:
		if !validBoundedID(t.ActivityID) {
			return fmt.Errorf("invalid activity id")
		}
	case TargetWorkspace:
		_, err := workspace.ParseWorkspaceID(t.WorkspaceID)
		return err
	case TargetRepository:
		_, err := workspace.ParseRepositoryID(t.RepositoryID)
		return err
	default:
		return fmt.Errorf("invalid observation target kind")
	}
	return nil
}
