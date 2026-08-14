package codeintel

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/codeintel"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type ProviderManagerLimits struct {
	MaxInstances           int
	MaxInFlight            int
	MaxInFlightPerProvider int
	MaxQueueDepth          int
	QueueWait              time.Duration
	IdleTTL                time.Duration
	FailuresBeforeCooldown int
	FailureWindow          time.Duration
	Cooldown               time.Duration
}

type ProviderManager struct {
	mu        sync.Mutex
	factory   ProviderFactory
	resolver  ProviderOptionsResolver
	limits    ProviderManagerLimits
	instances map[providerKey]*managedProvider
	health    map[providerKey]*providerHealth
	inFlight  int
	queued    int
	closed    bool
	notify    chan struct{}
}

type providerKey struct {
	workspaceID        workspacecore.WorkspaceID
	providerID         string
	executableIdentity string
	configFingerprint  string
	buildFingerprint   string
}

type managedProvider struct {
	key      providerKey
	provider Provider
	metadata core.ProviderMetadata
	inFlight int
	lastUsed time.Time
	starting bool
}

type providerHealth struct {
	failures      []time.Time
	cooldownUntil time.Time
}

func NewProviderManager(factory ProviderFactory, resolver ProviderOptionsResolver, limits ProviderManagerLimits) (*ProviderManager, error) {
	if factory == nil || resolver == nil || limits.Validate() != nil {
		return nil, fmt.Errorf("invalid provider manager config")
	}
	return &ProviderManager{
		factory: factory, resolver: resolver, limits: limits,
		instances: make(map[providerKey]*managedProvider),
		health:    make(map[providerKey]*providerHealth),
		notify:    make(chan struct{}),
	}, nil
}

func (m *ProviderManager) Query(ctx context.Context, request ProviderRequest) (ProviderResponse, error) {
	if err := m.ensureOpen(); err != nil {
		return ProviderResponse{}, err
	}
	options, err := m.resolver.Resolve(ctx, request.Workspace, request.Query)
	if err != nil {
		return ProviderResponse{}, newError(CodeProviderUnavailable, true, err)
	}
	if err := validateProviderOptions(options, request.Query); err != nil {
		return ProviderResponse{}, newError(CodeProviderUnavailable, false, err)
	}
	instance, err := m.acquire(ctx, request.Workspace, options)
	if err != nil {
		return ProviderResponse{}, err
	}
	if err := m.startIfNeeded(ctx, request.Workspace, options, instance); err != nil {
		return ProviderResponse{}, err
	}
	response, queryErr := instance.provider.Query(ctx, request)
	if queryErr != nil {
		return ProviderResponse{}, m.releaseError(instance, queryErr, ctx.Err())
	}
	if err := normalizeProviderResponse(&response, instance.metadata); err != nil {
		return ProviderResponse{}, m.releaseError(instance, err, nil)
	}
	m.releaseSuccess(instance)
	return response, nil
}

func (m *ProviderManager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	providers := make([]Provider, 0, len(m.instances))
	for key, instance := range m.instances {
		if instance.provider != nil {
			providers = append(providers, instance.provider)
		}
		delete(m.instances, key)
	}
	m.signalLocked()
	m.mu.Unlock()

	var errs []error
	for _, provider := range providers {
		if err := provider.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (m *ProviderManager) ensureOpen() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return newError(CodeProviderUnavailable, false, fmt.Errorf("provider manager closed"))
	}
	return nil
}

func (m *ProviderManager) acquire(ctx context.Context, workspace workspacecore.Workspace, options ProviderStartOptions) (*managedProvider, error) {
	key := providerCompatibilityKey(workspace, options)
	deadline := time.Time{}
	if m.limits.MaxQueueDepth > 0 {
		deadline = time.Now().Add(m.limits.QueueWait)
	}
	for {
		instance, evicted, wait, err := m.tryAcquire(key)
		closeProviders(evicted)
		if err != nil || instance != nil {
			return instance, err
		}
		if !wait || deadline.IsZero() {
			return nil, providerBusyError()
		}
		if err := m.waitForCapacity(ctx, deadline); err != nil {
			return nil, err
		}
	}
}

func (m *ProviderManager) tryAcquire(key providerKey) (*managedProvider, []Provider, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, nil, false, newError(CodeProviderUnavailable, false, fmt.Errorf("provider manager closed"))
	}
	now := time.Now()
	evicted := m.collectExpiredIdleLocked(now)
	if m.cooldownActiveLocked(key, now) {
		return nil, evicted, false, newError(CodeProviderCooldown, true, fmt.Errorf("provider restart cooldown"))
	}
	if m.inFlight >= m.limits.MaxInFlight {
		return nil, evicted, m.queueAvailableLocked(), nil
	}
	if instance := m.instances[key]; instance != nil {
		return m.acquireExistingLocked(instance, evicted, now)
	}
	evicted = append(evicted, m.evictIncompatibleIdleLocked(key)...)
	if len(m.instances) >= m.limits.MaxInstances {
		if provider := m.evictOldestIdleLocked(); provider != nil {
			evicted = append(evicted, provider)
		} else {
			return nil, evicted, m.queueAvailableLocked(), nil
		}
	}
	instance := &managedProvider{key: key, inFlight: 1, lastUsed: now, starting: true}
	m.instances[key] = instance
	m.inFlight++
	return instance, evicted, false, nil
}

