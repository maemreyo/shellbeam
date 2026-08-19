package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"runtime"

	delegatedtmux "github.com/maemreyo/shellbeam/internal/adapter/delegatedtmux"
	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
)

type delegatedProviderFactory func(stateDir, runtimeDir string) (daemonapp.DelegatedRuntime, error)

func composeDelegatedInteractiveRuntime(ctx context.Context, stateDir, runtimeDir string, catalog capability.Catalog) (daemonapp.DelegatedRuntime, capability.Catalog) {
	return composeDelegatedInteractive(ctx, runtime.GOOS, stateDir, runtimeDir, catalog, newQualifiedDelegatedProvider)
}

func composeDelegatedInteractive(ctx context.Context, goos, stateDir, runtimeDir string, catalog capability.Catalog, factory delegatedProviderFactory) (daemonapp.DelegatedRuntime, capability.Catalog) {
	if goos != "darwin" || factory == nil {
		return nil, catalog
	}
	provider, err := factory(stateDir, runtimeDir)
	if err != nil || provider == nil {
		return nil, catalog
	}
	if err := provider.Probe(ctx); err != nil {
		return nil, catalog
	}
	identity := provider.Identity()
	if err := identity.Validate(); err != nil {
		return nil, catalog
	}
	support := capability.DelegatedInteractiveSupport{
		ProviderID: identity.ID, ProviderVersion: identity.Version, Platform: goos,
		MaxMutationRecords:      storeadapter.DefaultMaxDelegatedMutationRecords,
		DaemonRestartContinuity: true, HostRebootContinuity: false,
	}
	return provider, catalog.WithDelegatedInteractive(support)
}

func newQualifiedDelegatedProvider(stateDir, runtimeDir string) (daemonapp.DelegatedRuntime, error) {
	tmuxPath, err := exec.LookPath("tmux")
	if err != nil {
		return nil, err
	}
	tmuxPath, err = filepath.Abs(tmuxPath)
	if err != nil {
		return nil, err
	}
	cfg := delegatedtmux.DarwinQualifiedConfig(filepath.Join(stateDir, "delegated-tmux"), tmuxPath)
	cfg.RuntimeBase = runtimeDir
	return delegatedtmux.New(cfg)
}

type delegatedStartupStore interface {
	ListDelegatedRecoveryCandidates(context.Context) ([]delegated.Binding, error)
}

func reconcileDelegatedDaemonStartup(ctx context.Context, store delegatedStartupStore, svc *daemonapp.Service) error {
	candidates, err := store.ListDelegatedRecoveryCandidates(ctx)
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		return nil
	}
	catalog := svc.CapabilityCatalog()
	if catalog.Features[capability.FeatureDelegatedInteractive] != capability.Available || catalog.DelegatedInteractive == nil {
		// A currently unavailable optional provider is not proof that an older
		// private provider session is gone. Preserve recovery markers and let the
		// ordinary daemon become ready without advertising delegated support.
		return nil
	}
	return svc.ReconcileDelegatedStartup(ctx, candidates, daemonapp.DelegatedStartupOptions{})
}
