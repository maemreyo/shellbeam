package codeintel

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/codeintel"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestProviderManagerStartsLazilyAndReusesCompatibleWarmProvider(t *testing.T) {
	resolver := &managerOptionsResolver{options: managerOptions()}
	factory := &managerFactory{}
	manager := newProviderManagerForTest(t, factory, resolver, managerLimits())

	if factory.startCount() != 0 {
		t.Fatal("provider started before explicit query")
	}
	request := managerRequest(serviceTestWorkspace())
	if _, err := manager.Query(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Query(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if factory.startCount() != 1 || factory.providers[0].callCount() != 2 {
		t.Fatalf("starts=%d calls=%d", factory.startCount(), factory.providers[0].callCount())
	}
}

func TestProviderManagerCompatibilityChangesDiscardWarmProvider(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*ProviderStartOptions)
	}{
		{"executable", func(o *ProviderStartOptions) { o.ExecutableIdentity = "gopls-bin-v2" }},
		{"config", func(o *ProviderStartOptions) { o.ConfigFingerprint = "cfg-v2" }},
		{"build", func(o *ProviderStartOptions) { o.BuildFingerprint = "build-v2" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resolver := &managerOptionsResolver{options: managerOptions()}
			factory := &managerFactory{}
			manager := newProviderManagerForTest(t, factory, resolver, managerLimits())
			request := managerRequest(serviceTestWorkspace())
			if _, err := manager.Query(t.Context(), request); err != nil {
				t.Fatal(err)
			}
			tc.mutate(&resolver.options)
			if _, err := manager.Query(t.Context(), request); err != nil {
				t.Fatal(err)
			}
			if factory.startCount() != 2 || factory.providers[0].closeCount() != 1 {
				t.Fatalf("starts=%d first_closes=%d", factory.startCount(), factory.providers[0].closeCount())
			}
		})
	}
}

