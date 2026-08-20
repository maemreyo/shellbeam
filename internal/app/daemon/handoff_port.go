package daemon

import (
	"context"
	"errors"
	"fmt"
	"time"

	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	handoffapp "github.com/maemreyo/shellbeam/internal/app/interactivehandoff"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	terminalpresentation "github.com/maemreyo/shellbeam/internal/core/terminalpresentation"
)

type handoffCoordinator interface {
	Reconcile(context.Context, string) (handoff.State, error)
	Expire(context.Context, string) (handoff.State, error)
	Request(context.Context, handoff.Request) (handoff.State, error)
	Wait(context.Context, handoffapp.WaitRequest) (handoffapp.WaitResult, error)
	Abort(context.Context, string) (handoff.State, error)
	Inspect(context.Context, string) (handoff.State, error)
	BootstrapLocalHuman(context.Context, string) (handoffapp.LocalBootstrap, error)
	BindLocalHuman(context.Context, string, delegatedapp.ProviderClientRef) (handoff.State, error)
	AttachLocalHuman(context.Context, string, delegatedapp.HumanAttachSpec) (handoffapp.LocalAttachResult, error)
	HumanControl(context.Context, handoff.ControlSignal) (handoffapp.ControlResult, error)
	ProjectPublic(context.Context, handoff.State) (handoff.PublicState, error)
}

type handoffPresentationCoordinator interface {
	RequestWithPresentation(context.Context, handoff.Request, *terminalpresentation.BridgeAffinityHint) (handoff.State, error)
}

type handoffStoreAdapter struct {
	handoff   InteractiveHandoffStore
	delegated DelegatedSessionStore
}

func configureHandoffCoordinator(service *Service) {
	if service == nil || service.store == nil || service.options.DelegatedRuntime == nil {
		return
	}
	handoffStore, ok := service.store.(InteractiveHandoffStore)
	if !ok {
		return
	}
	delegatedStore, ok := service.store.(DelegatedSessionStore)
	if !ok {
		return
	}
	provider := delegatedapp.New(service.options.DelegatedRuntime)
	coordinator := handoffapp.NewWithPresenter(
		handoffStoreAdapter{handoff: handoffStore, delegated: delegatedStore},
		provider,
		daemonAgentIngressFencer{service: service},
		service.options.HandoffPresenter,
	)
	if service.options.HandoffPresenterFactory != nil {
		if presenter := service.options.HandoffPresenterFactory(coordinator); presenter != nil {
			coordinator.SetPresenter(presenter)
		}
	}
	service.handoff = coordinator
}

func (a handoffStoreAdapter) FindHandoff(ctx context.Context, id string) (handoff.Request, handoff.State, bool, error) {
	return a.handoff.FindHandoff(ctx, id)
}
func (a handoffStoreAdapter) ReserveHandoff(ctx context.Context, req handoff.Request, state handoff.State) (handoff.State, bool, error) {
	stored, created, result := a.handoff.ReserveHandoff(ctx, req, state)
	return stored, created, handoffStoreError(result)
}
func (a handoffStoreAdapter) AdvanceHandoff(ctx context.Context, state handoff.State) error {
	return handoffStoreError(a.handoff.AdvanceHandoff(ctx, state))
}
func (a handoffStoreAdapter) LoadHandoff(ctx context.Context, id string) (handoff.Request, handoff.State, error) {
	return a.handoff.LoadHandoff(ctx, id)
}
func (a handoffStoreAdapter) RecoverHandoff(ctx context.Context, id string) (handoff.State, error) {
	state, result := a.handoff.RecoverHandoff(ctx, id)
	return state, handoffStoreError(result)
}
func (a handoffStoreAdapter) LoadDelegatedBinding(ctx context.Context, id operation.SessionID) (delegated.Binding, error) {
	return a.delegated.LoadDelegatedBinding(ctx, id)
}
func (a handoffStoreAdapter) LoadDelegatedProviderRef(ctx context.Context, id operation.SessionID) (delegated.ProviderRef, error) {
	return a.delegated.LoadDelegatedProviderRef(ctx, id)
}
func (a handoffStoreAdapter) ReserveControlSignal(ctx context.Context, signal handoff.ControlSignal) (handoff.ControlSignal, string, bool, error) {
	stored, outcome, created, result := a.handoff.ReserveControlSignal(ctx, signal)
	return stored, outcome, created, handoffStoreError(result)
}
func (a handoffStoreAdapter) CompleteControlSignal(ctx context.Context, signal handoff.ControlSignal, outcome string) (string, error) {
	stored, result := a.handoff.CompleteControlSignal(ctx, signal, outcome)
	return stored, handoffStoreError(result)
}
func (a handoffStoreAdapter) MarkHumanWriteAuthorityGranted(ctx context.Context, id operation.SessionID) error {
	return handoffStoreError(a.handoff.MarkHumanWriteAuthorityGranted(ctx, id))
}
func (a handoffStoreAdapter) LoadInputAuthorityProvenance(ctx context.Context, id operation.SessionID) (string, error) {
	return a.handoff.LoadInputAuthorityProvenance(ctx, id)
}

