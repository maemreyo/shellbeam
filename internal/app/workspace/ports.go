package workspace

import (
	"context"

	core "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type Registry interface {
	SaveRepository(context.Context, core.Repository) error
	SaveWorkspace(context.Context, core.Workspace) error
	ListRepositories(context.Context) ([]core.Repository, error)
	ListWorkspaces(context.Context) ([]core.Workspace, error)
	DeleteWorkspace(context.Context, core.WorkspaceID) error
}

type GitObservation struct {
	CommonDir string
	Root      string
	GitDir    string
	Branch    string
	Bare      bool
}

type GitWorktree struct {
	Root   string
	Head   string
	Branch string
	Bare   bool
}

type Git interface {
	Inspect(context.Context, string) (GitObservation, error)
	ListWorktrees(context.Context, string) ([]GitWorktree, error)
	AddWorktree(context.Context, string, string, string) error
	RemoveWorktree(context.Context, string, string, bool) error
	Dirty(context.Context, string) (bool, error)
}
