package mcp

import (
	"fmt"
	"strings"

	checkpointcore "github.com/maemreyo/shellbeam/internal/core/checkpoint"
)

func validateCheckpointInput(v input) error {
	switch v.Action {
	case "checkpoint_create":
		_, err := (checkpointcore.CreateRequest{CreateID: v.CheckpointCreateID, WorkspaceID: v.WorkspaceID, ActivityID: v.ActivityID, Paths: v.Paths}).Normalize()
		return err
	case "checkpoint_restore":
		_, err := (checkpointcore.RestoreRequest{RestoreID: v.RestoreID, CheckpointID: v.CheckpointID, Paths: v.Paths}).Normalize()
		return err
	case "checkpoint_inspect":
		if !validCheckpointIDInput(v.CheckpointID) {
			return fmt.Errorf("invalid checkpoint id")
		}
		return nil
	default:
		return fmt.Errorf("invalid checkpoint action %q", v.Action)
	}
}

func validCheckpointIDInput(value string) bool {
	if len(value) != 30 || !strings.HasPrefix(value, "chk_") {
		return false
	}
	for _, r := range value[4:] {
		if strings.ContainsRune("0123456789ABCDEFGHJKMNPQRSTVWXYZ", r) {
			continue
		}
		return false
	}
	return true
}
