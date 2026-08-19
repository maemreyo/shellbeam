package structuredresult

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/maemreyo/shellbeam/internal/core/operation"
)

var ErrArtifactPathAuthorityCapacity = fmt.Errorf("artifact_path_authority_capacity")

const MaxActiveArtifactPathAuthoritiesGlobal = 4

type ArtifactPathAuthority interface {
	NormalizedWorkspacePath() string
	FinalName() string
	BaselineDigest() string
	Close() error
}

type CaptureAuthorityStateStore interface {
	MarkCaptureAuthorityState(context.Context, operation.ID, CaptureAuthorityState) (CaptureAuthorityRecord, error)
}

type managedArtifactPathKey struct {
	workspaceID string
	path        string
}

type artifactPathRegistry struct {
	mu          sync.Mutex
	maxSlots    int
	activeSlots int
	claims      map[managedArtifactPathKey]map[operation.ID]*ManagedArtifactPathClaim
}

type ArtifactPathAuthoritySlot struct {
	registry    *artifactPathRegistry
	released    bool
	transferred bool
}

type CaptureCoordinator struct {
	store    CaptureAuthorityStateStore
	registry *artifactPathRegistry
}

type ManagedArtifactPathClaim struct {
	mu               sync.Mutex
	registry         *artifactPathRegistry
	key              managedArtifactPathKey
	operationID      operation.ID
	authority        ArtifactPathAuthority
	slot             *ArtifactPathAuthoritySlot
	collided         bool
	persistCollision bool
	released         bool
}

var globalArtifactPathRegistry = newArtifactPathRegistry(MaxActiveArtifactPathAuthoritiesGlobal)

func NewCaptureCoordinator(store CaptureAuthorityStateStore) *CaptureCoordinator {
	return newCaptureCoordinatorWithRegistry(store, globalArtifactPathRegistry)
}

func newCaptureCoordinatorWithRegistry(store CaptureAuthorityStateStore, registry *artifactPathRegistry) *CaptureCoordinator {
	return &CaptureCoordinator{store: store, registry: registry}
}

func newArtifactPathRegistry(max int) *artifactPathRegistry {
	return &artifactPathRegistry{maxSlots: max, claims: map[managedArtifactPathKey]map[operation.ID]*ManagedArtifactPathClaim{}}
}

func (c *CaptureCoordinator) AcquirePathAuthoritySlot(ctx context.Context) (*ArtifactPathAuthoritySlot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c == nil || c.registry == nil || c.registry.maxSlots < 1 {
		return nil, ErrArtifactPathAuthorityCapacity
	}
	c.registry.mu.Lock()
	defer c.registry.mu.Unlock()
	if c.registry.activeSlots >= c.registry.maxSlots {
		return nil, ErrArtifactPathAuthorityCapacity
	}
	c.registry.activeSlots++
	return &ArtifactPathAuthoritySlot{registry: c.registry}, nil
}

func (s *ArtifactPathAuthoritySlot) Release() error {
	if s == nil || s.registry == nil {
		return nil
	}
	r := s.registry
	r.mu.Lock()
	defer r.mu.Unlock()
	if s.released || s.transferred {
		return nil
	}
	r.releaseSlotLocked(s)
	return nil
}

func (r *artifactPathRegistry) releaseSlotLocked(slot *ArtifactPathAuthoritySlot) {
	if slot == nil || slot.released {
		return
	}
	slot.released = true
	if r.activeSlots > 0 {
		r.activeSlots--
	}
}

func (c *CaptureCoordinator) RegisterManagedPathClaim(ctx context.Context, slot *ArtifactPathAuthoritySlot, id operation.ID, workspaceID string, authority ArtifactPathAuthority) (*ManagedArtifactPathClaim, bool, error) {
	if c == nil || c.registry == nil || c.store == nil || slot == nil || slot.registry != c.registry || authority == nil || workspaceID == "" || authority.NormalizedWorkspacePath() == "" {
		return nil, false, fmt.Errorf("invalid managed artifact path claim")
	}
	claim := &ManagedArtifactPathClaim{
		registry: c.registry, key: managedArtifactPathKey{workspaceID: workspaceID, path: authority.NormalizedWorkspacePath()}, operationID: id,
		authority: authority, slot: slot, persistCollision: true,
	}
	return c.registerClaim(ctx, claim, slot)
}