func TestProviderManagerProviderIncarnationChangeRestarts(t *testing.T) {
	resolver := &managerOptionsResolver{options: managerOptions()}
	factory := &managerFactory{}
	manager := newProviderManagerForTest(t, factory, resolver, managerLimits())
	request := managerRequest(serviceTestWorkspace())

	if _, err := manager.Query(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	factory.providers[0].meta.Incarnation = "provider_01K00000000000000000000002"
	if _, err := manager.Query(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if factory.startCount() != 2 || factory.providers[0].closeCount() != 1 {
		t.Fatalf("starts=%d first_closes=%d", factory.startCount(), factory.providers[0].closeCount())
	}
}

func TestProviderManagerCrashRestartsThenEntersCooldown(t *testing.T) {
	resolver := &managerOptionsResolver{options: managerOptions()}
	factory := &managerFactory{configure: func(p *managerProvider) {
		p.queryErr = errors.New("provider crashed")
	}}
	limits := managerLimits()
	limits.FailuresBeforeCooldown = 2
	manager := newProviderManagerForTest(t, factory, resolver, limits)
	request := managerRequest(serviceTestWorkspace())

	for i := 0; i < 2; i++ {
		if _, err := manager.Query(t.Context(), request); ErrorCode(err) != CodeProviderFailed || !Retryable(err) {
			t.Fatalf("failure %d err=%v code=%q", i+1, err, ErrorCode(err))
		}
	}
	if _, err := manager.Query(t.Context(), request); ErrorCode(err) != CodeProviderCooldown || !Retryable(err) {
		t.Fatalf("cooldown err=%v code=%q", err, ErrorCode(err))
	}
	if factory.startCount() != 2 {
		t.Fatalf("restart loop not bounded: starts=%d", factory.startCount())
	}
}

func TestProviderManagerCapacityNeverEvictsActiveRequestAndEvictsIdleOnPressure(t *testing.T) {
	resolver := &managerOptionsResolver{options: managerOptions()}
	block := make(chan struct{})
	entered := make(chan struct{}, 1)
	factory := &managerFactory{configure: func(p *managerProvider) {
		if len(p.factory.providers) == 0 {
			p.block = block
			p.entered = entered
		}
	}}
	limits := managerLimits()
	limits.MaxInstances = 1
	manager := newProviderManagerForTest(t, factory, resolver, limits)
	first := managerRequest(serviceTestWorkspace())
	secondWorkspace := serviceTestWorkspace()
	secondWorkspace.ID = workspacecore.WorkspaceID("ws_01K00000000000000000000001")
	secondWorkspace.Root = "/tmp/codeintel-2"
	secondWorkspace.GitDir = "/tmp/codeintel-2/.git"
	second := managerRequest(secondWorkspace)

	done := make(chan error, 1)
	go func() {
		_, err := manager.Query(context.Background(), first)
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first provider query did not start")
	}

	if _, err := manager.Query(t.Context(), second); ErrorCode(err) != CodeProviderBusy || !Retryable(err) {
		t.Fatalf("capacity err=%v code=%q", err, ErrorCode(err))
	}
	if factory.providers[0].closeCount() != 0 {
		t.Fatal("active provider was evicted")
	}
	close(block)
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	if _, err := manager.Query(t.Context(), second); err != nil {
		t.Fatal(err)
	}
	if factory.startCount() != 2 || factory.providers[0].closeCount() != 1 {
		t.Fatalf("starts=%d first_closes=%d", factory.startCount(), factory.providers[0].closeCount())
	}
}

func TestProviderManagerInFlightCapacityReturnsBusyWithoutBacklog(t *testing.T) {
	resolver := &managerOptionsResolver{options: managerOptions()}
	block := make(chan struct{})
	entered := make(chan struct{}, 1)
	factory := &managerFactory{configure: func(p *managerProvider) {
		p.block = block
		p.entered = entered
	}}
	limits := managerLimits()
	limits.MaxInFlight = 1
	limits.MaxInFlightPerProvider = 1
	manager := newProviderManagerForTest(t, factory, resolver, limits)
	request := managerRequest(serviceTestWorkspace())

	done := make(chan error, 1)
	go func() {
		_, err := manager.Query(context.Background(), request)
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first query did not enter provider")
	}
	if _, err := manager.Query(t.Context(), request); ErrorCode(err) != CodeProviderBusy {
		t.Fatalf("expected provider busy, got %v (%q)", err, ErrorCode(err))
	}
	close(block)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestProviderManagerBoundedQueueTimesOut(t *testing.T) {
	resolver := &managerOptionsResolver{options: managerOptions()}
	block := make(chan struct{})
	entered := make(chan struct{}, 1)
	factory := &managerFactory{configure: func(p *managerProvider) {
		p.block = block
		p.entered = entered
	}}
	limits := managerLimits()
	limits.MaxInFlight = 1
	limits.MaxInFlightPerProvider = 1
	limits.MaxQueueDepth = 1
	limits.QueueWait = 10 * time.Millisecond
	manager := newProviderManagerForTest(t, factory, resolver, limits)
	request := managerRequest(serviceTestWorkspace())

	done := make(chan error, 1)
	go func() {
		_, err := manager.Query(context.Background(), request)
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first query did not enter provider")
	}
	start := time.Now()
	if _, err := manager.Query(t.Context(), request); ErrorCode(err) != CodeProviderBusy {
		t.Fatalf("expected bounded queue busy, got %v (%q)", err, ErrorCode(err))
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("bounded queue waited too long: %v", elapsed)
	}
	close(block)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestProviderManagerRejectsInvalidProviderMetadata(t *testing.T) {
	resolver := &managerOptionsResolver{options: managerOptions()}
	factory := &managerFactory{configure: func(p *managerProvider) {
		p.meta.Coverage = core.SyncCoverage("per_path_sync_map")
	}}
	manager := newProviderManagerForTest(t, factory, resolver, managerLimits())

	if _, err := manager.Query(t.Context(), managerRequest(serviceTestWorkspace())); ErrorCode(err) != CodeProviderFailed {
		t.Fatalf("invalid metadata err=%v code=%q", err, ErrorCode(err))
	}
	if factory.providers[0].closeCount() != 1 {
		t.Fatal("invalid provider was not discarded")
	}
}

func TestProviderManagerCloseReapsProvidersAndStopsAdmission(t *testing.T) {
	resolver := &managerOptionsResolver{options: managerOptions()}
	factory := &managerFactory{}
	manager := newProviderManagerForTest(t, factory, resolver, managerLimits())
	request := managerRequest(serviceTestWorkspace())
	if _, err := manager.Query(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	if factory.providers[0].closeCount() != 1 {
		t.Fatalf("close count=%d", factory.providers[0].closeCount())
	}
	if _, err := manager.Query(t.Context(), request); ErrorCode(err) != CodeProviderUnavailable {
		t.Fatalf("query after close err=%v code=%q", err, ErrorCode(err))
	}
}

func newProviderManagerForTest(t *testing.T, factory *managerFactory, resolver *managerOptionsResolver, limits ProviderManagerLimits) *ProviderManager {
	t.Helper()
	manager, err := NewProviderManager(factory, resolver, limits)
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func managerLimits() ProviderManagerLimits {
	return ProviderManagerLimits{
		MaxInstances:           2,
		MaxInFlight:            4,
		MaxInFlightPerProvider: 2,
		MaxQueueDepth:          0,
		QueueWait:              0,
		IdleTTL:                time.Minute,
		FailuresBeforeCooldown: 3,
		FailureWindow:          time.Minute,
		Cooldown:               time.Minute,
	}
}

func managerOptions() ProviderStartOptions {
	return ProviderStartOptions{
		ProviderID:         "gopls",
		ExecutableIdentity: "gopls-bin-v1",
		ConfigFingerprint:  "cfg-v1",
		BuildFingerprint:   "build-v1",
	}
}

func managerRequest(workspace workspacecore.Workspace) ProviderRequest {
	return ProviderRequest{
		Workspace: workspace,
		Query: core.Query{
			Kind: core.QueryDiagnostics, Scope: core.ScopeWorkspace, Provider: "gopls",
		},
	}
}

type managerOptionsResolver struct {
	options ProviderStartOptions
	err     error
}

func (r *managerOptionsResolver) Resolve(_ context.Context, _ workspacecore.Workspace, _ core.Query) (ProviderStartOptions, error) {
	return r.options, r.err
}

type managerFactory struct {
	mu        sync.Mutex
	providers []*managerProvider
	configure func(*managerProvider)
}

func (f *managerFactory) Start(_ context.Context, _ workspacecore.Workspace, options ProviderStartOptions) (Provider, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	index := len(f.providers) + 1
	p := &managerProvider{
		factory: f,
		meta: core.ProviderMetadata{
			ProviderID:        options.ProviderID,
			Incarnation:       "provider_01K0000000000000000000000" + string(rune('0'+index)),
			ExecutableVersion: options.ExecutableIdentity,
			ConfigFingerprint: options.ConfigFingerprint,
			BuildFingerprint:  options.BuildFingerprint,
			BuildQuality:      "observed",
			Coverage:          core.SyncExactForKnownPaths,
		},
	}
	if f.configure != nil {
		f.configure(p)
	}
	f.providers = append(f.providers, p)
	return p, nil
}

func (f *managerFactory) startCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.providers)
}

type managerProvider struct {
	factory  *managerFactory
	meta     core.ProviderMetadata
	queryErr error
	block    <-chan struct{}
	entered  chan<- struct{}
	calls    atomic.Int32
	closes   atomic.Int32
}

func (p *managerProvider) Metadata() core.ProviderMetadata { return p.meta }

func (p *managerProvider) Query(ctx context.Context, _ ProviderRequest) (ProviderResponse, error) {
	p.calls.Add(1)
	if p.entered != nil {
		select {
		case p.entered <- struct{}{}:
		default:
		}
	}
	if p.block != nil {
		select {
		case <-p.block:
		case <-ctx.Done():
			return ProviderResponse{}, ctx.Err()
		}
	}
	if p.queryErr != nil {
		return ProviderResponse{}, p.queryErr
	}
	return ProviderResponse{Status: core.StatusReady, Metadata: p.meta}, nil
}

func (p *managerProvider) Close() error {
	p.closes.Add(1)
	return nil
}

func (p *managerProvider) callCount() int  { return int(p.calls.Load()) }
func (p *managerProvider) closeCount() int { return int(p.closes.Load()) }

func TestProviderManagerPreservesNonRetryableQueryContractErrorsWithoutRestart(t *testing.T) {
	resolver := &managerOptionsResolver{options: managerOptions()}
	factory := &managerFactory{configure: func(p *managerProvider) {
		p.queryErr = &Error{Code: CodeQueryUnsupported, Retryable: false, Cause: errors.New("unsupported capability")}
	}}
	manager := newProviderManagerForTest(t, factory, resolver, managerLimits())
	request := managerRequest(serviceTestWorkspace())
	if _, err := manager.Query(t.Context(), request); ErrorCode(err) != CodeQueryUnsupported || Retryable(err) {
		t.Fatalf("first err=%v code=%q retryable=%v", err, ErrorCode(err), Retryable(err))
	}
	if _, err := manager.Query(t.Context(), request); ErrorCode(err) != CodeQueryUnsupported || Retryable(err) {
		t.Fatalf("second err=%v code=%q retryable=%v", err, ErrorCode(err), Retryable(err))
	}
	if factory.startCount() != 1 || factory.providers[0].closeCount() != 0 {
		t.Fatalf("contract error restarted provider: starts=%d closes=%d", factory.startCount(), factory.providers[0].closeCount())
	}
}
