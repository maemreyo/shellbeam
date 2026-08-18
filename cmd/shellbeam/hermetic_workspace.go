package main

import (
	"context"

	hermeticapp "github.com/maemreyo/shellbeam/internal/app/hermetic"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type hermeticWorkspaceSource struct {
	fresh *checkpointWorkspaceSource
}

func newHermeticWorkspaceSource(workspaces checkpointWorkspaceLookup, observer checkpointFreshWorkspaceObserver) *hermeticWorkspaceSource {
	return &hermeticWorkspaceSource{fresh: newCheckpointWorkspaceSource(workspaces, observer, nil)}
}

func (s *hermeticWorkspaceSource) ResolveFresh(ctx context.Context, workspaceID string) (hermeticapp.WorkspaceContext, error) {
	got, err := s.fresh.ResolveFresh(ctx, workspaceID)
	if err != nil {
		return hermeticapp.WorkspaceContext{}, err
	}
	return hermeticapp.WorkspaceContext{
		WorkspaceID: workspacecore.WorkspaceID(got.WorkspaceID), RepositoryID: workspacecore.RepositoryID(got.RepositoryID), Root: got.Root, SourceGeneration: got.SourceGeneration,
	}, nil
}

var _ hermeticapp.WorkspaceSource = (*hermeticWorkspaceSource)(nil)