func (c *CaptureCoordinator) registerClaim(ctx context.Context, claim *ManagedArtifactPathClaim, slot *ArtifactPathAuthoritySlot) (*ManagedArtifactPathClaim, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if c == nil || c.registry == nil || c.store == nil || claim == nil || claim.key.workspaceID == "" || claim.key.path == "" {
		return nil, false, fmt.Errorf("invalid managed artifact path claim")
	}
	if _, err := operation.ParseID(string(claim.operationID)); err != nil {
		return nil, false, err
	}

	var collidedClaims []*ManagedArtifactPathClaim
	var closeAuthorities []ArtifactPathAuthority
	c.registry.mu.Lock()
	if slot != nil {
		if slot.released || slot.transferred || slot.registry != c.registry {
			c.registry.mu.Unlock()
			return nil, false, fmt.Errorf("artifact path authority slot unavailable")
		}
	}
	claims := c.registry.claims[claim.key]
	if claims == nil {
		claims = map[operation.ID]*ManagedArtifactPathClaim{}
		c.registry.claims[claim.key] = claims
	}
	if _, exists := claims[claim.operationID]; exists {
		c.registry.mu.Unlock()
		return nil, false, fmt.Errorf("managed artifact path claim already active")
	}
	if slot != nil {
		slot.transferred = true
	}
	claims[claim.operationID] = claim
	if len(claims) > 1 {
		collidedClaims = make([]*ManagedArtifactPathClaim, 0, len(claims))
		for _, current := range claims {
			current.mu.Lock()
			current.collided = true
			if current.authority != nil {
				closeAuthorities = append(closeAuthorities, current.authority)
				current.authority = nil
			}
			if current.slot != nil {
				c.registry.releaseSlotLocked(current.slot)
				current.slot = nil
			}
			current.mu.Unlock()
			collidedClaims = append(collidedClaims, current)
		}
	}
	c.registry.mu.Unlock()
	for _, current := range closeAuthorities {
		_ = current.Close()
	}
	if len(collidedClaims) == 0 {
		return claim, false, nil
	}

	sort.Slice(collidedClaims, func(i, j int) bool { return collidedClaims[i].operationID < collidedClaims[j].operationID })
	var markErr error
	seen := map[operation.ID]struct{}{}
	for _, current := range collidedClaims {
		if _, duplicate := seen[current.operationID]; duplicate || !current.persistCollision {
			continue
		}
		seen[current.operationID] = struct{}{}
		if _, err := c.store.MarkCaptureAuthorityState(ctx, current.operationID, CaptureAuthorityManagedPathCollision); err != nil {
			markErr = errors.Join(markErr, err)
		}
	}
	return claim, true, markErr
}

func (c *ManagedArtifactPathClaim) AllowsMechanicalCapture() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.collided && !c.released && c.authority != nil
}

func (c *ManagedArtifactPathClaim) Release() error {
	if c == nil || c.registry == nil {
		return nil
	}
	registry := c.registry
	var authority ArtifactPathAuthority
	registry.mu.Lock()
	c.mu.Lock()
	if c.released {
		c.mu.Unlock()
		registry.mu.Unlock()
		return nil
	}
	c.released = true
	if claims := registry.claims[c.key]; claims != nil {
		delete(claims, c.operationID)
		if len(claims) == 0 {
			delete(registry.claims, c.key)
		}
	}
	authority = c.authority
	c.authority = nil
	if c.slot != nil {
		registry.releaseSlotLocked(c.slot)
		c.slot = nil
	}
	c.mu.Unlock()
	registry.mu.Unlock()
	if authority != nil {
		return authority.Close()
	}
	return nil
}

var ErrCaptureAuthorityNotFound = errors.New("capture_authority_not_found")

type CaptureAuthorityRepository interface {
	CaptureAuthorityStateStore
	ReserveCaptureAuthority(context.Context, CaptureAuthority) (CaptureAuthorityRecord, bool, error)
	FindCaptureAuthority(context.Context, operation.ID) (CaptureAuthorityRecord, error)
}

type ArtifactBaselineQualifier interface {
	QualifyAbsent(context.Context, string, string) (ArtifactPathAuthority, CaptureBaselineIdentity, error)
}

type CaptureOperationReserver interface {
	ReserveCaptureOperation(context.Context, operation.ID, string) error
}