func (a handoffStoreAdapter) LoadHandoffTimestamps(ctx context.Context, id string) (time.Time, time.Time, error) {
	store, ok := a.handoff.(interface {
		LoadHandoffTimestamps(context.Context, string) (time.Time, time.Time, error)
	})
	if !ok {
		return time.Time{}, time.Time{}, failure.New(failure.PersistenceUnavailable, map[string]string{"feature": "interactive_handoff_timestamps"}, nil)
	}
	return store.LoadHandoffTimestamps(ctx, id)
}

func handoffStoreError(result StoreResult) error {
	if result.Err == nil {
		return nil
	}
	var typed *failure.Failure
	if errors.As(result.Err, &typed) {
		return result.Err
	}
	if result.Durability == AmbiguousChange {
		return failure.New(failure.PersistenceAmbiguous, nil, result.Err)
	}
	return failure.New(failure.PersistenceUnavailable, nil, result.Err)
}

type daemonAgentIngressFencer struct{ service *Service }

func (f daemonAgentIngressFencer) FenceAgentIngress(ctx context.Context, sessionID string, epoch delegated.AuthorityEpoch) (handoffapp.AgentIngressProof, error) {
	if f.service == nil || f.service.options.DelegatedRuntime == nil {
		return handoffapp.AgentIngressProof{}, failure.New(failure.FeatureUnavailable, map[string]string{"feature": "interactive_handoff"}, nil)
	}
	live := f.service.get(sessionID)
	if live == nil {
		return handoffapp.AgentIngressProof{}, failure.New(failure.HandoffReclaimBlocked, map[string]string{"session_id": sessionID, "reason": "agent_ingress_live_session_missing"}, nil)
	}
	live.mu.Lock()
	isDelegated := live.delegated
	live.mu.Unlock()
	if !isDelegated {
		return handoffapp.AgentIngressProof{}, failure.New(failure.HandoffReclaimBlocked, map[string]string{"session_id": sessionID, "reason": "agent_ingress_not_delegated"}, nil)
	}
	// The mutation lane stays locked through the proof. Any old-epoch mutation
	// admitted before ReserveHandoff must finish provider delivery first; after
	// the durable owner/epoch rotation, unseen agent mutations cannot reserve.
	live.delegatedMutationMu.Lock()
	defer live.delegatedMutationMu.Unlock()
	store := f.service.delegatedStore()
	if store == nil {
		return handoffapp.AgentIngressProof{}, failure.New(failure.PersistenceUnavailable, nil, nil)
	}
	binding, err := store.LoadDelegatedBinding(ctx, operation.SessionID(sessionID))
	if err != nil {
		return handoffapp.AgentIngressProof{}, failure.Normalize(err)
	}
	if binding.Lifecycle != delegated.LifecycleLive || binding.AuthorityEpoch != epoch || binding.DesiredOwner != delegated.OwnerHuman {
		return handoffapp.AgentIngressProof{}, failure.New(failure.HandoffReclaimBlocked, map[string]string{
			"session_id": sessionID,
			"reason":     "agent_ingress_binding_unproven",
			"epoch":      fmt.Sprint(binding.AuthorityEpoch),
		}, nil)
	}
	ref, err := store.LoadDelegatedProviderRef(ctx, operation.SessionID(sessionID))
	if err != nil {
		return handoffapp.AgentIngressProof{}, failure.Normalize(err)
	}
	obs, err := f.service.options.DelegatedRuntime.Inspect(ctx, ref)
	if err != nil {
		return handoffapp.AgentIngressProof{}, failure.Normalize(err)
	}
	if obs.Provider != binding.ProviderIdentity() {
		return handoffapp.AgentIngressProof{}, failure.New(failure.DelegatedProviderMismatch, map[string]string{
			"session_id":           sessionID,
			"provider_id":          obs.Provider.ID,
			"expected_provider_id": binding.ProviderID,
		}, nil)
	}
	if !obs.ProviderCurrent || obs.ProviderGeneration == "" || obs.Owner != delegated.OwnerAgent {
		return handoffapp.AgentIngressProof{}, failure.New(failure.HandoffReclaimBlocked, map[string]string{"session_id": sessionID, "reason": "agent_ingress_provider_unproven"}, nil)
	}
	return handoffapp.AgentIngressProof{AuthorityEpoch: epoch, ProviderGeneration: obs.ProviderGeneration, Fenced: true}, nil
}
