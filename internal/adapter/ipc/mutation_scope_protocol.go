package ipc

import (
	"github.com/maemreyo/shellbeam/internal/core/activity"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/mutationscope"
	"github.com/maemreyo/shellbeam/internal/core/workspace"
)

func validateMutationScopeRequestV2(v RequestV2) error {
	switch v.Action {
	case "mutation_scope.set":
		if core.ValidateMutationID(v.MutationID) != nil || core.ValidateScopeID(v.ScopeID) != nil {
			return failure.New(failure.InvalidInput, map[string]string{"field": "mutation_id"}, nil)
		}
		if _, err := activity.ParseID(v.ActivityID); err != nil {
			return failure.New(failure.InvalidInput, map[string]string{"field": "activity_id"}, err)
		}
		if _, err := workspace.ParseWorkspaceID(v.WorkspaceID); err != nil {
			return failure.New(failure.InvalidInput, map[string]string{"field": "workspace_id"}, err)
		}
		if v.Mode != core.ModeRead && v.Mode != core.ModeMutate {
			return failure.New(failure.InvalidInput, map[string]string{"field": "mode"}, nil)
		}
		if _, err := core.NormalizeSelectors(v.Paths); err != nil {
			return failure.New(failure.InvalidInput, map[string]string{"field": "paths"}, err)
		}
		if v.TTLMS != 0 && (v.TTLMS < core.MinTTL.Milliseconds() || v.TTLMS > core.MaxTTL.Milliseconds()) {
			return failure.New(failure.InvalidInput, map[string]string{"field": "ttl_ms"}, nil)
		}
	case "mutation_scope.release":
		if core.ValidateMutationID(v.MutationID) != nil || core.ValidateScopeID(v.ScopeID) != nil {
			return failure.New(failure.InvalidInput, map[string]string{"field": "mutation_id"}, nil)
		}
	case "inspect.mutation_scopes":
		if _, err := workspace.ParseWorkspaceID(v.WorkspaceID); err != nil {
			return failure.New(failure.InvalidInput, map[string]string{"field": "workspace_id"}, err)
		}
		if v.ActivityID != "" {
			if _, err := activity.ParseID(v.ActivityID); err != nil {
				return failure.New(failure.InvalidInput, map[string]string{"field": "activity_id"}, err)
			}
		}
	}
	return nil
}