type CaptureSpawner interface {
	SpawnCaptureOperation(context.Context, operation.ID) error
}

type PreSpawnCaptureRequest struct {
	OperationID   operation.ID
	SessionID     operation.SessionID
	RepositoryID  string
	WorkspaceID   string
	WorkspaceRoot string
	MaxBlobBytes  int64
	Invocation    PytestInvocationRequest
}

type PreSpawnCaptureResult struct {
	Record                  *CaptureAuthorityRecord
	Claim                   *ManagedArtifactPathClaim
	StructuredCaptureDigest string
	Qualified               bool
	Replayed                bool
	Collision               bool
	CaptureUnavailable      error
}

type CapturePreparer struct {
	store       CaptureAuthorityRepository
	baseline    ArtifactBaselineQualifier
	presence    EnvironmentPresenceObserver
	reserver    CaptureOperationReserver
	spawner     CaptureSpawner
	coordinator *CaptureCoordinator
}

func NewCapturePreparer(store CaptureAuthorityRepository, baseline ArtifactBaselineQualifier, presence EnvironmentPresenceObserver, reserver CaptureOperationReserver, spawner CaptureSpawner) *CapturePreparer {
	return newCapturePreparerWithRegistry(store, baseline, presence, reserver, spawner, globalArtifactPathRegistry)
}

func newCapturePreparerWithRegistry(store CaptureAuthorityRepository, baseline ArtifactBaselineQualifier, presence EnvironmentPresenceObserver, reserver CaptureOperationReserver, spawner CaptureSpawner, registry *artifactPathRegistry) *CapturePreparer {
	return &CapturePreparer{
		store: store, baseline: baseline, presence: presence, reserver: reserver, spawner: spawner,
		coordinator: newCaptureCoordinatorWithRegistry(store, registry),
	}
}

func (p *CapturePreparer) PrepareAndSpawn(ctx context.Context, req PreSpawnCaptureRequest) (PreSpawnCaptureResult, error) {
	if err := p.validatePreSpawnRequest(req); err != nil {
		return PreSpawnCaptureResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return PreSpawnCaptureResult{}, err
	}

	stored, err := p.store.FindCaptureAuthority(ctx, req.OperationID)
	if err == nil {
		return p.prepareReplayAndSpawn(ctx, req, stored)
	}
	if !errors.Is(err, ErrCaptureAuthorityNotFound) {
		return PreSpawnCaptureResult{}, err
	}

	binding, qualified, err := QualifyPytestInvocation(ctx, req.Invocation, p.presence)
	if err != nil {
		return PreSpawnCaptureResult{}, err
	}
	if !qualified {
		return p.spawnWithoutCapture(ctx, req, fmt.Errorf("pytest invocation unqualified"))
	}

	record, slot, authority, err := p.prepareNewCaptureAuthority(ctx, req, binding)
	if err != nil {
		return p.spawnWithoutCapture(ctx, req, err)
	}
	result := PreSpawnCaptureResult{Record: &record, StructuredCaptureDigest: record.StructuredCaptureDigest}
	if err := p.reserver.ReserveCaptureOperation(ctx, req.OperationID, record.StructuredCaptureDigest); err != nil {
		_ = authority.Close()
		_ = slot.Release()
		abandonErr := p.abandonPrepared(ctx, req.OperationID)
		return result, errors.Join(err, abandonErr)
	}

	claim, collided, claimErr := p.coordinator.RegisterManagedPathClaim(ctx, slot, req.OperationID, req.WorkspaceID, authority)
	if claim == nil {
		_ = authority.Close()
		_ = slot.Release()
		abandonErr := p.abandonPrepared(ctx, req.OperationID)
		return result, errors.Join(claimErr, abandonErr)
	}
	result.Claim = claim
	result.Collision = collided
	result.Qualified = claim.AllowsMechanicalCapture()
	if claimErr != nil {
		result.CaptureUnavailable = claimErr
	}
	if latest, findErr := p.store.FindCaptureAuthority(ctx, req.OperationID); findErr == nil {
		result.Record = &latest
	}
	if err := p.spawner.SpawnCaptureOperation(ctx, req.OperationID); err != nil {
		_ = claim.Release()
		result.Claim = nil
		abandonErr := p.abandonPrepared(ctx, req.OperationID)
		return result, errors.Join(err, abandonErr)
	}
	return result, nil
}

