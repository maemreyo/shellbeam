package mutationscope

import (
	"context"
	"errors"
	"fmt"

	activity "github.com/maemreyo/shellbeam/internal/core/activity"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/mutationscope"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type Service struct {
	store Store
	clock Clock
}

func New(store Store, clock Clock) *Service {
	if clock == nil {
		clock = systemClock{}
	}
	return &Service{store: store, clock: clock}
}

func (s *Service) Set(ctx context.Context, request SetRequest) (MutationResult, error) {
	request, fingerprint, err := normalizeSetRequest(request)
	if err != nil {
		return MutationResult{}, err
	}
	if err := s.available(ctx); err != nil {
		return MutationResult{}, err
	}
	if receipt, found, err := s.store.LoadMutationReceipt(ctx, request.MutationID); err != nil {
		return MutationResult{}, storeError(err)
	} else if found {
		return s.replaySet(ctx, request.ScopeID, fingerprint, receipt)
	}
	registered, err := s.workspaceRegistered(ctx, request.WorkspaceID)
	if err != nil {
		return MutationResult{}, err
	}
	if !registered {
		return MutationResult{}, failure.New(failure.WorkspaceNotFound, nil, nil)
	}

	now := s.clock.Now().UTC()
	expires := now.Add(ttlDuration(request))
	scope := core.Scope{
		SchemaVersion: core.SchemaVersion, ScopeID: request.ScopeID, ActivityID: request.ActivityID,
		WorkspaceID: request.WorkspaceID, Mode: request.Mode, Paths: append([]string(nil), request.Paths...),
		DeclaredAt: now, ExpiresAt: expires, RevisionID: request.MutationID,
	}
	identity := core.ScopeIdentity{SchemaVersion: core.SchemaVersion, ScopeID: request.ScopeID, ActivityID: request.ActivityID, WorkspaceID: request.WorkspaceID, BoundAt: now}
	intent := core.MutationReceipt{
		SchemaVersion: core.SchemaVersion, MutationID: request.MutationID, RequestFingerprint: fingerprint,
		Result: core.ResultSet, ScopeID: request.ScopeID, CommittedAt: now, ExpiresAt: expires,
	}
	if err := s.store.CommitMutationScopeSet(ctx, scope, identity, intent); err != nil {
		return MutationResult{}, storeError(err)
	}
	receipt, found, err := s.store.LoadMutationReceipt(ctx, request.MutationID)
	if err != nil {
		return MutationResult{}, storeError(err)
	}
	if !found {
		return MutationResult{}, failure.New(failure.PersistenceUnavailable, nil, fmt.Errorf("mutation receipt missing after set"))
	}
	return s.setResult(ctx, request.ScopeID, receipt, false)
}

func (s *Service) Release(ctx context.Context, request ReleaseRequest) (MutationResult, error) {
	request, fingerprint, err := normalizeReleaseRequest(request)
	if err != nil {
		return MutationResult{}, err
	}
	if err := s.available(ctx); err != nil {
		return MutationResult{}, err
	}
	if receipt, found, err := s.store.LoadMutationReceipt(ctx, request.MutationID); err != nil {
		return MutationResult{}, storeError(err)
	} else if found {
		if receipt.RequestFingerprint != fingerprint || receipt.ScopeID != request.ScopeID || (receipt.Result != core.ResultReleased && receipt.Result != core.ResultAlreadyAbsent) {
			return MutationResult{}, mutationConflict(request.MutationID, request.ScopeID)
		}
		return MutationResult{Receipt: receipt, Replayed: true}, nil
	}
	intent := core.MutationReceipt{
		SchemaVersion: core.SchemaVersion, MutationID: request.MutationID, RequestFingerprint: fingerprint,
		Result: core.ResultReleased, ScopeID: request.ScopeID, CommittedAt: s.clock.Now().UTC(),
	}
	if err := s.store.CommitMutationScopeRelease(ctx, request.ScopeID, intent); err != nil {
		return MutationResult{}, storeError(err)
	}
	receipt, found, err := s.store.LoadMutationReceipt(ctx, request.MutationID)
	if err != nil {
		return MutationResult{}, storeError(err)
	}
	if !found {
		return MutationResult{}, failure.New(failure.PersistenceUnavailable, nil, fmt.Errorf("mutation receipt missing after release"))
	}
	return MutationResult{Receipt: receipt}, nil
}

