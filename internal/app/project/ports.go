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

type ExecutableObserver interface {
	ObserveExecutable(context.Context, string) core.ReadinessCheck
}

type EnvironmentObserver interface {
	ObserveEnvironmentPresence(context.Context, string, bool) core.ReadinessCheck
}

type ToolchainObserver interface {
	ObserveToolchain(context.Context, string, string, core.Toolchain) core.ReadinessCheck
}

type ReviewStore interface {
	LoadProjectReview(context.Context, workspace.RepositoryID) (core.Review, bool, error)
	SaveProjectReview(context.Context, core.Review) error
}

type ParameterValidation struct {
	Value              string
	ProviderID         string
	ProviderVersion    int
	ObservationQuality string
}

type RepoPathValidator interface {
	ValidatePath(context.Context, workspace.Workspace, core.ParameterDefinition, string) (ParameterValidation, error)
}

type RepoPackageValidator interface {
	ValidatePackage(context.Context, workspace.Workspace, string, string) (ParameterValidation, error)
}
