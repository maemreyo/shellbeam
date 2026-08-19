package main

import (
	"context"
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	handoffapp "github.com/maemreyo/shellbeam/internal/app/interactivehandoff"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
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
