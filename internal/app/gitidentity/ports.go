package gitidentity

import (
	"context"
	coreidentity "github.com/maemreyo/shellbeam/internal/core/gitidentity"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type WorkspaceLookup interface {
	Inspect(context.Context, string) (workspace.Workspace, error)
}

type Probe interface {
	Shallow(context.Context, string) (coreidentity.Observation, coreidentity.RemoteIdentity, error)
	Deep(context.Context, string, coreidentity.Profile, coreidentity.Observation) (coreidentity.Observation, error)
}

type Profiles struct {
	Values             map[string]coreidentity.Profile
	RepositoryBindings map[string]string
	WorkspaceBindings  map[string]string
}