func (p *CapturePreparer) prepareNewCaptureAuthority(ctx context.Context, req PreSpawnCaptureRequest, binding PytestInvocationBindingV1) (CaptureAuthorityRecord, *ArtifactPathAuthoritySlot, ArtifactPathAuthority, error) {
	slot, err := p.coordinator.AcquirePathAuthoritySlot(ctx)
	if err != nil {
		return CaptureAuthorityRecord{}, nil, nil, err
	}
	authority, baseline, err := p.baseline.QualifyAbsent(ctx, req.WorkspaceRoot, binding.JUnitOutput.NormalizedWorkspacePath)
	if err != nil {
		_ = slot.Release()
		return CaptureAuthorityRecord{}, nil, nil, err
	}
	cleanup := func(err error) (CaptureAuthorityRecord, *ArtifactPathAuthoritySlot, ArtifactPathAuthority, error) {
		if authority != nil {
			_ = authority.Close()
		}
		_ = slot.Release()
		return CaptureAuthorityRecord{}, nil, nil, err
	}
	if authority == nil || authority.NormalizedWorkspacePath() != binding.JUnitOutput.NormalizedWorkspacePath || authority.BaselineDigest() != baseline.AuthorityDigest {
		return cleanup(fmt.Errorf("artifact baseline authority mismatch"))
	}
	captureAuthority, err := buildCaptureAuthority(req, binding, baseline)
	if err != nil {
		return cleanup(err)
	}
	record, _, err := p.store.ReserveCaptureAuthority(ctx, captureAuthority)
	if err != nil {
		return cleanup(err)
	}
	return record, slot, authority, nil
}

func (p *CapturePreparer) prepareReplayAndSpawn(ctx context.Context, req PreSpawnCaptureRequest, record CaptureAuthorityRecord) (PreSpawnCaptureResult, error) {
	if err := record.Validate(); err != nil {
		return PreSpawnCaptureResult{}, err
	}
	intent := record.Authority.Intent
	if intent.OperationID != string(req.OperationID) || intent.SessionID != string(req.SessionID) || intent.RepositoryID != req.RepositoryID || intent.WorkspaceID != req.WorkspaceID || intent.MaxBlobBytes != req.MaxBlobBytes {
		return PreSpawnCaptureResult{}, fmt.Errorf("capture replay metadata conflict")
	}
	result := PreSpawnCaptureResult{Record: &record, StructuredCaptureDigest: record.StructuredCaptureDigest, Replayed: true}
	if err := p.reserver.ReserveCaptureOperation(ctx, req.OperationID, record.StructuredCaptureDigest); err != nil {
		abandonErr := p.abandonPrepared(ctx, req.OperationID)
		return result, errors.Join(err, abandonErr)
	}
	claim, collided, claimErr := p.coordinator.RegisterReplayManagedPathClaim(ctx, req.OperationID, record.Authority.Intent.WorkspaceID, record.Authority.Intent.NormalizedWorkspacePath, record.State)
	if claim == nil {
		abandonErr := p.abandonPrepared(ctx, req.OperationID)
		return result, errors.Join(claimErr, abandonErr)
	}
	result.Claim = claim
	result.Collision = collided
	if claimErr != nil {
		result.CaptureUnavailable = claimErr
	}
	if err := p.spawner.SpawnCaptureOperation(ctx, req.OperationID); err != nil {
		_ = claim.Release()
		result.Claim = nil
		abandonErr := p.abandonPrepared(ctx, req.OperationID)
		return result, errors.Join(err, abandonErr)
	}
	return result, nil
}

func (p *CapturePreparer) spawnWithoutCapture(ctx context.Context, req PreSpawnCaptureRequest, reason error) (PreSpawnCaptureResult, error) {
	result := PreSpawnCaptureResult{CaptureUnavailable: reason}
	if err := p.reserver.ReserveCaptureOperation(ctx, req.OperationID, ""); err != nil {
		return result, err
	}
	if err := p.spawner.SpawnCaptureOperation(ctx, req.OperationID); err != nil {
		return result, err
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

func (p *CapturePreparer) validatePreSpawnRequest(req PreSpawnCaptureRequest) error {
	if p == nil || p.store == nil || p.baseline == nil || p.presence == nil || p.reserver == nil || p.spawner == nil || p.coordinator == nil {
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
