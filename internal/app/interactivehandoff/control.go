package interactivehandoff

import (
	"context"

	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func (s *Service) HumanControl(ctx context.Context, signal handoff.ControlSignal) (ControlResult, error) {
	if err := signal.Validate(); err != nil {
		return ControlResult{}, err
	}
	unlock := s.lockMutation(signal.HandoffID)
	defer unlock()
	if signal.Kind == handoff.HumanControlRequestControl {
		return ControlResult{}, failure.New(failure.FeatureUnavailable, map[string]string{"feature": "human_request_control"}, nil)
	}
	_, outcome, created, err := s.store.ReserveControlSignal(ctx, signal)
	if err != nil {
		return ControlResult{}, failure.Normalize(err)
	}
	req, state, err := s.store.LoadHandoff(ctx, signal.HandoffID)
	if err != nil {
		return ControlResult{}, failure.Normalize(err)
	}
	if !created && outcome != "" {
		return ControlResult{State: state, Outcome: outcome}, nil
	}
	switch signal.Kind {
	case handoff.HumanControlStatus:
		return s.completeControl(ctx, signal, state, "status")
	case handoff.HumanControlReady:
		return s.ready(ctx, signal, state)
	case handoff.HumanControlAbort:
		state, err = s.abortLoaded(ctx, state)
		if err != nil {
			return ControlResult{}, err
		}
		return s.completeControl(ctx, signal, state, "aborted")
	case handoff.HumanControlResume:
		return s.resume(ctx, signal, req, state)
	case handoff.HumanControlTerminate:
		return s.terminate(ctx, signal, state)
	default:
		return ControlResult{}, failure.New(failure.InvalidInput, map[string]string{"field": "human_control_kind"}, nil)
	}
}

func (s *Service) ready(ctx context.Context, signal handoff.ControlSignal, state handoff.State) (ControlResult, error) {
	if state.Phase == handoff.PhaseAgentOwned {
		return s.completeControl(ctx, signal, state, "ready")
	}
	if state.Phase == handoff.PhaseHumanOwned {
		state.Phase = handoff.PhaseHumanFencing
		state.TransferBoundary = handoff.TransferBoundary{Kind: handoff.BoundaryHumanAttested, Established: true}
		if err := s.advance(ctx, state); err != nil {
			return ControlResult{}, err
		}
	}
	if state.Phase != handoff.PhaseHumanFencing || state.HumanClient == nil {
		return ControlResult{}, failure.New(failure.HandoffNotPending, map[string]string{"handoff_id": signal.HandoffID, "phase": string(state.Phase)}, nil)
	}
	ref, err := s.store.LoadDelegatedProviderRef(ctx, operation.SessionID(state.SessionID))
	if err != nil {
		return ControlResult{}, failure.Normalize(err)
	}
	client := delegatedapp.ProviderClientRef{Ref: state.HumanClient.Ref}
	if state.HumanIngress != handoff.IngressFenced {
		proof, err := s.runtime.FenceHumanIngress(ctx, ref, client, state.AuthorityEpoch)
		if err != nil {
			return ControlResult{}, failure.Normalize(err)
		}
		if !proof.Fenced || proof.AuthorityEpoch != state.AuthorityEpoch || proof.ProviderGeneration != state.ProviderGeneration {
			return ControlResult{}, failure.New(failure.HandoffReclaimBlocked, map[string]string{"handoff_id": state.HandoffID, "reason": "human_fence_unproven"}, nil)
		}
		state.HumanIngress = handoff.IngressFenced
		state.ProviderOwner = delegated.OwnerNone
		if err := s.advance(ctx, state); err != nil {
			return ControlResult{}, err
		}
	}
	if err := s.runtime.PrepareReadOnlyLocalControl(ctx, ref, client); err != nil {
		return ControlResult{}, failure.Normalize(err)
	}
	obs, err := s.runtime.Inspect(ctx, ref)
	if err != nil {
		return ControlResult{}, failure.Normalize(err)
	}
	binding, err := s.store.LoadDelegatedBinding(ctx, operation.SessionID(state.SessionID))
	if err != nil {
		return ControlResult{}, failure.Normalize(err)
	}
	if obs.Provider != binding.ProviderIdentity() {
		return ControlResult{}, failure.New(failure.DelegatedProviderMismatch, map[string]string{"session_id": state.SessionID, "provider_id": obs.Provider.ID, "expected_provider_id": binding.ProviderID}, nil)
	}
	if binding.AuthorityEpoch != state.AuthorityEpoch || binding.DesiredOwner != delegated.OwnerHuman || !obs.ProviderCurrent || obs.ProviderGeneration != state.ProviderGeneration || obs.Owner != delegated.OwnerAgent {
		return ControlResult{}, failure.New(failure.HandoffReclaimBlocked, map[string]string{"handoff_id": state.HandoffID, "reason": "agent_provider_unproven"}, nil)
	}
	state.AuthorityEpoch++
	state.DesiredOwner = delegated.OwnerAgent
	state.ProviderOwner = delegated.OwnerAgent
	state.AgentIngress = handoff.IngressWritable
	state.HumanIngress = handoff.IngressFenced
	state.Phase = handoff.PhaseAgentOwned
	if err := s.advance(ctx, state); err != nil {
		return ControlResult{}, err
	}
	return s.completeControl(ctx, signal, state, "ready")
}

func (s *Service) Abort(ctx context.Context, handoffID string) (handoff.State, error) {
	unlock := s.lockMutation(handoffID)
	defer unlock()
	_, state, found, err := s.store.FindHandoff(ctx, handoffID)
	if err != nil {
		return handoff.State{}, failure.Normalize(err)
	}
	if !found {
		return handoff.State{}, failure.New(failure.HandoffNotPending, map[string]string{"handoff_id": handoffID}, nil)
	}
	return s.abortLoaded(ctx, state)
}

func (s *Service) abortLoaded(ctx context.Context, state handoff.State) (handoff.State, error) {
	if state.Phase == handoff.PhaseAborted {
		return state, nil
	}
	if state.Phase != handoff.PhaseHumanOwned && state.Phase != handoff.PhaseHumanConnecting && state.Phase != handoff.PhaseHumanFencing && state.Phase != handoff.PhaseAgentFencing {
		return handoff.State{}, failure.New(failure.HandoffNotPending, map[string]string{"handoff_id": state.HandoffID, "phase": string(state.Phase)}, nil)
	}
	if state.Phase == handoff.PhaseAgentFencing && state.AgentIngress != handoff.IngressFenced {
		fenced, err := s.finishAgentFence(ctx, state)
		if err != nil {
			return handoff.State{}, err
		}
		state = fenced
	}
	if state.HumanClient != nil {
		ref, err := s.store.LoadDelegatedProviderRef(ctx, operation.SessionID(state.SessionID))
		if err != nil {
			return handoff.State{}, failure.Normalize(err)
		}
		client := delegatedapp.ProviderClientRef{Ref: state.HumanClient.Ref}
		if state.HumanIngress != handoff.IngressFenced {
			proof, err := s.runtime.FenceHumanIngress(ctx, ref, client, state.AuthorityEpoch)
			if err != nil {
				return handoff.State{}, failure.Normalize(err)
			}
			if !proof.Fenced || proof.AuthorityEpoch != state.AuthorityEpoch || proof.ProviderGeneration != state.ProviderGeneration {
				return handoff.State{}, failure.New(failure.HandoffReclaimBlocked, map[string]string{"handoff_id": state.HandoffID, "reason": "abort_fence_unproven"}, nil)
			}
			state.HumanIngress = handoff.IngressFenced
			state.ProviderOwner = delegated.OwnerNone
		}
		if err := s.runtime.PrepareReadOnlyLocalControl(ctx, ref, client); err != nil {
			return handoff.State{}, failure.Normalize(err)
		}
	}
	s.cancelReadiness(state.HandoffID)
	state.AuthorityEpoch++
	state.DesiredOwner = delegated.OwnerNone
	state.AgentIngress = handoff.IngressFenced
	state.HumanIngress = handoff.IngressFenced
	state.Phase = handoff.PhaseAborted
	if err := s.advance(ctx, state); err != nil {
		return handoff.State{}, err
	}
	return state, nil
}

func (s *Service) resume(ctx context.Context, signal handoff.ControlSignal, req handoff.Request, state handoff.State) (ControlResult, error) {
	if state.FailureCode == failure.HandoffExpired {
		return ControlResult{}, failure.New(failure.HandoffExpired, map[string]string{"handoff_id": state.HandoffID}, nil)
	}
	if state.Phase == handoff.PhaseHumanConnecting {
		// A response may have been lost after the durable resume transition.
		// HUMAN_CONNECTING is the terminal state of the resume control itself;
		// the local attach/bind path owns creation and proof of the next client.
		return s.completeControl(ctx, signal, state, "resumed")
	}
	if state.Phase != handoff.PhaseAborted {
		return ControlResult{}, failure.New(failure.HandoffReclaimBlocked, map[string]string{"handoff_id": state.HandoffID, "reason": "resume_state"}, nil)
	}
	state.AuthorityEpoch++
	state.DesiredOwner = delegated.OwnerHuman
	state.ProviderOwner = delegated.OwnerNone
	state.AgentIngress = handoff.IngressFenced
	state.HumanIngress = handoff.IngressFenced
	state.Phase = handoff.PhaseHumanConnecting
	state.HumanClient = nil
	s.forgetAttachment(state.HandoffID)
	state.TransferBoundary = handoff.TransferBoundary{Kind: handoff.BoundaryProviderOrdered, Established: true}
	if err := s.advance(ctx, state); err != nil {
		return ControlResult{}, err
	}
	if s.h4Enabled {
		var err error
		state, err = s.prepareH4(ctx, req, state)
		if err != nil {
			return ControlResult{}, err
		}
	}
	return s.completeControl(ctx, signal, state, "resumed")
}

func (s *Service) terminate(ctx context.Context, signal handoff.ControlSignal, state handoff.State) (ControlResult, error) {
	if state.Phase != handoff.PhaseAborted && state.Phase != handoff.PhaseHumanFencing && state.Phase != handoff.PhaseReclaimPending {
		return ControlResult{}, failure.New(failure.HandoffNotPending, map[string]string{"handoff_id": state.HandoffID, "phase": string(state.Phase)}, nil)
	}
	ref, err := s.store.LoadDelegatedProviderRef(ctx, operation.SessionID(state.SessionID))
	if err != nil {
		return ControlResult{}, failure.Normalize(err)
	}
	if err := s.runtime.Signal(ctx, ref, "TERM"); err != nil {
		return ControlResult{}, failure.Normalize(err)
	}
	return s.completeControl(ctx, signal, state, "terminated")
}

func (s *Service) completeControl(ctx context.Context, signal handoff.ControlSignal, state handoff.State, outcome string) (ControlResult, error) {
	stored, err := s.store.CompleteControlSignal(ctx, signal, outcome)
	if err != nil {
		return ControlResult{}, failure.Normalize(err)
	}
	return ControlResult{State: state, Outcome: stored}, nil
}
