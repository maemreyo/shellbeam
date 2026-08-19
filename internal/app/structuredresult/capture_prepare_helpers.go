package structuredresult

import (
	"context"
	"fmt"

	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func (p *CapturePreparer) prepareReplay(ctx context.Context, req PreSpawnCaptureRequest, record CaptureAuthorityRecord) (PreSpawnCaptureResult, error) {
	if err := record.Validate(); err != nil {
		return PreSpawnCaptureResult{}, err
	}
	intent := record.Authority.Intent
	if intent.OperationID != string(req.OperationID) || intent.SessionID != string(req.SessionID) || intent.RepositoryID != req.RepositoryID || intent.WorkspaceID != req.WorkspaceID || intent.MaxBlobBytes != req.MaxBlobBytes {
		return PreSpawnCaptureResult{}, fmt.Errorf("capture replay metadata conflict")
	}
	result := PreSpawnCaptureResult{Record: &record, StructuredCaptureDigest: record.StructuredCaptureDigest, InvocationQualified: true, Replayed: true}
	claim, collided, claimErr := p.coordinator.RegisterReplayManagedPathClaim(ctx, req.OperationID, record.Authority.Intent.WorkspaceID, record.Authority.Intent.NormalizedWorkspacePath, record.State)
	if claim == nil {
		result.CaptureUnavailable = claimErr
		return result, nil
	}
	result.Claim = claim
	result.Collision = collided
	if claimErr != nil {
		result.CaptureUnavailable = claimErr
	}
	return result, nil
}

func (p *CapturePreparer) abandonPrepared(ctx context.Context, id operation.ID) error {
	record, err := p.store.FindCaptureAuthority(ctx, id)
	if err != nil {
		return err
	}
	if record.State != CaptureAuthorityPrepared {
		return nil
	}
	_, err = p.store.MarkCaptureAuthorityState(ctx, id, CaptureAuthorityAbandoned)
	return err
}

func (p *CapturePreparer) validatePreparationRequest(req PreSpawnCaptureRequest) error {
	if p == nil || p.store == nil || p.baseline == nil || p.presence == nil || p.coordinator == nil {
		return fmt.Errorf("capture preparer unavailable")
	}
	if _, err := operation.ParseID(string(req.OperationID)); err != nil {
		return err
	}
	if _, err := operation.ParseSessionID(string(req.SessionID)); err != nil {
		return err
	}
	if req.RepositoryID == "" || req.WorkspaceID == "" || req.WorkspaceRoot == "" || req.MaxBlobBytes < 1 || req.MaxBlobBytes > MaxArtifactBlobBytes {
		return fmt.Errorf("invalid pre-spawn capture request")
	}
	return nil
}

func buildCaptureAuthority(req PreSpawnCaptureRequest, binding PytestInvocationBindingV1, baseline CaptureBaselineIdentity) (CaptureAuthority, error) {
	producerDigest, err := binding.ProducerBindingDigest()
	if err != nil {
		return CaptureAuthority{}, err
	}
	intent := ArtifactCaptureIntent{
		SchemaVersion: ArtifactCaptureIntentSchemaV1,
		OperationID:   string(req.OperationID), SessionID: string(req.SessionID), RepositoryID: req.RepositoryID, WorkspaceID: req.WorkspaceID,
		AdapterID:         PytestJUnitAdapterID,
		DeclaredPathToken: binding.JUnitOutput.DeclaredPathToken, NormalizedWorkspacePath: binding.JUnitOutput.NormalizedWorkspacePath,
		ExpectedKind: CaptureExpectedRegularFile, MaxBlobBytes: req.MaxBlobBytes, ProducerBindingDigest: producerDigest, Baseline: baseline,
	}
	authority := CaptureAuthority{SchemaVersion: CaptureAuthoritySchemaV1, PytestInvocation: &binding, Intent: intent}
	return authority, authority.Validate()
}

func (c *CaptureCoordinator) RegisterReplayManagedPathClaim(ctx context.Context, id operation.ID, workspaceID, normalizedPath string, state CaptureAuthorityState) (*ManagedArtifactPathClaim, bool, error) {
	if state != CaptureAuthorityPrepared && state != CaptureAuthorityManagedPathCollision && state != CaptureAuthorityAbandoned {
		return nil, false, fmt.Errorf("invalid replay capture authority state")
	}
	claim := &ManagedArtifactPathClaim{
		registry: c.registry, key: managedArtifactPathKey{workspaceID: workspaceID, path: normalizedPath}, operationID: id,
		collided: state == CaptureAuthorityManagedPathCollision, persistCollision: state == CaptureAuthorityPrepared,
	}
	return c.registerClaim(ctx, claim, nil)
}
