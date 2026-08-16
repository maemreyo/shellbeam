package main

import (
	"context"

	checkpointadapter "github.com/maemreyo/shellbeam/internal/adapter/checkpoint/localfs"
	checkpointapp "github.com/maemreyo/shellbeam/internal/app/checkpoint"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	checkpointcore "github.com/maemreyo/shellbeam/internal/core/checkpoint"
	"github.com/maemreyo/shellbeam/internal/core/failure"
)

type checkpointProviderFactory func(stateDir, runtimeDir string) (checkpointapp.Provider, error)

func composeSafetyCheckpoints(
	enabled bool,
	stateDir, runtimeDir string,
	repository checkpointapp.Repository,
	workspace checkpointapp.WorkspaceSource,
	catalog capability.Catalog,
	factory checkpointProviderFactory,
) (*checkpointapp.Service, capability.Catalog) {
	if !enabled {
		return nil, catalog
	}
	if factory == nil {
		factory = newLocalCheckpointProvider
	}
	provider, err := factory(stateDir, runtimeDir)
	if err != nil || provider == nil {
		return nil, catalog
	}
	advertised := catalog.WithSafetyCheckpoints(provider.Identity(), provider.ConflictDetection())
	if advertised.Features[capability.FeatureSafetyCheckpoints] != capability.Available || advertised.SafetyCheckpoints == nil {
		return nil, catalog
	}
	return checkpointapp.New(repository, workspace, provider), advertised
}

func newLocalCheckpointProvider(stateDir, runtimeDir string) (checkpointapp.Provider, error) {
	if err := checkpointadapter.Probe(stateDir); err != nil {
		return nil, err
	}
	return checkpointadapter.New(stateDir, runtimeDir), nil
}

func (a *daemonActions) CreateCheckpoint(ctx context.Context, request checkpointcore.CreateRequest) (checkpointcore.Checkpoint, error) {
	if a == nil || a.checkpoints == nil {
		return checkpointcore.Checkpoint{}, checkpointFeatureUnavailable()
	}
	return a.checkpoints.Create(ctx, request)
}

func (a *daemonActions) RestoreCheckpoint(ctx context.Context, request checkpointcore.RestoreRequest) (checkpointcore.RestoreResult, error) {
	if a == nil || a.checkpoints == nil {
		return checkpointcore.RestoreResult{}, checkpointFeatureUnavailable()
	}
	return a.checkpoints.Restore(ctx, request)
}

func (a *daemonActions) InspectCheckpoint(ctx context.Context, checkpointID string) (checkpointapp.CheckpointInspection, error) {
	if a == nil || a.checkpoints == nil {
		return checkpointapp.CheckpointInspection{}, checkpointFeatureUnavailable()
	}
	return a.checkpoints.Inspect(ctx, checkpointID)
}

func checkpointFeatureUnavailable() error {
	return failure.New(failure.FeatureUnavailable, map[string]string{"feature": string(capability.FeatureSafetyCheckpoints)}, nil)
}
