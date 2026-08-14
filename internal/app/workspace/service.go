package workspace

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/workspace"
)

var (
	ErrWorkspaceNotFound = errors.New("workspace_not_found")
	ErrDirtyWorktree     = errors.New("workspace_dirty_requires_force")
	ErrUnsafeWorktree    = errors.New("unsafe_worktree_state")
)

type CreateRequest struct {
	Repository string
	Ref        string
	Path       string
	Label      string
}

type Service struct {
	registry Registry
	git      Git
	now      func() time.Time
}

func New(registry Registry, git Git) *Service {
	return &Service{registry: registry, git: git, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) List(ctx context.Context) ([]core.Workspace, error) {
	workspaces, err := s.registry.ListWorkspaces(ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(workspaces, func(i, j int) bool {
		if workspaces[i].Label == workspaces[j].Label {
			return workspaces[i].ID < workspaces[j].ID
		}
		return workspaces[i].Label < workspaces[j].Label
	})
	return workspaces, nil
}

func (s *Service) Inspect(ctx context.Context, labelOrID string) (core.Workspace, error) {
	workspaces, err := s.registry.ListWorkspaces(ctx)
	if err != nil {
		return core.Workspace{}, err
	}
	return resolveWorkspace(workspaces, labelOrID)
}

func (s *Service) Attach(ctx context.Context, path, label string) (core.Workspace, error) {
	observation, err := s.git.Inspect(ctx, path)
	if err != nil {
		return core.Workspace{}, err
	}
	repositories, err := s.registry.ListRepositories(ctx)
	if err != nil {
		return core.Workspace{}, err
	}
	workspaces, err := s.registry.ListWorkspaces(ctx)
	if err != nil {
		return core.Workspace{}, err
	}

	now := s.now()
	repository, found := repositoryByCommonDir(repositories, observation.CommonDir)
	if !found {
		repository = core.Repository{
			SchemaVersion: core.SchemaVersion, ID: core.NewRepositoryID(),
			CommonDir: observation.CommonDir, Bare: observation.Bare,
			CreatedAt: now, LastSeenAt: now,
		}
	} else {
		repository.Bare = observation.Bare
		repository.LastSeenAt = now
	}

	if existing, ok := workspaceByGitDir(workspaces, observation.GitDir); ok {
		existing.RepositoryID = repository.ID
		existing.Root = observation.Root
		existing.GitDir = observation.GitDir
		existing.Branch = observation.Branch
		existing.LastSeenAt = now
		if label != "" {
			existing.Label = resolvedLabel(label, existing.ID, workspaces)
		}
		if err := s.persist(ctx, repository, existing); err != nil {
			return core.Workspace{}, err
		}
		return existing, nil
	}

	id := core.NewWorkspaceID()
	if label == "" {
		label = derivedLabel(observation)
	}
	record := core.Workspace{
		SchemaVersion: core.SchemaVersion, ID: id, RepositoryID: repository.ID,
		Label: resolvedLabel(label, id, workspaces), Root: observation.Root,
		GitDir: observation.GitDir, Branch: observation.Branch,
		CreatedAt: now, LastSeenAt: now,
	}
	if err := s.persist(ctx, repository, record); err != nil {
		return core.Workspace{}, err
	}
	return record, nil
}

func (s *Service) Create(ctx context.Context, request CreateRequest) (core.Workspace, error) {
	observation, err := s.git.Inspect(ctx, request.Repository)
	if err != nil {
		return core.Workspace{}, err
	}
	worktrees, err := s.git.ListWorktrees(ctx, observation.CommonDir)
	if err != nil {
		return core.Workspace{}, err
	}

	if request.Path != "" {
		target, err := absoluteClean(request.Path)
		if err != nil {
			return core.Workspace{}, err
		}
		if existing, ok := worktreeByRoot(worktrees, target); ok {
			return s.Attach(ctx, existing.Root, request.Label)
		}
	}
	if existing, ok := worktreeByRef(worktrees, request.Ref); ok {
		return s.Attach(ctx, existing.Root, request.Label)
	}

	label := request.Label
	if label == "" {
		label = labelFromRef(request.Ref)
	}
	if label == "" {
		label = "workspace"
	}
	target, err := defaultOrExplicitTarget(observation, request.Path, label)
	if err != nil {
		return core.Workspace{}, err
	}
	if existing, ok := worktreeByRoot(worktrees, target); ok {
		return s.Attach(ctx, existing.Root, label)
	}
	target, err = availableTarget(target, request.Path != "", observation.CommonDir, request.Ref, label)
	if err != nil {
		return core.Workspace{}, err
	}
	if err := s.git.AddWorktree(ctx, observation.CommonDir, target, request.Ref); err != nil {
		return core.Workspace{}, err
	}
	return s.Attach(ctx, target, label)
}

func (s *Service) Rename(ctx context.Context, labelOrID, newLabel string) (core.Workspace, error) {
	workspaces, err := s.registry.ListWorkspaces(ctx)
	if err != nil {
		return core.Workspace{}, err
	}
	record, err := resolveWorkspace(workspaces, labelOrID)
	if err != nil {
		return core.Workspace{}, err
	}
	record.Label = resolvedLabel(newLabel, record.ID, workspaces)
	record.LastSeenAt = s.now()
	if err := s.registry.SaveWorkspace(ctx, record); err != nil {
		return core.Workspace{}, err
	}
	return record, nil
}

func (s *Service) Forget(ctx context.Context, labelOrID string) (core.Workspace, error) {
	record, err := s.Inspect(ctx, labelOrID)
	if err != nil {
		return core.Workspace{}, err
	}
	if err := s.registry.DeleteWorkspace(ctx, record.ID); err != nil {
		return core.Workspace{}, err
	}
	return record, nil
}

func (s *Service) Remove(ctx context.Context, labelOrID string, force bool) (core.Workspace, error) {
	record, err := s.Inspect(ctx, labelOrID)
	if err != nil {
		return core.Workspace{}, err
	}
	repositories, err := s.registry.ListRepositories(ctx)
	if err != nil {
		return core.Workspace{}, err
	}
	repository, ok := repositoryByID(repositories, record.RepositoryID)
	if !ok || filepath.Clean(record.GitDir) == filepath.Clean(repository.CommonDir) {
		return core.Workspace{}, ErrUnsafeWorktree
	}
	worktrees, err := s.git.ListWorktrees(ctx, repository.CommonDir)
	if err != nil {
		return core.Workspace{}, err
	}
	existing, ok := worktreeByRoot(worktrees, record.Root)
	if !ok || existing.Bare {
		return core.Workspace{}, ErrUnsafeWorktree
	}
	dirty, err := s.git.Dirty(ctx, record.Root)
	if err != nil {
		return core.Workspace{}, fmt.Errorf("%w: unable to verify worktree status", ErrUnsafeWorktree)
	}
	if dirty && !force {
		return core.Workspace{}, ErrDirtyWorktree
	}
	if err := s.git.RemoveWorktree(ctx, repository.CommonDir, record.Root, force); err != nil {
		return core.Workspace{}, err
	}
	if err := s.registry.DeleteWorkspace(ctx, record.ID); err != nil {
		return core.Workspace{}, err
	}
	return record, nil
}

func (s *Service) persist(ctx context.Context, repository core.Repository, record core.Workspace) error {
	if err := s.registry.SaveRepository(ctx, repository); err != nil {
		return err
	}
	return s.registry.SaveWorkspace(ctx, record)
}