func (s *Service) Inspect(ctx context.Context, request InspectRequest) (core.InspectResult, error) {
	if err := s.available(ctx); err != nil {
		return core.InspectResult{}, err
	}
	if _, err := workspace.ParseWorkspaceID(string(request.WorkspaceID)); err != nil {
		return core.InspectResult{}, invalidScope("workspace_id", "invalid_id", err)
	}
	if request.ActivityID != "" {
		if _, err := activity.ParseID(request.ActivityID); err != nil {
			return core.InspectResult{}, invalidScope("activity_id", "invalid_id", err)
		}
	}
	all, err := s.store.ListMutationScopes(ctx, "", request.WorkspaceID)
	if err != nil {
		return core.InspectResult{}, storeError(err)
	}
	all = activeScopesAt(all, s.clock.Now().UTC())
	limit := core.MaxActiveScopesPerWorkspace
	if request.ActivityID != "" {
		limit = core.MaxActiveScopesPerActivity
	}
	selected, activeCount, scopesTruncated := selectedScopes(all, request.ActivityID, limit)
	advisories, advisoryCount, advisoriesTruncated := evaluateAdvisories(all, request.ActivityID, "", core.MaxAdvisories)
	return core.InspectResult{
		ActiveScopes: selected, Advisories: advisories, ActiveCount: activeCount, AdvisoryCount: advisoryCount,
		ActiveScopeLimit: limit, AdvisoryLimit: core.MaxAdvisories,
		ScopesTruncated: scopesTruncated, AdvisoriesTruncated: advisoriesTruncated,
	}, nil
}

func (s *Service) replaySet(ctx context.Context, scopeID, fingerprint string, receipt core.MutationReceipt) (MutationResult, error) {
	if receipt.RequestFingerprint != fingerprint || receipt.ScopeID != scopeID || receipt.Result != core.ResultSet {
		return MutationResult{}, mutationConflict(receipt.MutationID, scopeID)
	}
	return s.setResult(ctx, scopeID, receipt, true)
}

func (s *Service) setResult(ctx context.Context, scopeID string, receipt core.MutationReceipt, replayed bool) (MutationResult, error) {
	current, found, err := s.store.LoadMutationScope(ctx, scopeID)
	if err != nil {
		return MutationResult{}, storeError(err)
	}
	result := MutationResult{Receipt: receipt, Replayed: replayed, AdvisoryLimit: core.MaxAdvisories}
	if !found {
		return result, nil
	}
	copy := current
	result.Scope = &copy
	result.CurrentRevision = current.RevisionID == receipt.MutationID
	all, err := s.store.ListMutationScopes(ctx, "", current.WorkspaceID)
	if err != nil {
		return MutationResult{}, storeError(err)
	}
	all = activeScopesAt(all, s.clock.Now().UTC())
	result.Advisories, result.AdvisoryCount, result.AdvisoriesTruncated = evaluateAdvisories(all, "", current.ScopeID, core.MaxAdvisories)
	return result, nil
}

func (s *Service) workspaceRegistered(ctx context.Context, id workspace.WorkspaceID) (bool, error) {
	records, err := s.store.ListWorkspaces(ctx)
	if err != nil {
		return false, storeError(err)
	}
	for _, record := range records {
		if record.ID == id {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) available(ctx context.Context) error {
	if s == nil || s.store == nil || s.clock == nil {
		return failure.New(failure.FeatureUnavailable, nil, nil)
	}
	return ctx.Err()
}

func mutationConflict(mutationID, scopeID string) error {
	return failure.New(failure.MutationMetadataConflict, map[string]string{"mutation_id": mutationID, "scope_id": scopeID}, nil)
}

func storeError(err error) error {
	if err == nil {
		return nil
	}
	var typed *failure.Failure
	if errors.As(err, &typed) {
		return err
	}
	return failure.New(failure.PersistenceUnavailable, nil, err)
}
