package main

import (
	"context"
	"errors"
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	handoffapp "github.com/maemreyo/shellbeam/internal/app/interactivehandoff"
	terminalapp "github.com/maemreyo/shellbeam/internal/app/terminalpresentation"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
	terminalpresentation "github.com/maemreyo/shellbeam/internal/core/terminalpresentation"
	"sort"
	"time"
)

type handoffStartupStore interface {
	ListHandoffRecoveryCandidates(context.Context) ([]handoff.State, error)
}

type handoffStartupReconciler interface {
	CapabilityCatalog() capability.Catalog
	ReconcileHandoffStartup(context.Context, []handoff.State, daemonapp.HandoffStartupOptions) error
}

func reconcileHandoffDaemonStartup(ctx context.Context, store handoffStartupStore, svc handoffStartupReconciler) error {
	candidates, err := store.ListHandoffRecoveryCandidates(ctx)
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		return nil
	}
	catalog := svc.CapabilityCatalog()
	if catalog.Features[capability.FeatureInteractiveHandoff] != capability.Available || catalog.InteractiveHandoff == nil {
		return nil
	}
	return svc.ReconcileHandoffStartup(ctx, candidates, daemonapp.HandoffStartupOptions{})
}

func composeInteractiveHandoffCapability(catalog capability.Catalog, runtime daemonapp.DelegatedRuntime) capability.Catalog {
	if runtime == nil || catalog.Features[capability.FeatureDelegatedInteractive] != capability.Available || catalog.DelegatedInteractive == nil {
		return catalog
	}
	if _, ok := runtime.(delegatedapp.HumanProvider); !ok {
		return catalog
	}
	return catalog.WithInteractiveHandoff(capability.InteractiveHandoffSupport{ManualStandard: true})
}

const (
	terminalActiveFreshness  = 5 * time.Second
	terminalRecentFreshness  = 2 * time.Minute
	terminalRunningFreshness = 5 * time.Second
)

type terminalPresentationRuntimeInput struct {
	Catalog    capability.Catalog
	Providers  []terminalpresentation.TerminalIdentity
	Store      terminalapp.TerminalLaunchStore
	Activity   terminalapp.ActivitySource
	Running    terminalapp.RunningSource
	Launcher   terminalapp.LaunchExecutor
	Executable string
	Now        func() time.Time
}

type terminalPresentationRuntime struct {
	Catalog          capability.Catalog
	PresenterFactory func(handoffapp.ExactClientProver) handoffapp.Presenter
	Start            func(context.Context) error
}

func buildTerminalPresentationRuntime(input terminalPresentationRuntimeInput) (terminalPresentationRuntime, error) {
	degraded := terminalPresentationRuntime{Catalog: input.Catalog}
	if input.Catalog.Features[capability.FeatureInteractiveHandoff] != capability.Available || input.Catalog.InteractiveHandoff == nil || len(input.Providers) == 0 {
		return degraded, nil
	}
	if input.Store == nil || input.Activity == nil || input.Running == nil || input.Launcher == nil || input.Now == nil {
		return degraded, errors.New("incomplete terminal presentation runtime")
	}
	if _, err := terminalapp.BuildAttachArgv(input.Executable, "handoff-terminal-runtime-probe"); err != nil {
		return degraded, err
	}
	registry, err := terminalapp.NewRecentRegistry(terminalActiveFreshness, terminalRecentFreshness)
	if err != nil {
		return degraded, err
	}
	resolver, err := terminalapp.NewResolver(registry, input.Activity, input.Running, terminalRunningFreshness, input.Now)
	if err != nil {
		return degraded, err
	}
	catalog := composeTerminalPresentationCapability(input.Catalog, input.Providers)
	if catalog.InteractiveHandoff == nil || catalog.InteractiveHandoff.TerminalPresentation == nil {
		return degraded, nil
	}
	runtime := terminalPresentationRuntime{Catalog: catalog}
	runtime.PresenterFactory = func(prover handoffapp.ExactClientProver) handoffapp.Presenter {
		if prover == nil {
			return nil
		}
		launch := terminalapp.NewLaunchService(input.Store, input.Launcher, prover)
		return terminalapp.NewPresenter(resolver, launch, input.Executable, nil)
	}
	runtime.Start = func(ctx context.Context) error {
		return input.Activity.Run(ctx, registry.Observe)
	}
	return runtime, nil
}

func composeTerminalPresentationCapability(catalog capability.Catalog, providers []terminalpresentation.TerminalIdentity) capability.Catalog {
	if catalog.Features[capability.FeatureInteractiveHandoff] != capability.Available || catalog.InteractiveHandoff == nil || len(providers) == 0 {
		return catalog
	}
	providerIDs := make([]string, 0, len(providers))
	seen := make(map[string]struct{}, len(providers))
	for _, identity := range providers {
		if err := identity.Validate(); err != nil || identity.Platform != terminalpresentation.PlatformDarwin {
			return catalog
		}
		if _, ok := seen[identity.ProviderID]; ok {
			return catalog
		}
		seen[identity.ProviderID] = struct{}{}
		providerIDs = append(providerIDs, identity.ProviderID)
	}
	sort.Strings(providerIDs)
	return catalog.WithTerminalPresentation(capability.TerminalPresentationSupport{
		ResolutionSources:  []string{"active", "recent", "bridge_affinity", "single_running"},
		QualifiedLaunchers: providerIDs,
	})
}

func (a *daemonActions) RequestHandoffPublic(ctx context.Context, req handoff.Request) (handoff.PublicState, error) {
	if a == nil || a.observation == nil {
		return handoff.PublicState{}, failure.New(failure.FeatureUnavailable, map[string]string{"feature": "interactive_handoff"}, nil)
	}
	return a.observation.RequestHandoffPublic(ctx, req)
}

func (a *daemonActions) WaitHandoffPublic(ctx context.Context, req handoffapp.WaitRequest) (handoff.PublicState, bool, error) {
	if a == nil || a.observation == nil {
		return handoff.PublicState{}, false, failure.New(failure.FeatureUnavailable, map[string]string{"feature": "interactive_handoff"}, nil)
	}
	return a.observation.WaitHandoffPublic(ctx, req)
}

func (a *daemonActions) AbortHandoffPublic(ctx context.Context, id string) (handoff.PublicState, error) {
	if a == nil || a.observation == nil {
		return handoff.PublicState{}, failure.New(failure.FeatureUnavailable, map[string]string{"feature": "interactive_handoff"}, nil)
	}
	return a.observation.AbortHandoffPublic(ctx, id)
}

func (a *daemonActions) InspectHandoffPublic(ctx context.Context, id string) (handoff.PublicState, error) {
	if a == nil || a.observation == nil {
		return handoff.PublicState{}, failure.New(failure.FeatureUnavailable, map[string]string{"feature": "interactive_handoff"}, nil)
	}
	return a.observation.InspectHandoffPublic(ctx, id)
}
