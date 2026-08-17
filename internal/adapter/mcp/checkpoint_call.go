package mcp

import (
	"fmt"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	checkpointcore "github.com/maemreyo/shellbeam/internal/core/checkpoint"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"
)

func applyCheckpointInput(request *bridge.Request, in input) {
	switch in.Action {
	case "checkpoint_create":
		request.CheckpointCreate = checkpointcore.CreateRequest{CreateID: in.CheckpointCreateID, WorkspaceID: in.WorkspaceID, ActivityID: in.ActivityID, Paths: append([]string(nil), in.Paths...)}
	case "checkpoint_restore":
		request.CheckpointRestore = checkpointcore.RestoreRequest{RestoreID: in.RestoreID, CheckpointID: in.CheckpointID, Paths: append([]string(nil), in.Paths...)}
	case "checkpoint_inspect":
		request.CheckpointID = in.CheckpointID
	}
}

func checkpointSuccessV2(action string, out bridge.Response, body map[string]any) (string, *mcpgo.CallToolResult) {
	switch action {
	case "checkpoint_create":
		if out.Checkpoint == nil {
			return "", toolErrorV2(action, "invalid_daemon_response", "checkpoint result missing", false)
		}
		body["checkpoint"] = out.Checkpoint
		return "checkpoint_create: " + out.Checkpoint.CheckpointID, nil
	case "checkpoint_restore":
		if out.Restore == nil {
			return "", toolErrorV2(action, "invalid_daemon_response", "checkpoint restore result missing", false)
		}
		body["restore"] = out.Restore
		counts := map[checkpointcore.RestorePathOutcome]int{}
		for _, result := range out.Restore.Paths {
			counts[result.Outcome]++
		}
		return fmt.Sprintf("checkpoint_restore: restored=%d noop=%d conflict=%d unsupported=%d failed=%d", counts[checkpointcore.RestoreRestored], counts[checkpointcore.RestoreNoop], counts[checkpointcore.RestoreConflict], counts[checkpointcore.RestoreUnsupported], counts[checkpointcore.RestoreFailed]), nil
	case "checkpoint_inspect":
		if out.CheckpointInspection == nil {
			return "", toolErrorV2(action, "invalid_daemon_response", "checkpoint inspection missing", false)
		}
		body["checkpoint_inspection"] = out.CheckpointInspection
		return "checkpoint_inspect: " + string(out.CheckpointInspection.Provider.RetentionState), nil
	default:
		return "", toolErrorV2(action, "invalid_daemon_response", "checkpoint response action invalid", false)
	}
}
