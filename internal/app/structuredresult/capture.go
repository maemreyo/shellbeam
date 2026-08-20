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
	ArtifactSourceOpener
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

func (c *ManagedArtifactPathClaim) TakeArtifactSourceOpener() (ArtifactSourceOpener, error) {
	if c == nil || c.registry == nil {
		return nil, ErrArtifactCaptureUnavailable
	}
	registry := c.registry
	registry.mu.Lock()
	defer registry.mu.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.released || c.collided || c.authority == nil {
		return nil, ErrArtifactCaptureUnavailable
	}
	claims := registry.claims[c.key]
	if claims == nil || claims[c.operationID] != c {
		return nil, ErrArtifactCaptureUnavailable
	}
	delete(claims, c.operationID)
	if len(claims) == 0 {
		delete(registry.claims, c.key)
	}
	authority := c.authority
	c.authority = nil
	c.released = true
	if c.slot != nil {
		registry.releaseSlotLocked(c.slot)
		c.slot = nil
	}
	return authority, nil
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

type ProducerCaptureRequest interface {
	AdapterID() string
	Qualify(context.Context, EnvironmentPresenceObserver) (ProducerInvocationBinding, bool, error)
}

type PytestCaptureRequest struct {
	Invocation PytestInvocationRequest
}

func (r PytestCaptureRequest) AdapterID() string { return PytestJUnitAdapterID }

func (r PytestCaptureRequest) Qualify(ctx context.Context, observer EnvironmentPresenceObserver) (ProducerInvocationBinding, bool, error) {
	binding, qualified, err := QualifyPytestInvocation(ctx, r.Invocation, observer)
	if err != nil || !qualified {
		return ProducerInvocationBinding{}, qualified, err
	}
	return ProducerInvocationBinding{Kind: ProducerInvocationPytest, PytestInvocation: &binding}, true, nil
}

type PreSpawnCaptureRequest struct {
	OperationID   operation.ID
	SessionID     operation.SessionID
	RepositoryID  string
	WorkspaceID   string
	WorkspaceRoot string
	MaxBlobBytes  int64
	Producer      ProducerCaptureRequest
}

type PreSpawnCaptureResult struct {
	Record                  *CaptureAuthorityRecord
	Claim                   *ManagedArtifactPathClaim
	StructuredCaptureDigest string
	InvocationQualified     bool
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

func (p *CapturePreparer) Prepare(ctx context.Context, req PreSpawnCaptureRequest) (PreSpawnCaptureResult, error) {
	if err := p.validatePreparationRequest(req); err != nil {
		return PreSpawnCaptureResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return PreSpawnCaptureResult{}, err
	}

	stored, err := p.store.FindCaptureAuthority(ctx, req.OperationID)
	if err == nil {
		return p.prepareReplay(ctx, req, stored)
	}
	if !errors.Is(err, ErrCaptureAuthorityNotFound) {
		return PreSpawnCaptureResult{}, err
	}

	binding, qualified, err := req.Producer.Qualify(ctx, p.presence)
	if err != nil {
		return PreSpawnCaptureResult{}, err
	}
	if !qualified {
		return PreSpawnCaptureResult{CaptureUnavailable: fmt.Errorf("%s invocation unqualified", req.Producer.AdapterID())}, nil
	}
	if err := binding.Validate(); err != nil || binding.AdapterID() != req.Producer.AdapterID() {
		if err == nil {
			err = fmt.Errorf("producer capture binding adapter mismatch")
		}
		return PreSpawnCaptureResult{}, err
	}
	result := PreSpawnCaptureResult{InvocationQualified: true}
	record, slot, authority, err := p.prepareNewCaptureAuthority(ctx, req, binding)
	if err != nil {
		result.CaptureUnavailable = err
		return result, nil
	}
	result.Record = &record
	result.StructuredCaptureDigest = record.StructuredCaptureDigest
	claim, collided, claimErr := p.coordinator.RegisterManagedPathClaim(ctx, slot, req.OperationID, req.WorkspaceID, authority)
	if claim == nil {
		_ = authority.Close()
		_ = slot.Release()
		result.CaptureUnavailable = claimErr
		if claimErr != nil {
			_ = p.abandonPrepared(ctx, req.OperationID)
		}
		return result, nil
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
	return result, nil
}

func (p *CapturePreparer) PrepareAndSpawn(ctx context.Context, req PreSpawnCaptureRequest) (PreSpawnCaptureResult, error) {
	if p == nil || p.reserver == nil || p.spawner == nil {
		return PreSpawnCaptureResult{}, fmt.Errorf("capture spawn coordinator unavailable")
	}
	result, err := p.Prepare(ctx, req)
	if err != nil {
		return result, err
	}
	digest := result.StructuredCaptureDigest
	if err := p.reserver.ReserveCaptureOperation(ctx, req.OperationID, digest); err != nil {
		if result.Claim != nil {
			_ = result.Claim.Release()
			result.Claim = nil
		}
		if result.Record != nil {
			_ = p.abandonPrepared(ctx, req.OperationID)
		}
		return result, err
	}
	if err := p.spawner.SpawnCaptureOperation(ctx, req.OperationID); err != nil {
		if result.Claim != nil {
			_ = result.Claim.Release()
			result.Claim = nil
		}
		if result.Record != nil {
			_ = p.abandonPrepared(ctx, req.OperationID)
		}
		return result, err
	}
	return result, nil
}

func (p *CapturePreparer) prepareNewCaptureAuthority(ctx context.Context, req PreSpawnCaptureRequest, binding ProducerInvocationBinding) (CaptureAuthorityRecord, *ArtifactPathAuthoritySlot, ArtifactPathAuthority, error) {
	slot, err := p.coordinator.AcquirePathAuthoritySlot(ctx)
	if err != nil {
		return CaptureAuthorityRecord{}, nil, nil, err
	}
	output := binding.OutputBinding()
	authority, baseline, err := p.baseline.QualifyAbsent(ctx, req.WorkspaceRoot, output.NormalizedWorkspacePath)
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
	if authority == nil || authority.NormalizedWorkspacePath() != output.NormalizedWorkspacePath || authority.BaselineDigest() != baseline.AuthorityDigest {
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
