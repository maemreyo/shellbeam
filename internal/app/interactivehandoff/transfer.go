package interactivehandoff

import (
	"context"

	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

type LocalBootstrap struct {
	HandoffID   string
	State       handoff.State
	ProviderRef delegated.ProviderRef
}

func (s *Service) BootstrapLocalHuman(ctx context.Context, handoffID string) (LocalBootstrap, error) {
	unlock := s.lockMutation(handoffID)
	defer unlock()
	_, state, found, err := s.store.FindHandoff(ctx, handoffID)
	if err != nil {
		return LocalBootstrap{}, failure.Normalize(err)
	}
	if !found {
		return LocalBootstrap{}, failure.New(failure.HandoffNotPending, map[string]string{"handoff_id": handoffID}, nil)
	}
	if state.Phase != handoff.PhaseHumanConnecting && state.Phase != handoff.PhaseHumanOwned {
		return LocalBootstrap{}, failure.New(failure.HandoffNotPending, map[string]string{"handoff_id": handoffID, "phase": string(state.Phase)}, nil)
	}
	ref, err := s.store.LoadDelegatedProviderRef(ctx, operation.SessionID(state.SessionID))
	if err != nil {
		return LocalBootstrap{}, failure.Normalize(err)
	}
	return LocalBootstrap{HandoffID: handoffID, State: state, ProviderRef: ref}, nil
}

func (s *Service) BindLocalHuman(ctx context.Context, handoffID string, client delegatedapp.ProviderClientRef) (handoff.State, error) {
	unlock := s.lockMutation(handoffID)
	defer unlock()
	if err := client.Validate(); err != nil {
		return handoff.State{}, failure.New(failure.InvalidInput, map[string]string{"field": "client_ref"}, err)
	}
	_, state, found, err := s.store.FindHandoff(ctx, handoffID)
	if err != nil {
		return handoff.State{}, failure.Normalize(err)
	}
	if !found {
		return handoff.State{}, failure.New(failure.HandoffNotPending, map[string]string{"handoff_id": handoffID}, nil)
	}
	if state.HumanClient != nil && state.HumanClient.Ref != client.Ref {
		return handoff.State{}, failure.New(failure.HandoffConflict, map[string]string{"handoff_id": handoffID}, nil)
	}
	if state.Phase == handoff.PhaseHumanOwned {
		if state.HumanClient == nil || state.HumanClient.Ref != client.Ref {
			return handoff.State{}, failure.New(failure.HandoffConflict, map[string]string{"handoff_id": handoffID}, nil)
		}
		return state, nil
	}
	if state.Phase != handoff.PhaseHumanConnecting || state.AgentIngress != handoff.IngressFenced || state.HumanIngress != handoff.IngressFenced {
		return handoff.State{}, failure.New(failure.HandoffNotPending, map[string]string{"handoff_id": handoffID, "phase": string(state.Phase)}, nil)
	}
	ref, err := s.store.LoadDelegatedProviderRef(ctx, operation.SessionID(state.SessionID))
	if err != nil {
		return handoff.State{}, failure.Normalize(err)
	}
	return s.bindLocalHumanLocked(ctx, ref, state, client)
}

func (s *Service) bindLocalHumanLocked(ctx context.Context, ref delegated.ProviderRef, state handoff.State, client delegatedapp.ProviderClientRef) (handoff.State, error) {
	obs, err := s.runtime.InspectHumanClient(ctx, ref, client)
	if err != nil {
		return handoff.State{}, failure.Normalize(err)
	}
	if !obs.Present || obs.ProviderGeneration != state.ProviderGeneration {
		return handoff.State{}, failure.New(failure.HumanClientNotProven, map[string]string{"handoff_id": state.HandoffID, "reason": "client_unproven"}, nil)
	}
	if state.HumanClient == nil {
		if !obs.ReadOnly || obs.ObservedOwner != delegated.OwnerNone {
			return handoff.State{}, failure.New(failure.HumanClientNotProven, map[string]string{"handoff_id": state.HandoffID, "reason": "prebound_client_not_read_only"}, nil)
		}
		state.HumanClient = &handoff.HumanClientRef{Ref: client.Ref}
		state.FailureCode = ""
		state.ProviderOwner = delegated.OwnerNone
		if err := s.advance(ctx, state); err != nil {
			return handoff.State{}, err
		}
	} else if !obs.ReadOnly {
		provenance, err := s.store.LoadInputAuthorityProvenance(ctx, operation.SessionID(state.SessionID))
		if err != nil {
			return handoff.State{}, failure.Normalize(err)
		}
		if obs.ObservedOwner != delegated.OwnerHuman || provenance != receipt.InputAuthorityHumanWriteGranted {
			return handoff.State{}, failure.New(failure.HumanClientNotProven, map[string]string{"handoff_id": state.HandoffID, "reason": "writable_client_without_durable_provenance"}, nil)
		}
	}
	if err := s.store.MarkHumanWriteAuthorityGranted(ctx, operation.SessionID(state.SessionID)); err != nil {
		return handoff.State{}, failure.Normalize(err)
	}
	obs, err = s.makeHumanWritable(ctx, ref, client, state)
	if err != nil {
		return handoff.State{}, err
	}
	state.Phase = handoff.PhaseHumanOwned
	state.ProviderOwner = obs.ObservedOwner
	state.HumanIngress = handoff.IngressWritable
	if err := s.advance(ctx, state); err != nil {
		_, _ = s.runtime.FenceHumanIngress(context.Background(), ref, client, state.AuthorityEpoch)
		return handoff.State{}, err
	}
	return state, nil
}

func (s *Service) AttachLocalHuman(ctx context.Context, handoffID string, spec delegatedapp.HumanAttachSpec) (LocalAttachResult, error) {
	unlock := s.lockMutation(handoffID)
	defer unlock()
	_, state, found, err := s.store.FindHandoff(ctx, handoffID)
	if err != nil {
		return LocalAttachResult{}, failure.Normalize(err)
	}
	if !found {
		return LocalAttachResult{}, failure.New(failure.HandoffNotPending, map[string]string{"handoff_id": handoffID}, nil)
	}
	if state.Phase == handoff.PhaseHumanOwned {
		return LocalAttachResult{State: state, Attachment: s.pendingAttachment(handoffID)}, nil
	}
	if state.Phase != handoff.PhaseHumanConnecting || state.AgentIngress != handoff.IngressFenced || state.HumanIngress != handoff.IngressFenced {
		return LocalAttachResult{}, failure.New(failure.HandoffNotPending, map[string]string{"handoff_id": handoffID, "phase": string(state.Phase)}, nil)
	}
	ref, err := s.store.LoadDelegatedProviderRef(ctx, operation.SessionID(state.SessionID))
	if err != nil {
		return LocalAttachResult{}, failure.Normalize(err)
	}
	attachment, client, err := s.ensureReadOnlyHuman(ctx, ref, state, spec)
	if err != nil {
		return LocalAttachResult{}, err
	}
	state, err = s.bindLocalHumanLocked(ctx, ref, state, client)
	if err != nil {
		return LocalAttachResult{}, err
	}
	return LocalAttachResult{State: state, Attachment: attachment}, nil
}

func (s *Service) ensureReadOnlyHuman(ctx context.Context, ref delegated.ProviderRef, state handoff.State, spec delegatedapp.HumanAttachSpec) (delegatedapp.HumanAttachResult, delegatedapp.ProviderClientRef, error) {
	if state.HumanClient != nil {
		client := delegatedapp.ProviderClientRef{Ref: state.HumanClient.Ref}
		obs, err := s.runtime.InspectHumanClient(ctx, ref, client)
		if err != nil {
			return delegatedapp.HumanAttachResult{}, client, failure.Normalize(err)
		}
		if !obs.Present || obs.ProviderGeneration != state.ProviderGeneration {
			return delegatedapp.HumanAttachResult{}, client, failure.New(failure.HumanClientNotProven, map[string]string{"handoff_id": state.HandoffID, "reason": "client_unproven"}, nil)
		}
		if obs.ReadOnly {
			if obs.ObservedOwner != delegated.OwnerNone {
				return delegatedapp.HumanAttachResult{}, client, failure.New(failure.HumanClientNotProven, map[string]string{"handoff_id": state.HandoffID, "reason": "read_only_owner_unproven"}, nil)
			}
			return s.pendingAttachment(state.HandoffID), client, nil
		}
		provenance, err := s.store.LoadInputAuthorityProvenance(ctx, operation.SessionID(state.SessionID))
		if err != nil {
			return delegatedapp.HumanAttachResult{}, client, failure.Normalize(err)
		}
		if obs.ObservedOwner != delegated.OwnerHuman || provenance != receipt.InputAuthorityHumanWriteGranted {
			return delegatedapp.HumanAttachResult{}, client, failure.New(failure.HumanClientNotProven, map[string]string{"handoff_id": state.HandoffID, "reason": "writable_client_without_durable_provenance"}, nil)
		}
		return s.pendingAttachment(state.HandoffID), client, nil
	}
	attachment := s.pendingAttachment(state.HandoffID)
	if attachment.ClientRef.Ref == "" {
		var err error
		attachment, err = s.runtime.AttachHuman(ctx, ref, spec)
		if err != nil {
			return LocalAttachResult{}.Attachment, delegatedapp.ProviderClientRef{}, failure.Normalize(err)
		}
		s.rememberAttachment(state.HandoffID, attachment)
	}
	client := attachment.ClientRef
	obs, err := s.runtime.InspectHumanClient(ctx, ref, client)
	if err != nil {
		return delegatedapp.HumanAttachResult{}, client, failure.Normalize(err)
	}
	if !obs.Present || !obs.ReadOnly || obs.ObservedOwner != delegated.OwnerNone || obs.ProviderGeneration != state.ProviderGeneration {
		return delegatedapp.HumanAttachResult{}, client, failure.New(failure.HumanClientNotProven, map[string]string{"handoff_id": state.HandoffID, "reason": "read_only_client_unproven"}, nil)
	}
	return attachment, client, nil
}

func (s *Service) makeHumanWritable(ctx context.Context, ref delegated.ProviderRef, client delegatedapp.ProviderClientRef, state handoff.State) (delegatedapp.HumanClientObservation, error) {
	obs, err := s.runtime.InspectHumanClient(ctx, ref, client)
	if err != nil {
		return delegatedapp.HumanClientObservation{}, failure.Normalize(err)
	}
	if obs.ReadOnly {
		if err := s.runtime.SetHumanWritable(ctx, ref, client, true); err != nil {
			return delegatedapp.HumanClientObservation{}, failure.Normalize(err)
		}
	}
	obs, err = s.runtime.InspectHumanClient(ctx, ref, client)
	if err != nil {
		return delegatedapp.HumanClientObservation{}, failure.Normalize(err)
	}
	if !obs.Present || obs.ReadOnly || obs.ObservedOwner != delegated.OwnerHuman || obs.ProviderGeneration != state.ProviderGeneration {
		return delegatedapp.HumanClientObservation{}, failure.New(failure.HumanClientNotProven, map[string]string{"handoff_id": state.HandoffID, "reason": "writable_client_unproven"}, nil)
	}
	control := delegatedapp.HumanControlSpec{HandoffID: state.HandoffID, AuthorityEpoch: state.AuthorityEpoch}
	if err := s.runtime.ArmWritableHumanControl(ctx, ref, client, control); err != nil {
		_, _ = s.runtime.FenceHumanIngress(context.Background(), ref, client, state.AuthorityEpoch)
		return delegatedapp.HumanClientObservation{}, failure.Normalize(err)
	}
	return obs, nil
}

func (s *Service) rememberAttachment(handoffID string, result delegatedapp.HumanAttachResult) {
	s.mu.Lock()
	s.attachments[handoffID] = result
	s.mu.Unlock()
}

func (s *Service) pendingAttachment(handoffID string) delegatedapp.HumanAttachResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attachments[handoffID]
}