func (m *ProviderManager) acquireExistingLocked(instance *managedProvider, evicted []Provider, now time.Time) (*managedProvider, []Provider, bool, error) {
	if instance.starting {
		return nil, evicted, m.queueAvailableLocked(), nil
	}
	current := instance.provider.Metadata()
	if err := current.Validate(); err != nil || !sameProviderIdentity(instance.metadata, current) {
		if instance.inFlight != 0 {
			return nil, evicted, m.queueAvailableLocked(), nil
		}
		delete(m.instances, instance.key)
		evicted = append(evicted, instance.provider)
		replacement := &managedProvider{key: instance.key, inFlight: 1, lastUsed: now, starting: true}
		m.instances[instance.key] = replacement
		m.inFlight++
		return replacement, evicted, false, nil
	}
	if instance.inFlight >= m.limits.MaxInFlightPerProvider {
		return nil, evicted, m.queueAvailableLocked(), nil
	}
	instance.inFlight++
	instance.lastUsed = now
	m.inFlight++
	return instance, evicted, false, nil
}

func (m *ProviderManager) waitForCapacity(ctx context.Context, deadline time.Time) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return newError(CodeProviderUnavailable, false, fmt.Errorf("provider manager closed"))
	}
	if m.queued >= m.limits.MaxQueueDepth {
		m.mu.Unlock()
		return providerBusyError()
	}
	m.queued++
	notify := m.notify
	m.mu.Unlock()

	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	var err error
	select {
	case <-ctx.Done():
		err = ctx.Err()
	case <-timer.C:
		err = providerBusyError()
	case <-notify:
	}
	m.mu.Lock()
	m.queued--
	m.mu.Unlock()
	return err
}

func (m *ProviderManager) releaseSuccess(instance *managedProvider) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.releaseSlotLocked(instance)
	instance.lastUsed = time.Now()
	m.signalLocked()
}

func (m *ProviderManager) releaseError(instance *managedProvider, queryErr, contextErr error) error {
	m.mu.Lock()
	m.releaseSlotLocked(instance)
	if contextErr != nil || errors.Is(queryErr, context.Canceled) || errors.Is(queryErr, context.DeadlineExceeded) {
		instance.lastUsed = time.Now()
		m.signalLocked()
		m.mu.Unlock()
		return queryErr
	}
	if isQueryContractError(queryErr) {
		instance.lastUsed = time.Now()
		m.signalLocked()
		m.mu.Unlock()
		return queryErr
	}
	if current := m.instances[instance.key]; current == instance {
		delete(m.instances, instance.key)
	}
	m.recordFailureLocked(instance.key, time.Now())
	m.signalLocked()
	provider := instance.provider
	m.mu.Unlock()
	if provider != nil {
		_ = provider.Close()
	}
	return newError(CodeProviderFailed, true, queryErr)
}

func isQueryContractError(err error) bool {
	var contract *Error
	if !errors.As(err, &contract) || contract.Retryable {
		return false
	}
	return contract.Code == CodeQueryUnsupported || contract.Code == CodeLocationNotResolved
}

func (m *ProviderManager) releaseSlotLocked(instance *managedProvider) {
	if instance.inFlight > 0 {
		instance.inFlight--
	}
	if m.inFlight > 0 {
		m.inFlight--
	}
}

func (m *ProviderManager) startReserved(ctx context.Context, workspace workspacecore.Workspace, options ProviderStartOptions, instance *managedProvider) error {
	provider, err := m.factory.Start(ctx, workspace, options)
	if err != nil {
		m.failStart(instance, nil, err)
		return newError(CodeProviderFailed, true, err)
	}
	metadata := provider.Metadata()
	if err := validateStartedProvider(metadata, options); err != nil {
		m.failStart(instance, provider, err)
		return newError(CodeProviderFailed, true, err)
	}

	m.mu.Lock()
	current := m.instances[instance.key]
	if m.closed || current != instance {
		m.releaseSlotLocked(instance)
		m.signalLocked()
		m.mu.Unlock()
		_ = provider.Close()
		return newError(CodeProviderUnavailable, false, fmt.Errorf("provider admission stopped"))
	}
	instance.provider = provider
	instance.metadata = metadata
	instance.starting = false
	m.signalLocked()
	m.mu.Unlock()
	return nil
}

func (m *ProviderManager) failStart(instance *managedProvider, provider Provider, cause error) {
	m.mu.Lock()
	if current := m.instances[instance.key]; current == instance {
		delete(m.instances, instance.key)
	}
	m.releaseSlotLocked(instance)
	m.recordFailureLocked(instance.key, time.Now())
	m.signalLocked()
	m.mu.Unlock()
	if provider != nil {
		_ = provider.Close()
	}
}

func (m *ProviderManager) queueAvailableLocked() bool {
	return m.limits.MaxQueueDepth > 0 && m.queued < m.limits.MaxQueueDepth
}

func (m *ProviderManager) signalLocked() {
	close(m.notify)
	m.notify = make(chan struct{})
}

func (m *ProviderManager) startIfNeeded(ctx context.Context, workspace workspacecore.Workspace, options ProviderStartOptions, instance *managedProvider) error {
	if !instance.starting {
		return nil
	}
	return m.startReserved(ctx, workspace, options, instance)
}
