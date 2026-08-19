package interactivehandoff

import (
	"context"
	"errors"

	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

// Reconcile re-proves a durable handoff against the exact current provider state.
// It never treats disappearance or proof loss as successful human completion.
func (s *Service) Reconcile(ctx context.Context, handoffID string) (handoff.State, error) {
	unlock := s.lockMutation(handoffID)
	defer unlock()
	state, err := s.store.RecoverHandoff(ctx, handoffID)
	if err != nil {
		return handoff.State{}, failure.Normalize(err)
	}
	if err := state.ValidateH2(); err != nil {
		return handoff.State{}, err
	}
	switch state.Phase {
	case handoff.PhaseAgentFencing:
		return s.finishAgentFence(ctx, state)
	case handoff.PhaseHumanConnecting:
		return s.reconcileHumanConnecting(ctx, state)
	case handoff.PhaseHumanOwned:
		return s.reconcileHumanOwned(ctx, state)
	case handoff.PhaseHumanFencing:
		return s.reconcileHumanFencing(ctx, state)
	case handoff.PhaseReclaimPending:
		return s.reconcileBlocked(ctx, state)
	case handoff.PhaseAborted:
		return s.reconcileAborted(ctx, state)
	case handoff.PhaseAgentOwned:
		return state, nil
	default:
		return handoff.State{}, failure.New(failure.HandoffReclaimBlocked, map[string]string{"handoff_id": state.HandoffID, "reason": "unknown_reconcile_phase", "phase": string(state.Phase)}, nil)
	}
}

func (s *Service) reconcileHumanConnecting(ctx context.Context, state handoff.State) (handoff.State, error) {
	ref, err := s.loadReconcileProvider(ctx, state)
	if err != nil {
		return handoff.State{}, err
	}
	if state.HumanClient == nil {
		if _, err := s.requireCurrentGeneration(ctx, ref, state); err != nil {
			return handoff.State{}, err
		}
		return state, nil
	}
	client := delegatedapp.ProviderClientRef{Ref: state.HumanClient.Ref}
	obs, err := s.runtime.InspectHumanClient(ctx, ref, client)
	if err != nil {
		if exactClientMissing(err) {
			return s.persistConnectingWithoutClient(ctx, state)
		}
		if errors.Is(err, failure.HandoffClientLost) || errors.Is(err, failure.HumanClientNotProven) {
			return s.persistClientProofBlocked(ctx, state)
		}
		return handoff.State{}, failure.Normalize(err)
	}
	if !obs.Present || obs.ProviderGeneration != state.ProviderGeneration {
		return s.persistClientProofBlocked(ctx, state)
	}
	if obs.ReadOnly && obs.ObservedOwner == delegated.OwnerNone {
		return s.bindLocalHumanLocked(ctx, ref, state, client)
	}
	if !obs.ReadOnly && obs.ObservedOwner == delegated.OwnerHuman {
		if err := s.store.MarkHumanWriteAuthorityGranted(ctx, operation.SessionID(state.SessionID)); err != nil {
			return handoff.State{}, failure.Normalize(err)
		}
		if err := s.runtime.ArmWritableHumanControl(ctx, ref, client, delegatedapp.HumanControlSpec{HandoffID: state.HandoffID, AuthorityEpoch: state.AuthorityEpoch}); err != nil {
			return handoff.State{}, failure.Normalize(err)
		}
		state.Phase = handoff.PhaseHumanOwned
		state.FailureCode = ""
		state.ProviderOwner = delegated.OwnerHuman
		state.AgentIngress = handoff.IngressFenced
		state.HumanIngress = handoff.IngressWritable
		if err := s.advance(ctx, state); err != nil {
			return handoff.State{}, err
		}
		return state, nil
	}
	return s.persistClientProofBlocked(ctx, state)
}

func (s *Service) reconcileHumanOwned(ctx context.Context, state handoff.State) (handoff.State, error) {
	if state.HumanClient == nil {
		return s.persistClientProofBlocked(ctx, state)
	}
	ref, err := s.loadReconcileProvider(ctx, state)
	if err != nil {
		return handoff.State{}, err
	}
	client := delegatedapp.ProviderClientRef{Ref: state.HumanClient.Ref}
	obs, err := s.runtime.InspectHumanClient(ctx, ref, client)
	if err != nil {
		if exactClientMissing(err) {
			return s.persistConnectingWithoutClient(ctx, state)
		}
		if errors.Is(err, failure.HandoffClientLost) || errors.Is(err, failure.HumanClientNotProven) {
			return s.persistClientProofBlocked(ctx, state)
		}
		return handoff.State{}, failure.Normalize(err)
	}
	if !obs.Present || obs.ProviderGeneration != state.ProviderGeneration {
		return s.persistClientProofBlocked(ctx, state)
	}
	if obs.ReadOnly && obs.ObservedOwner == delegated.OwnerNone {
		return s.persistConnectingWithoutClient(ctx, state)
	}
	if obs.ReadOnly || obs.ObservedOwner != delegated.OwnerHuman {
		return s.persistClientProofBlocked(ctx, state)
	}
	if err := s.runtime.ArmWritableHumanControl(ctx, ref, client, delegatedapp.HumanControlSpec{HandoffID: state.HandoffID, AuthorityEpoch: state.AuthorityEpoch}); err != nil {
		proof, fenceErr := s.runtime.FenceHumanIngress(ctx, ref, client, state.AuthorityEpoch)
		if fenceErr != nil || !proof.Fenced || proof.AuthorityEpoch != state.AuthorityEpoch || proof.ProviderGeneration != state.ProviderGeneration {
			return s.persistClientProofBlocked(ctx, state)
		}
		_ = s.runtime.PrepareReadOnlyLocalControl(ctx, ref, client)
		return s.persistConnectingAfterFence(ctx, state, failure.HandoffReclaimBlocked)
	}
	if state.FailureCode != "" {
		state.FailureCode = ""
		if err := s.advance(ctx, state); err != nil {
			return handoff.State{}, err
		}
	}
	return state, nil
}

func (s *Service) reconcileHumanFencing(ctx context.Context, state handoff.State) (handoff.State, error) {
	if state.HumanClient == nil || !state.TransferBoundary.Established {
		return s.persistClientProofBlocked(ctx, state)
	}
	ref, err := s.loadReconcileProvider(ctx, state)
	if err != nil {
		return handoff.State{}, err
	}
	client := delegatedapp.ProviderClientRef{Ref: state.HumanClient.Ref}
	if state.HumanIngress != handoff.IngressFenced {
		proof, fenceErr := s.runtime.FenceHumanIngress(ctx, ref, client, state.AuthorityEpoch)
		if fenceErr != nil {
			if exactClientMissing(fenceErr) {
				state.HumanClient = nil
				state.HumanIngress = handoff.IngressFenced
				state.ProviderOwner = delegated.OwnerNone
				if err := s.advance(ctx, state); err != nil {
					return handoff.State{}, err
				}
			} else if errors.Is(fenceErr, failure.HandoffClientLost) || errors.Is(fenceErr, failure.HumanClientNotProven) {
				return s.persistClientProofBlocked(ctx, state)
			} else {
				return handoff.State{}, failure.Normalize(fenceErr)
			}
		} else {
			if !proof.Fenced || proof.AuthorityEpoch != state.AuthorityEpoch || proof.ProviderGeneration != state.ProviderGeneration {
				return s.persistClientProofBlocked(ctx, state)
			}
			state.HumanIngress = handoff.IngressFenced
			state.ProviderOwner = delegated.OwnerNone
			if err := s.advance(ctx, state); err != nil {
				return handoff.State{}, err
			}
		}
	}
	if state.HumanClient != nil {
		if err := s.runtime.PrepareReadOnlyLocalControl(ctx, ref, client); err != nil {
			if !exactClientMissing(err) {
				return handoff.State{}, failure.Normalize(err)
			}
			state.HumanClient = nil
		}
	}
	obs, err := s.runtime.Inspect(ctx, ref)
	if err != nil {
		return handoff.State{}, failure.Normalize(err)
	}
	binding, err := s.store.LoadDelegatedBinding(ctx, operation.SessionID(state.SessionID))
	if err != nil {
		return handoff.State{}, failure.Normalize(err)
	}
	if binding.AuthorityEpoch != state.AuthorityEpoch || binding.DesiredOwner != delegated.OwnerHuman || obs.Provider != binding.ProviderIdentity() || !obs.ProviderCurrent || obs.ProviderGeneration != state.ProviderGeneration || obs.Owner != delegated.OwnerAgent {
		return handoff.State{}, failure.New(failure.HandoffReclaimBlocked, map[string]string{"handoff_id": state.HandoffID, "reason": "agent_provider_unproven", "phase": string(state.Phase)}, nil)
	}
	state.AuthorityEpoch++
	state.DesiredOwner = delegated.OwnerAgent
	state.ProviderOwner = delegated.OwnerAgent
	state.AgentIngress = handoff.IngressWritable
	state.HumanIngress = handoff.IngressFenced
	state.Phase = handoff.PhaseAgentOwned
	state.FailureCode = ""
	if err := s.advance(ctx, state); err != nil {
		return handoff.State{}, err
	}
	return state, nil
}

func (s *Service) reconcileBlocked(ctx context.Context, state handoff.State) (handoff.State, error) {
	if state.FailureCode == failure.HandoffExpired && state.DesiredOwner == delegated.OwnerNone {
		return s.finishExpiryLocked(ctx, state)
	}
	if state.DesiredOwner != delegated.OwnerHuman || state.HumanClient == nil {
		return state, nil
	}
	// A blocked human proof can recover only from fresh exact client facts.
	ref, err := s.loadReconcileProvider(ctx, state)
	if err != nil {
		return handoff.State{}, err
	}
	client := delegatedapp.ProviderClientRef{Ref: state.HumanClient.Ref}
	obs, err := s.runtime.InspectHumanClient(ctx, ref, client)
	if err != nil {
		if exactClientMissing(err) {
			return s.persistConnectingWithoutClient(ctx, state)
		}
		return state, nil
	}
	if obs.ProviderGeneration != state.ProviderGeneration || !obs.Present {
		return state, nil
	}
	if obs.ReadOnly && obs.ObservedOwner == delegated.OwnerNone {
		return s.persistConnectingWithoutClient(ctx, state)
	}
	if !obs.ReadOnly && obs.ObservedOwner == delegated.OwnerHuman {
		state.Phase = handoff.PhaseHumanOwned
		state.FailureCode = ""
		state.ProviderOwner = delegated.OwnerHuman
		state.AgentIngress = handoff.IngressFenced
		state.HumanIngress = handoff.IngressWritable
		if err := s.store.MarkHumanWriteAuthorityGranted(ctx, operation.SessionID(state.SessionID)); err != nil {
			return handoff.State{}, failure.Normalize(err)
		}
		if err := s.runtime.ArmWritableHumanControl(ctx, ref, client, delegatedapp.HumanControlSpec{HandoffID: state.HandoffID, AuthorityEpoch: state.AuthorityEpoch}); err != nil {
			return handoff.State{}, failure.Normalize(err)
		}
		if err := s.advance(ctx, state); err != nil {
			return handoff.State{}, err
		}
	}
	return state, nil
}

func (s *Service) reconcileAborted(ctx context.Context, state handoff.State) (handoff.State, error) {
	if state.HumanClient == nil {
		return state, nil
	}
	ref, err := s.loadReconcileProvider(ctx, state)
	if err != nil {
		return handoff.State{}, err
	}
	client := delegatedapp.ProviderClientRef{Ref: state.HumanClient.Ref}
	obs, err := s.runtime.InspectHumanClient(ctx, ref, client)
	if err != nil {
		if exactClientMissing(err) {
			state.HumanClient = nil
			if err := s.advance(ctx, state); err != nil {
				return handoff.State{}, err
			}
			return state, nil
		}
		return s.persistClientProofBlocked(ctx, state)
	}
	if obs.ProviderGeneration != state.ProviderGeneration || !obs.Present {
		return s.persistClientProofBlocked(ctx, state)
	}
	if !obs.ReadOnly || obs.ObservedOwner != delegated.OwnerNone {
		proof, err := s.runtime.FenceHumanIngress(ctx, ref, client, state.AuthorityEpoch)
		if err != nil || !proof.Fenced || proof.AuthorityEpoch != state.AuthorityEpoch || proof.ProviderGeneration != state.ProviderGeneration {
			return s.persistClientProofBlocked(ctx, state)
		}
	}
	if err := s.runtime.PrepareReadOnlyLocalControl(ctx, ref, client); err != nil && !exactClientMissing(err) {
		return handoff.State{}, failure.Normalize(err)
	}
	return state, nil
}

func (s *Service) loadReconcileProvider(ctx context.Context, state handoff.State) (delegated.ProviderRef, error) {
	binding, err := s.store.LoadDelegatedBinding(ctx, operation.SessionID(state.SessionID))
	if err != nil {
		return delegated.ProviderRef{}, failure.Normalize(err)
	}
	if binding.Lifecycle != delegated.LifecycleLive || binding.AuthorityEpoch != state.AuthorityEpoch || binding.DesiredOwner != state.DesiredOwner {
		return delegated.ProviderRef{}, failure.New(failure.HandoffReclaimBlocked, map[string]string{"handoff_id": state.HandoffID, "reason": "durable_authority_mismatch", "phase": string(state.Phase)}, nil)
	}
	ref, err := s.store.LoadDelegatedProviderRef(ctx, operation.SessionID(state.SessionID))
	if err != nil {
		return delegated.ProviderRef{}, failure.Normalize(err)
	}
	return ref, nil
}

func (s *Service) requireCurrentGeneration(ctx context.Context, ref delegated.ProviderRef, state handoff.State) (delegatedapp.Observation, error) {
	obs, err := s.runtime.Inspect(ctx, ref)
	if err != nil {
		return delegatedapp.Observation{}, failure.Normalize(err)
	}
	binding, err := s.store.LoadDelegatedBinding(ctx, operation.SessionID(state.SessionID))
	if err != nil {
		return delegatedapp.Observation{}, failure.Normalize(err)
	}
	if obs.Provider != binding.ProviderIdentity() || !obs.ProviderCurrent || obs.ProviderGeneration != state.ProviderGeneration {
		return delegatedapp.Observation{}, failure.New(failure.HandoffReclaimBlocked, map[string]string{"handoff_id": state.HandoffID, "reason": "provider_generation_unproven", "phase": string(state.Phase)}, nil)
	}
	return obs, nil
}

func (s *Service) persistConnectingWithoutClient(ctx context.Context, state handoff.State) (handoff.State, error) {
	return s.persistConnectingAfterFence(ctx, state, failure.HandoffClientLost)
}

func (s *Service) persistConnectingAfterFence(ctx context.Context, state handoff.State, code failure.Code) (handoff.State, error) {
	state.Phase = handoff.PhaseHumanConnecting
	state.ProviderOwner = delegated.OwnerNone
	state.AgentIngress = handoff.IngressFenced
	state.HumanIngress = handoff.IngressFenced
	state.HumanClient = nil
	state.TransferBoundary = handoff.TransferBoundary{Kind: handoff.BoundaryProviderOrdered, Established: true}
	state.FailureCode = code
	if err := s.advance(ctx, state); err != nil {
		return handoff.State{}, err
	}
	return state, nil
}

func (s *Service) persistClientProofBlocked(ctx context.Context, state handoff.State) (handoff.State, error) {
	state.Phase = handoff.PhaseReclaimPending
	state.ProviderOwner = delegated.OwnerNone
	state.AgentIngress = handoff.IngressFenced
	state.HumanIngress = handoff.IngressUnknown
	state.TransferBoundary = handoff.TransferBoundary{Kind: handoff.BoundaryNone}
	state.FailureCode = failure.HandoffReclaimBlocked
	if err := s.advance(ctx, state); err != nil {
		return handoff.State{}, err
	}
	return state, failure.New(failure.HandoffReclaimBlocked, map[string]string{"handoff_id": state.HandoffID, "reason": "human_ingress_unproven", "phase": string(state.Phase)}, nil)
}

func exactClientMissing(err error) bool {
	var typed *failure.Failure
	return errors.As(err, &typed) && typed.Code == failure.HandoffClientLost && typed.Details["reason"] == "client_missing"
}
