package project

import (
	"context"

	core "github.com/maemreyo/shellbeam/internal/core/project"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type WorkspaceLookup interface {
	ListWorkspaces(context.Context) ([]workspace.Workspace, error)
}

type Loader interface {
	Load(context.Context, string) core.LoadResult
}
