package main

import (
	"context"

	environmentapp "github.com/maemreyo/shellbeam/internal/app/environment"
	projectapp "github.com/maemreyo/shellbeam/internal/app/project"
	environmentcore "github.com/maemreyo/shellbeam/internal/core/environment"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

type projectEnvironmentManifestProvider struct {
	project *projectapp.Service
}

func (p projectEnvironmentManifestProvider) Manifest(ctx context.Context, workspaceID string) (environmentapp.ManifestView, error) {
	if p.project == nil {
		return environmentapp.ManifestView{}, failure.New(failure.EnvironmentObservationUnavailable, map[string]string{"reason": "manifest_provider_unavailable"}, nil)
	}
	inspection, err := p.project.Inspect(ctx, workspaceID)
	if err != nil {
		return environmentapp.ManifestView{}, err
	}
	if inspection.Manifest == nil || inspection.ManifestDigest == "" {
		return environmentapp.ManifestView{}, failure.New(failure.EnvironmentObservationUnavailable, map[string]string{"reason": "manifest_unavailable"}, nil)
	}
	return environmentapp.ManifestView{WorkspaceID: workspaceID, ManifestDigest: inspection.ManifestDigest, Manifest: *inspection.Manifest}, nil
}

type daemonEnvironmentBindingProvider struct {
	environment *environmentapp.Service
}

func (p daemonEnvironmentBindingProvider) CachedEnvironmentBinding(reservation operation.Reservation) (environmentcore.Binding, bool) {
	if p.environment == nil {
		return environmentcore.Binding{}, false
	}
	manifestDigest := ""
	if reservation.ProjectCommand != nil {
		manifestDigest = reservation.ProjectCommand.ManifestDigest
	} else if reservation.WorkspaceID != "" {
		// A raw workspace command does not freeze manifest identity into the
		// reservation, so binding a workspace-scoped cached snapshot would
		// overclaim compatibility. Typed commands carry that authority.
		return environmentcore.Binding{}, false
	}
	mode := string(reservation.ExecutionMode)
	identity := reservation.Executable
	if (mode != "shell" && mode != "argv") || identity == "" {
		return environmentcore.Binding{}, false
	}
	return p.environment.CachedBinding(environmentapp.BindingRequest{
		WorkspaceID: reservation.WorkspaceID, ManifestDigest: manifestDigest,
		Execution: environmentcore.ExecutionContext{Mode: mode, Identity: identity},
	})
}
