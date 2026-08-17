package ipc

import (
	"context"
	"fmt"
	checkpointapp "github.com/maemreyo/shellbeam/internal/app/checkpoint"
	"regexp"

	core "github.com/maemreyo/shellbeam/internal/core/checkpoint"
	"github.com/maemreyo/shellbeam/internal/core/failure"
)

var ipcCheckpointIDPattern = regexp.MustCompile(`^chk_[0-9A-HJKMNP-TV-Z]{26}$`)

type CheckpointActions interface {
	CreateCheckpoint(context.Context, core.CreateRequest) (core.Checkpoint, error)
	RestoreCheckpoint(context.Context, core.RestoreRequest) (core.RestoreResult, error)
	InspectCheckpoint(context.Context, string) (checkpointapp.CheckpointInspection, error)
}

func validateCheckpointRequestV2(v RequestV2) error {
	switch v.Action {
	case "checkpoint_create":
		_, err := (core.CreateRequest{CreateID: v.CheckpointCreateID, WorkspaceID: v.WorkspaceID, ActivityID: v.ActivityID, Paths: v.Paths}).Normalize()
		if err != nil {
			return failure.New(failure.InvalidInput, map[string]string{"field": "checkpoint_create"}, err)
		}
	case "checkpoint_restore":
		_, err := (core.RestoreRequest{RestoreID: v.RestoreID, CheckpointID: v.CheckpointID, Paths: v.Paths}).Normalize()
		if err != nil {
			return failure.New(failure.InvalidInput, map[string]string{"field": "checkpoint_restore"}, err)
		}
	case "checkpoint_inspect":
		if !ipcCheckpointIDPattern.MatchString(v.CheckpointID) {
			return failure.New(failure.InvalidInput, map[string]string{"field": "checkpoint_id"}, fmt.Errorf("invalid checkpoint id"))
		}
	default:
		return failure.New(failure.InvalidInput, map[string]string{"field": "action"}, nil)
	}
	return nil
}

func (s *Server) checkpointV2(ctx context.Context, req RequestV2, resp *ResponseV2) error {
	actions, ok := s.actions.(CheckpointActions)
	if !ok {
		return failure.New(failure.FeatureUnavailable, map[string]string{"feature": req.Action}, nil)
	}
	switch req.Action {
	case "checkpoint_create":
		result, err := actions.CreateCheckpoint(ctx, core.CreateRequest{CreateID: req.CheckpointCreateID, WorkspaceID: req.WorkspaceID, ActivityID: req.ActivityID, Paths: append([]string(nil), req.Paths...)})
		if err == nil {
			resp.Checkpoint = &result
		}
		return err
	case "checkpoint_restore":
		result, err := actions.RestoreCheckpoint(ctx, core.RestoreRequest{RestoreID: req.RestoreID, CheckpointID: req.CheckpointID, Paths: append([]string(nil), req.Paths...)})
		if err == nil {
			resp.Restore = &result
		}
		return err
	case "checkpoint_inspect":
		result, err := actions.InspectCheckpoint(ctx, req.CheckpointID)
		if err == nil {
			resp.CheckpointInspection = &result
		}
		return err
	default:
		return failure.New(failure.InvalidInput, map[string]string{"field": "action"}, nil)
	}
}
