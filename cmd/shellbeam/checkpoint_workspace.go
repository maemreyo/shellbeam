package main

import (
	"context"
	"fmt"
	"strings"

	checkpointapp "github.com/maemreyo/shellbeam/internal/app/checkpoint"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type checkpointWorkspaceLookup interface {
	Inspect(context.Context, string) (workspacecore.Workspace, error)
}

type checkpointFreshWorkspaceObserver interface {
	ObserveFresh(context.Context, string) workspacecore.FastSnapshot
}

type checkpointCoherenceInvalidator interface {
	Invalidate(string)
}

type checkpointWorkspaceSource struct {
	workspaces checkpointWorkspaceLookup
	observer   checkpointFreshWorkspaceObserver
	coherence  checkpointCoherenceInvalidator
}

func newCheckpointWorkspaceSource(
	workspaces checkpointWorkspaceLookup,
	observer checkpointFreshWorkspaceObserver,
	coherence checkpointCoherenceInvalidator,
) *checkpointWorkspaceSource {
	return &checkpointWorkspaceSource{workspaces: workspaces, observer: observer, coherence: coherence}
}

func (s *checkpointWorkspaceSource) ResolveFresh(ctx context.Context, workspaceID string) (checkpointapp.WorkspaceContext, error) {
	if err := ctx.Err(); err != nil {
		return checkpointapp.WorkspaceContext{}, err
	}
	if s == nil || s.workspaces == nil || s.observer == nil {
		return checkpointapp.WorkspaceContext{}, fmt.Errorf("checkpoint workspace source unavailable")
	}
	record, err := s.workspaces.Inspect(ctx, workspaceID)
	if err != nil {
		return checkpointapp.WorkspaceContext{}, err
	}
	if string(record.ID) != workspaceID || record.RepositoryID == "" || !strings.HasPrefix(record.Root, "/") {
		return checkpointapp.WorkspaceContext{}, fmt.Errorf("checkpoint workspace binding mismatch")
	}
	snapshot := s.observer.ObserveFresh(ctx, record.Root)
	if err := snapshot.Validate(); err != nil {
		return checkpointapp.WorkspaceContext{}, fmt.Errorf("checkpoint fresh workspace observation invalid: %w", err)
	}
	if snapshot.Quality != workspacecore.QualityFresh || snapshot.WorkspaceID != record.ID || snapshot.RepositoryID != record.RepositoryID || !validCheckpointGeneration(snapshot.Generation) {
		return checkpointapp.WorkspaceContext{}, fmt.Errorf("checkpoint fresh workspace observation mismatch")
	}
	return checkpointapp.WorkspaceContext{
		WorkspaceID:      string(record.ID),
		RepositoryID:     string(record.RepositoryID),
		Root:             record.Root,
		SourceGeneration: snapshot.Generation,
	}, nil
}

func (s *checkpointWorkspaceSource) InvalidateAfterMutation(ctx context.Context, workspaceID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || s.workspaces == nil || s.coherence == nil {
		return fmt.Errorf("checkpoint workspace invalidation unavailable")
	}
	record, err := s.workspaces.Inspect(ctx, workspaceID)
	if err != nil {
		return err
	}
	if string(record.ID) != workspaceID {
		return fmt.Errorf("checkpoint workspace binding mismatch")
	}
	s.coherence.Invalidate("checkpoint_restore")
	return nil
}

func validCheckpointGeneration(generation string) bool {
	if len(generation) != 68 || !strings.HasPrefix(generation, "gen_") {
		return false
	}
	for _, r := range generation[4:] {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}

var _ checkpointapp.WorkspaceSource = (*checkpointWorkspaceSource)(nil)
