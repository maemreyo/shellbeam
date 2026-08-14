package project

import (
	"context"
	"fmt"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/project"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type Service struct {
	workspaces WorkspaceLookup
	loader     Loader
}

func New(workspaces WorkspaceLookup, loader Loader) *Service {
	return &Service{workspaces: workspaces, loader: loader}
}

func (s *Service) Inspect(ctx context.Context, workspaceID string) (core.Inspection, error) {
	id, err := workspace.ParseWorkspaceID(workspaceID)
	if err != nil {
		return core.Inspection{}, failure.New(failure.InvalidInput, map[string]string{"field": "workspace_id"}, err)
	}
	records, err := s.workspaces.ListWorkspaces(ctx)
	if err != nil {
		return core.Inspection{}, failure.New(failure.PersistenceUnavailable, nil, err)
	}
	var record workspace.Workspace
	found := false
	for _, candidate := range records {
		if candidate.ID == id {
			record, found = candidate, true
			break
		}
	}
	if !found {
		return core.Inspection{}, failure.New(failure.WorkspaceNotFound, map[string]string{"workspace_id": workspaceID}, fmt.Errorf("workspace not found"))
	}
	load := s.loader.Load(ctx, record.Root)
	input := core.StatusInput{
		LoadState: load.State, ManifestDigest: load.ManifestDigest, Code: load.Code,
	}
	var manifest *core.Manifest
	if load.Parsed != nil {
		input.SchemaVersion = load.Parsed.Manifest.SchemaVersion
		input.DiscoveryFingerprint = load.Parsed.Fingerprint
		manifest = &load.Parsed.Manifest
	}
	return core.NewInspection(input, manifest), nil
}
