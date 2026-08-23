package interactivehandoff

import (
	"context"
	"fmt"

	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

type privacyRuntime interface {
	ArmPrivateObservation(context.Context, delegated.ProviderRef, delegatedapp.PrivacySpec) (delegatedapp.PrivacyHandle, error)
	ProvePrivateObservation(context.Context, delegated.ProviderRef, delegatedapp.PrivacyHandle) (delegatedapp.PrivateObservationProof, error)
	ReleasePrivateObservation(context.Context, delegated.ProviderRef, delegatedapp.PrivacyHandle, delegatedapp.ForwardBoundary) error
}

type privateCaptureMarker interface {
	MarkPrivateCapture(context.Context, string) error
}

type privateLease struct {
	spec   delegatedapp.PrivacySpec
	handle delegatedapp.PrivacyHandle
	proof  delegatedapp.PrivateObservationProof
}

func (s *Service) prepareH4(ctx context.Context, req handoff.Request, state handoff.State) (handoff.State, error) {
	if req.Privacy != handoff.PrivacySecret && req.Completion.Kind == handoff.CompletionManualReady {
		return state, nil
	}
	ref, err := s.store.LoadDelegatedProviderRef(ctx, operation.SessionID(state.SessionID))
	if err != nil {
		return handoff.State{}, failure.Normalize(err)
	}
	if err := s.prepareReadiness(ctx, req, state, ref); err != nil {
		return handoff.State{}, err
	}
	if req.Privacy != handoff.PrivacySecret {
		return state, nil
	}
	return s.preparePrivate(ctx, req, state, ref)
}

func (s *Service) preparePrivate(ctx context.Context, req handoff.Request, state handoff.State, ref delegated.ProviderRef) (handoff.State, error) {
	runtime, ok := s.runtime.(privacyRuntime)
	if !ok {
		return handoff.State{}, failure.New(failure.PrivateOutputBarrierFailed, map[string]string{"handoff_id": state.HandoffID, "reason": "provider_unconfigured"}, nil)
	}
	spec := delegatedapp.PrivacySpec{HandoffID: state.HandoffID, AuthorityEpoch: state.AuthorityEpoch}
	handle, err := runtime.ArmPrivateObservation(ctx, ref, spec)
	if err != nil {
		return handoff.State{}, normalizePrivateBarrierError(state.HandoffID, "arm", err)
	}
	proof, err := runtime.ProvePrivateObservation(ctx, ref, handle)
	if err != nil {
		return handoff.State{}, normalizePrivateBarrierError(state.HandoffID, "prove", err)
	}
	if err := proof.Validate(); err != nil || proof.Handle != handle || proof.ProviderGeneration != state.ProviderGeneration || !proof.PrivateFromFirstByte {
		return handoff.State{}, failure.New(failure.PrivateOutputBarrierFailed, map[string]string{"handoff_id": state.HandoffID, "reason": "proof_unproven"}, err)
	}
	prep := s.ensurePreparation(state.HandoffID, state.AuthorityEpoch)
	s.mu.Lock()
	if current := s.preparations[state.HandoffID]; current == prep {
		current.privacy = &privateLease{spec: spec, handle: handle, proof: proof}
	}
	s.mu.Unlock()
	if state.PrivacyState != handoff.PrivacyPrivate || state.PrivacyRelease != handoff.PrivacyReleasePending || state.CaptureState != handoff.CapturePrivate {
		state.PrivacyState = handoff.PrivacyPrivate
		state.PrivacyRelease = handoff.PrivacyReleasePending
		state.CaptureState = handoff.CapturePrivate
		if err := s.advance(ctx, state); err != nil {
			return handoff.State{}, err
		}
	}
	marker, ok := s.store.(privateCaptureMarker)
	if !ok {
		return handoff.State{}, failure.New(failure.PrivateOutputBarrierFailed, map[string]string{"handoff_id": state.HandoffID, "reason": "capture_marker_unavailable"}, nil)
	}
	if err := marker.MarkPrivateCapture(ctx, state.SessionID); err != nil {
		return handoff.State{}, failure.New(failure.PrivateOutputBarrierFailed, map[string]string{"handoff_id": state.HandoffID, "reason": "capture_marker_failed"}, err)
	}
	_ = req
	return state, nil
}

func (s *Service) ensurePrivateCurrent(ctx context.Context, req handoff.Request, state handoff.State) (handoff.State, error) {
	if req.Privacy != handoff.PrivacySecret {
		return state, nil
	}
	prep := s.ensurePreparation(state.HandoffID, state.AuthorityEpoch)
	s.mu.Lock()
	lease := prep.privacy
	s.mu.Unlock()
	if lease == nil || lease.spec.AuthorityEpoch != state.AuthorityEpoch {
		return s.prepareH4(ctx, req, state)
	}
	runtime, ok := s.runtime.(privacyRuntime)
	if !ok {
		return handoff.State{}, failure.New(failure.PrivateOutputBarrierFailed, map[string]string{"handoff_id": state.HandoffID, "reason": "provider_unconfigured"}, nil)
	}
	ref, err := s.store.LoadDelegatedProviderRef(ctx, operation.SessionID(state.SessionID))
	if err != nil {
		return handoff.State{}, failure.Normalize(err)
	}
	proof, err := runtime.ProvePrivateObservation(ctx, ref, lease.handle)
	if err != nil {
		return handoff.State{}, normalizePrivateBarrierError(state.HandoffID, "reprove", err)
	}
	if err := proof.Validate(); err != nil || proof.Handle != lease.handle || proof.ProviderGeneration != state.ProviderGeneration || !proof.PrivateFromFirstByte {
		return handoff.State{}, failure.New(failure.PrivateOutputBarrierFailed, map[string]string{"handoff_id": state.HandoffID, "reason": "reproof_unproven"}, err)
	}
	s.mu.Lock()
	if current := s.preparations[state.HandoffID]; current != nil && current.epoch == state.AuthorityEpoch && current.privacy != nil {
		current.privacy.proof = proof
	}
	s.mu.Unlock()
	return state, nil
}

func (s *Service) privateLeaseFor(handoffID string, epoch delegated.AuthorityEpoch) (*privateLease, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	prep := s.preparations[handoffID]
	if prep == nil || prep.epoch != epoch || prep.privacy == nil {
		return nil, false
	}
	copy := *prep.privacy
	return &copy, true
}

func normalizePrivateBarrierError(handoffID, reason string, err error) error {
	if err == nil {
		return nil
	}
	if failure.Public(err).Code == failure.PrivateOutputBarrierFailed {
		return failure.Normalize(err)
	}
	return failure.New(failure.PrivateOutputBarrierFailed, map[string]string{"handoff_id": handoffID, "reason": reason}, err)
}

func newShellIntegrationUnavailable(err error) error {
	if failure.Public(err).Code == failure.ShellIntegrationUnavailable || failure.Public(err).Code == failure.ShellIdentityChanged || failure.Public(err).Code == failure.RequirementUnsupported {
		return failure.Normalize(err)
	}
	return failure.New(failure.ShellIntegrationUnavailable, map[string]string{"reason": "preparation_failed"}, err)
}

var _ = fmt.Sprintf
