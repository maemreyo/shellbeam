package interactivehandoff

import (
	"context"

	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

// Expire durably revokes the current handoff authority generation before any
// provider cleanup. The delegated session itself is never signalled or killed.
func (s *Service) Expire(ctx context.Context, handoffID string) (handoff.State, error) {
	unlock := s.lockMutation(handoffID)
	defer unlock()
	state, err := s.store.RecoverHandoff(ctx, handoffID)
	if err != nil {
		return handoff.State{}, failure.Normalize(err)
	}
	if state.Phase == handoff.PhaseAgentOwned || state.Phase == handoff.PhaseAborted {
		return state, nil
	}
	if state.FailureCode != failure.HandoffExpired || state.Phase != handoff.PhaseReclaimPending || state.DesiredOwner != delegated.OwnerNone {
		state.AuthorityEpoch++
		state.DesiredOwner = delegated.OwnerNone
		state.Phase = handoff.PhaseReclaimPending
		state.TransferBoundary = handoff.TransferBoundary{Kind: handoff.BoundaryNone}
		state.FailureCode = failure.HandoffExpired
		if state.AgentIngress == handoff.IngressWritable {
			state.AgentIngress = handoff.IngressUnknown
		}
		if err := s.advance(ctx, state); err != nil {
			return handoff.State{}, err
		}
	}
	return s.finishExpiryLocked(ctx, state)
}

func (s *Service) finishExpiryLocked(ctx context.Context, state handoff.State) (handoff.State, error) {
	if state.FailureCode != failure.HandoffExpired || state.DesiredOwner != delegated.OwnerNone {
		return state, failure.New(failure.HandoffReclaimBlocked, map[string]string{"handoff_id": state.HandoffID, "reason": "expiry_intent_missing", "phase": string(state.Phase)}, nil)
	}
	changed := false
	if state.AgentIngress != handoff.IngressFenced {
		proof, err := s.fencer.FenceAgentIngress(ctx, state.SessionID, state.AuthorityEpoch)
		if err == nil && proof.Fenced && proof.AuthorityEpoch == state.AuthorityEpoch && proof.ProviderGeneration == state.ProviderGeneration {
			state.AgentIngress = handoff.IngressFenced
			changed = true
		} else {
			state.AgentIngress = handoff.IngressUnknown
		}
	}

	if state.HumanClient != nil {
		ref, err := s.store.LoadDelegatedProviderRef(ctx, operation.SessionID(state.SessionID))
		if err != nil {
			return state, failure.Normalize(err)
		}
		client := delegatedapp.ProviderClientRef{Ref: state.HumanClient.Ref}
		if state.HumanIngress != handoff.IngressFenced {
			proof, fenceErr := s.runtime.FenceHumanIngress(ctx, ref, client, state.AuthorityEpoch)
			switch {
			case fenceErr == nil && proof.Fenced && proof.AuthorityEpoch == state.AuthorityEpoch && proof.ProviderGeneration == state.ProviderGeneration:
				state.HumanIngress = handoff.IngressFenced
				state.ProviderOwner = delegated.OwnerNone
				changed = true
			case exactClientMissing(fenceErr):
				state.HumanIngress = handoff.IngressFenced
				state.ProviderOwner = delegated.OwnerNone
				state.HumanClient = nil
				changed = true
			default:
				state.HumanIngress = handoff.IngressUnknown
				changed = true
			}
		}
		if state.HumanIngress == handoff.IngressFenced && state.HumanClient != nil {
			if err := s.runtime.PrepareReadOnlyLocalControl(ctx, ref, client); exactClientMissing(err) {
				state.HumanClient = nil
				changed = true
			}
		}
	} else if state.HumanIngress != handoff.IngressFenced {
		// With no exact human client reference there is no basis for inventing a
		// fence unless prior canonical state already proved one.
		state.HumanIngress = handoff.IngressUnknown
	}

	if state.AgentIngress == handoff.IngressFenced && state.HumanIngress == handoff.IngressFenced {
		if state.Phase != handoff.PhaseAborted {
			state.Phase = handoff.PhaseAborted
			changed = true
		}
	} else {
		if state.Phase != handoff.PhaseReclaimPending || state.TransferBoundary.Established {
			state.Phase = handoff.PhaseReclaimPending
			state.TransferBoundary = handoff.TransferBoundary{Kind: handoff.BoundaryNone}
			changed = true
		}
	}
	if changed {
		if err := s.advance(ctx, state); err != nil {
			return handoff.State{}, err
		}
	}
	if state.Phase == handoff.PhaseReclaimPending {
		return state, failure.New(failure.HandoffReclaimBlocked, map[string]string{"handoff_id": state.HandoffID, "reason": "expiry_fence_unproven", "phase": string(state.Phase)}, nil)
	}
	return state, nil
}
