package project

import (
	"context"
	"fmt"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/project"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type Service struct {
	workspaces WorkspaceLookup
	loader     Loader
	reviews    ReviewStore
	readiness  *readinessRuntime
}

type ReviewRequest struct {
	Fingerprint   string
	ReviewedAt    time.Time
	ToolVersion   string
	ReviewerClass string
	SourceClass   string
}

func New(workspaces WorkspaceLookup, loader Loader, reviewStores ...ReviewStore) *Service {
	var reviews ReviewStore
	if len(reviewStores) > 0 {
		reviews = reviewStores[0]
	}
	return &Service{workspaces: workspaces, loader: loader, reviews: reviews}
}

func (s *Service) Inspect(ctx context.Context, workspaceID string) (core.Inspection, error) {
	record, err := s.workspace(ctx, workspaceID)
	if err != nil {
		return core.Inspection{}, err
	}
	load := s.loader.Load(ctx, record.Root)
	if load.State != core.LoadValid {
		return inspectionFromLoad(load, nil), nil
	}
	if s.reviews == nil {
		return inspectionFromLoad(load, nil), nil
	}
	review, found, err := s.reviews.LoadProjectReview(ctx, record.RepositoryID)
	if err != nil {
		return core.Inspection{}, failure.New(failure.PersistenceUnavailable, nil, err)
	}
	if !found {
		return inspectionFromLoad(load, nil), nil
	}
	return inspectionFromLoad(load, &review), nil
}

func (s *Service) Review(ctx context.Context, workspaceID string, request ReviewRequest) (core.Inspection, error) {
	if s.reviews == nil {
		return core.Inspection{}, failure.New(failure.PersistenceUnavailable, nil, fmt.Errorf("project review store is unavailable"))
	}
	record, err := s.workspace(ctx, workspaceID)
	if err != nil {
		return core.Inspection{}, err
	}
	load := s.loader.Load(ctx, record.Root)
	if load.State != core.LoadValid || load.Parsed == nil {
		return core.Inspection{}, fmt.Errorf("project review requires a valid supported manifest")
	}
	discovery := load.DiscoveryFingerprint
	if discovery == "" {
		discovery = load.Parsed.Fingerprint
	}
	if request.Fingerprint != discovery {
		return core.Inspection{}, core.ChangedDuringResolveError()
	}
	review := core.Review{
		RepositoryID: record.RepositoryID, ManifestFingerprint: load.Parsed.Fingerprint,
		DiscoveryFingerprint: discovery, ManifestSchemaVersion: load.Parsed.Manifest.SchemaVersion,
		ReviewedAt: request.ReviewedAt, ToolVersion: request.ToolVersion,
		ReviewerClass: request.ReviewerClass, SourceClass: request.SourceClass,
	}
	if err := review.Validate(); err != nil {
		return core.Inspection{}, err
	}
	confirmed := s.loader.Load(ctx, record.Root)
	if confirmed.State != core.LoadValid || confirmed.Parsed == nil {
		return core.Inspection{}, core.ChangedDuringResolveError()
	}
	confirmedDiscovery := confirmed.DiscoveryFingerprint
	if confirmedDiscovery == "" {
		confirmedDiscovery = confirmed.Parsed.Fingerprint
	}
	if confirmedDiscovery != request.Fingerprint || confirmed.Parsed.Fingerprint != review.ManifestFingerprint || confirmed.Parsed.Manifest.SchemaVersion != review.ManifestSchemaVersion {
		return core.Inspection{}, core.ChangedDuringResolveError()
	}
	if err := s.reviews.SaveProjectReview(ctx, review); err != nil {
		return core.Inspection{}, failure.New(failure.PersistenceUnavailable, nil, err)
	}
	return inspectionFromLoad(load, &review), nil
}

func (s *Service) workspace(ctx context.Context, workspaceID string) (workspace.Workspace, error) {
	id, err := workspace.ParseWorkspaceID(workspaceID)
	if err != nil {
		return workspace.Workspace{}, failure.New(failure.InvalidInput, map[string]string{"field": "workspace_id"}, err)
	}
	records, err := s.workspaces.ListWorkspaces(ctx)
	if err != nil {
		return workspace.Workspace{}, failure.New(failure.PersistenceUnavailable, nil, err)
	}
	for _, candidate := range records {
		if candidate.ID == id {
			return candidate, nil
		}
	}
	return workspace.Workspace{}, failure.New(failure.WorkspaceNotFound, map[string]string{"workspace_id": workspaceID}, fmt.Errorf("workspace not found"))
}

func inspectionFromLoad(load core.LoadResult, review *core.Review) core.Inspection {
	input := core.StatusInput{
		LoadState: load.State, ManifestDigest: load.ManifestDigest,
		DiscoveryFingerprint: load.DiscoveryFingerprint, DetectedFamilies: load.DetectedFamilies, DiscoveryEvidence: load.DiscoveryEvidence, Code: load.Code, Review: review,
	}
	var manifest *core.Manifest
	if load.Parsed != nil {
		input.SchemaVersion = load.Parsed.Manifest.SchemaVersion
		input.ManifestFingerprint = load.Parsed.Fingerprint
		if input.DiscoveryFingerprint == "" {
			input.DiscoveryFingerprint = load.Parsed.Fingerprint
		}
		manifest = &load.Parsed.Manifest
	}
	return core.NewInspection(input, manifest)
}
