package ipc

import (
	"context"

	mutationapp "github.com/maemreyo/shellbeam/internal/app/mutationscope"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/mutationscope"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func (s *Server) mutationScopeV2(ctx context.Context, req RequestV2, resp *ResponseV2) error {
	actions, ok := s.actions.(MutationScopeActions)
	if !ok {
		return failure.New(failure.FeatureUnavailable, map[string]string{"feature": req.Action}, nil)
	}
	switch req.Action {
	case "mutation_scope.set":
		result, err := actions.SetMutationScope(ctx, mutationapp.SetRequest{MutationID: req.MutationID, ScopeID: req.ScopeID, ActivityID: req.ActivityID, WorkspaceID: workspace.WorkspaceID(req.WorkspaceID), Mode: req.Mode, Paths: append([]string(nil), req.Paths...), TTLMS: req.TTLMS})
		resp.Mutation = &result
		return err
	case "mutation_scope.release":
		result, err := actions.ReleaseMutationScope(ctx, mutationapp.ReleaseRequest{MutationID: req.MutationID, ScopeID: req.ScopeID})
		resp.Mutation = &result
		return err
	case "inspect.mutation_scopes":
		result, err := actions.InspectMutationScopes(ctx, mutationapp.InspectRequest{WorkspaceID: workspace.WorkspaceID(req.WorkspaceID), ActivityID: req.ActivityID})
		resp.MutationScopes = &result
		return err
	default:
		return failure.New(failure.InvalidInput, map[string]string{"field": "action"}, nil)
	}
}

var _ core.Mode
