package main

import (
	"context"
	"fmt"

	decisionapp "github.com/maemreyo/shellbeam/internal/app/decisionprotocol"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type decisionWorkspaceList interface {
	List(context.Context) ([]workspacecore.Workspace, error)
}

type decisionWorkspaceRuntime interface {
	decisionWorkspaceList
	decisionapp.WorkspaceInspector
}

func resolveDecisionWorkspace(ctx context.Context, selector string, workspaces decisionWorkspaceRuntime) (workspacecore.Workspace, error) {
	if workspaces == nil {
		return workspacecore.Workspace{}, failure.New(failure.DecisionContextUnavailable, nil, fmt.Errorf("decision workspace dependency unavailable"))
	}
	if selector != "" {
		id, err := workspacecore.ParseWorkspaceID(selector)
		if err != nil {
			return workspacecore.Workspace{}, failure.New(failure.InvalidInput, map[string]string{"field": "workspace_id"}, err)
		}
		value, err := workspaces.Inspect(ctx, string(id))
		if err != nil {
			return workspacecore.Workspace{}, err
		}
		if err := value.Validate(); err != nil {
			return workspacecore.Workspace{}, fmt.Errorf("invalid inspected decision workspace: %w", err)
		}
		if value.ID != id {
			return workspacecore.Workspace{}, fmt.Errorf("inspected decision workspace identity mismatch")
		}
		return value, nil
	}
	values, err := workspaces.List(ctx)
	if err != nil {
		return workspacecore.Workspace{}, err
	}
	if len(values) != 1 {
		return workspacecore.Workspace{}, failure.New(failure.DecisionContextUnavailable, nil, fmt.Errorf("decision workspace context is not unique"))
	}
	if err := values[0].Validate(); err != nil {
		return workspacecore.Workspace{}, fmt.Errorf("invalid decision workspace context: %w", err)
	}
	return values[0], nil
}
