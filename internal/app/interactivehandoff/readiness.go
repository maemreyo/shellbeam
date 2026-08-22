package interactivehandoff

import (
	"context"
	"fmt"
	"os"

	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	shellapp "github.com/maemreyo/shellbeam/internal/app/shellintegration"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	shellcore "github.com/maemreyo/shellbeam/internal/core/shellintegration"
)

type ReadinessRequest struct {
	HandoffID          string
	SessionID          string
	AuthorityEpoch     delegated.AuthorityEpoch
	ProviderRef        delegated.ProviderRef
	ProviderGeneration string
	Requirement        shellcore.Requirement
}

func (v ReadinessRequest) Validate() error {
	if err := handoff.ValidateHandoffID(v.HandoffID); err != nil {
		return err
	}
	if v.SessionID == "" || v.ProviderGeneration == "" {
		return fmt.Errorf("shell readiness provider identity missing")
	}
	if err := v.AuthorityEpoch.Validate(); err != nil {
		return err
	}
	if err := v.ProviderRef.Validate(); err != nil {
		return err
	}
	return v.Requirement.Validate()
}

type PreparedReadiness struct {
	Shell   shellcore.ShellIdentity
	Watcher shellapp.RequirementWatcher
}

func (v PreparedReadiness) Validate() error {
	if err := v.Shell.Validate(); err != nil {
		return err
	}
	if v.Shell.Family == shellcore.ShellUnknown || v.Watcher == nil {
		return fmt.Errorf("exact shell readiness unavailable")
	}
	return nil
}

type ReadinessPreparer interface {
	Prepare(context.Context, ReadinessRequest) (PreparedReadiness, error)
}

type handoffPreparation struct {
	epoch     delegated.AuthorityEpoch
	readiness *PreparedReadiness
	privacy   *privateLease
	watching  bool
	cancel    context.CancelFunc
}

func (s *Service) EnableH4() {
	if s != nil {
		s.h4Enabled = true
	}
}

func (s *Service) SetReadiness(readiness ReadinessPreparer) {
	if s != nil {
		s.readiness = readiness
	}
}

func requirementFor(req handoff.Request) (shellcore.Requirement, bool) {
	if req.Completion.Kind != handoff.CompletionEnvironmentExportedNonempty {
		return shellcore.Requirement{}, false
	}
	return shellcore.Requirement{Kind: shellcore.RequirementEnvironmentExportedNonempty, Name: req.Completion.Name}, true
}

func (s *Service) prepareReadiness(ctx context.Context, req handoff.Request, state handoff.State, ref delegated.ProviderRef) error {
	requirement, automatic := requirementFor(req)
	if !automatic {
		return nil
	}
	if s.readiness == nil {
		return failure.New(failure.ShellIntegrationUnavailable, map[string]string{"reason": "readiness_unconfigured"}, nil)
	}
	prep := s.ensurePreparation(state.HandoffID, state.AuthorityEpoch)
	if prep.readiness != nil {
		return nil
	}
	prepared, err := s.readiness.Prepare(ctx, ReadinessRequest{
		HandoffID: req.HandoffID, SessionID: req.SessionID, AuthorityEpoch: state.AuthorityEpoch,
		ProviderRef: ref, ProviderGeneration: state.ProviderGeneration, Requirement: requirement,
	})
	if err != nil {
		return normalizeShellIntegrationError(err)
	}
	if err := prepared.Validate(); err != nil {
		_ = prepared.Watcher.Close()
		return normalizeShellIntegrationError(err)
	}
	s.mu.Lock()
	current := s.preparations[state.HandoffID]
	if current == nil || current.epoch != state.AuthorityEpoch {
		s.mu.Unlock()
		_ = prepared.Watcher.Close()
		return normalizeShellIntegrationError(fmt.Errorf("handoff generation changed during readiness preparation"))
	}
	current.readiness = &prepared
	s.mu.Unlock()
	return nil
}

func (s *Service) ensurePreparation(handoffID string, epoch delegated.AuthorityEpoch) *handoffPreparation {
	var old *handoffPreparation
	s.mu.Lock()
	prep := s.preparations[handoffID]
	if prep == nil || prep.epoch != epoch {
		old = prep
		prep = &handoffPreparation{epoch: epoch}
		s.preparations[handoffID] = prep
	}
	s.mu.Unlock()
	if old != nil {
		if old.cancel != nil {
			old.cancel()
		}
		if old.readiness != nil && old.readiness.Watcher != nil {
			_ = old.readiness.Watcher.Close()
		}
	}
	return prep
}

func (s *Service) preparedReadiness(handoffID string, epoch delegated.AuthorityEpoch) (PreparedReadiness, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	prep := s.preparations[handoffID]
	if prep == nil || prep.epoch != epoch || prep.readiness == nil {
		return PreparedReadiness{}, false
	}
	return *prep.readiness, true
}

func (s *Service) cancelReadiness(handoffID string) {
	var watcher shellapp.RequirementWatcher
	var cancel context.CancelFunc
	s.mu.Lock()
	if prep := s.preparations[handoffID]; prep != nil {
		cancel = prep.cancel
		prep.cancel = nil
		prep.watching = false
		if prep.readiness != nil {
			watcher = prep.readiness.Watcher
			prep.readiness = nil
		}
	}
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if watcher != nil {
		_ = watcher.Close()
	}
}

func normalizeShellIntegrationError(err error) error {
	if err == nil {
		return nil
	}
	return newShellIntegrationUnavailable(err)
}

func (s *Service) startAutomaticReadiness(req handoff.Request, state handoff.State) {
	if _, automatic := requirementFor(req); !automatic {
		return
	}
	prepared, ok := s.preparedReadiness(state.HandoffID, state.AuthorityEpoch)
	if !ok {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	prep := s.preparations[state.HandoffID]
	if prep == nil || prep.epoch != state.AuthorityEpoch || prep.readiness == nil || prep.watching {
		s.mu.Unlock()
		cancel()
		return
	}
	prep.watching = true
	prep.cancel = cancel
	s.mu.Unlock()
	go s.runAutomaticReadiness(ctx, req, state.AuthorityEpoch, prepared)
}

func (s *Service) runAutomaticReadiness(ctx context.Context, req handoff.Request, epoch delegated.AuthorityEpoch, prepared PreparedReadiness) {
	defer s.finishReadinessWatcher(req.HandoffID, epoch, prepared.Watcher)
	event, err := prepared.Watcher.Wait(ctx)
	if err != nil || ctx.Err() != nil {
		fmt.Fprintf(os.Stderr, "SHELLBEAM_H5_READINESS_DIAG stage=wait canceled=%t\n", ctx.Err() != nil)
		return
	}
	watchReq := shellapp.WatchRequest{HandoffID: req.HandoffID, AuthorityEpoch: epoch, Shell: prepared.Shell, Requirement: event.Result.Requirement}
	if err := event.Validate(watchReq); err != nil {
		public := failure.Public(err)
		fmt.Fprintf(os.Stderr, "SHELLBEAM_H5_READINESS_DIAG stage=event_validate code=%s reason=%s\n", public.Code, public.Details["reason"])
		return
	}
	if event.Result.State != shellcore.RequirementSatisfied || !event.Result.SafeBoundary {
		fmt.Fprintf(os.Stderr, "SHELLBEAM_H5_READINESS_DIAG stage=event_unsatisfied state=%s safe=%t\n", event.Result.State, event.Result.SafeBoundary)
		return
	}
	if err := s.automaticReady(context.Background(), req, epoch, event); err != nil {
		public := failure.Public(err)
		fmt.Fprintf(os.Stderr, "SHELLBEAM_H5_READINESS_DIAG stage=automatic_ready code=%s reason=%s\n", public.Code, public.Details["reason"])
	}
}

func (s *Service) finishReadinessWatcher(handoffID string, epoch delegated.AuthorityEpoch, watcher shellapp.RequirementWatcher) {
	s.mu.Lock()
	if prep := s.preparations[handoffID]; prep != nil && prep.epoch == epoch {
		prep.watching = false
		prep.cancel = nil
		if prep.readiness != nil && prep.readiness.Watcher == watcher {
			prep.readiness = nil
		}
	}
	s.mu.Unlock()
	_ = watcher.Close()
}

func (s *Service) automaticReady(ctx context.Context, req handoff.Request, epoch delegated.AuthorityEpoch, event shellapp.WatchEvent) error {
	unlock := s.lockMutation(req.HandoffID)
	defer unlock()
	storedReq, state, err := s.store.LoadHandoff(ctx, req.HandoffID)
	if err != nil {
		return failure.Normalize(err)
	}
	if storedReq != req || state.AuthorityEpoch != epoch || state.Phase != handoff.PhaseHumanOwned || state.HumanClient == nil {
		return failure.New(failure.HandoffReclaimBlocked, map[string]string{"handoff_id": req.HandoffID, "reason": "automatic_readiness_stale"}, nil)
	}
	prepared, ok := s.preparedReadiness(req.HandoffID, epoch)
	if !ok {
		return failure.New(failure.ShellIntegrationLost, map[string]string{"handoff_id": req.HandoffID, "reason": "watcher_missing"}, nil)
	}
	watchReq := shellapp.WatchRequest{HandoffID: req.HandoffID, AuthorityEpoch: epoch, Shell: prepared.Shell, Requirement: event.Result.Requirement}
	if err := event.Validate(watchReq); err != nil || event.Result.State != shellcore.RequirementSatisfied || !event.Result.SafeBoundary {
		return failure.New(failure.RequirementNotSatisfied, map[string]string{"handoff_id": req.HandoffID}, err)
	}
	state.Phase = handoff.PhaseHumanFencing
	state.TransferBoundary = handoff.TransferBoundary{Kind: boundaryKind(event.Boundary.Quality), Established: true}
	if state.TransferBoundary.Kind == handoff.BoundaryNone {
		return failure.New(failure.HandoffReclaimBlocked, map[string]string{"handoff_id": req.HandoffID, "reason": "boundary_unqualified"}, nil)
	}
	if err := s.advance(ctx, state); err != nil {
		return err
	}
	state, ref, privateEpoch, err := s.reclaimAutomaticAgent(ctx, state)
	if err != nil {
		return err
	}
	return s.releaseAutomaticPrivacy(ctx, req, state, ref, privateEpoch, event)
}

func (s *Service) reclaimAutomaticAgent(ctx context.Context, state handoff.State) (handoff.State, delegated.ProviderRef, delegated.AuthorityEpoch, error) {
	ref, err := s.store.LoadDelegatedProviderRef(ctx, operation.SessionID(state.SessionID))
	if err != nil {
		return handoff.State{}, delegated.ProviderRef{}, 0, failure.Normalize(err)
	}
	client := delegatedapp.ProviderClientRef{Ref: state.HumanClient.Ref}
	proof, err := s.runtime.FenceHumanIngress(ctx, ref, client, state.AuthorityEpoch)
	if err != nil {
		return handoff.State{}, delegated.ProviderRef{}, 0, failure.Normalize(err)
	}
	if !proof.Fenced || proof.AuthorityEpoch != state.AuthorityEpoch || proof.ProviderGeneration != state.ProviderGeneration {
		return handoff.State{}, delegated.ProviderRef{}, 0, failure.New(failure.HandoffReclaimBlocked, map[string]string{"handoff_id": state.HandoffID, "reason": "human_fence_unproven"}, nil)
	}
	state.HumanIngress = handoff.IngressFenced
	state.ProviderOwner = delegated.OwnerNone
	if err := s.advance(ctx, state); err != nil {
		return handoff.State{}, delegated.ProviderRef{}, 0, err
	}
	if err := s.runtime.PrepareReadOnlyLocalControl(ctx, ref, client); err != nil {
		return handoff.State{}, delegated.ProviderRef{}, 0, failure.Normalize(err)
	}
	obs, err := s.runtime.Inspect(ctx, ref)
	if err != nil {
		return handoff.State{}, delegated.ProviderRef{}, 0, failure.Normalize(err)
	}
	binding, err := s.store.LoadDelegatedBinding(ctx, operation.SessionID(state.SessionID))
	if err != nil {
		return handoff.State{}, delegated.ProviderRef{}, 0, failure.Normalize(err)
	}
	if obs.Provider != binding.ProviderIdentity() || binding.AuthorityEpoch != state.AuthorityEpoch || binding.DesiredOwner != delegated.OwnerHuman || !obs.ProviderCurrent || obs.ProviderGeneration != state.ProviderGeneration || obs.Owner != delegated.OwnerAgent {
		return handoff.State{}, delegated.ProviderRef{}, 0, failure.New(failure.HandoffReclaimBlocked, map[string]string{"handoff_id": state.HandoffID, "reason": "agent_provider_unproven"}, nil)
	}
	privateEpoch := state.AuthorityEpoch
	state.AuthorityEpoch++
	state.DesiredOwner = delegated.OwnerAgent
	state.ProviderOwner = delegated.OwnerAgent
	state.AgentIngress = handoff.IngressWritable
	state.HumanIngress = handoff.IngressFenced
	state.Phase = handoff.PhaseAgentOwned
	if err := s.advance(ctx, state); err != nil {
		return handoff.State{}, delegated.ProviderRef{}, 0, err
	}
	return state, ref, privateEpoch, nil
}

func (s *Service) releaseAutomaticPrivacy(ctx context.Context, req handoff.Request, state handoff.State, ref delegated.ProviderRef, privateEpoch delegated.AuthorityEpoch, event shellapp.WatchEvent) error {
	if req.Privacy != handoff.PrivacySecret {
		return nil
	}
	lease, ok := s.privateLeaseFor(req.HandoffID, privateEpoch)
	if !ok {
		return failure.New(failure.PrivacyReleaseUnproven, map[string]string{"handoff_id": req.HandoffID, "reason": "private_lease_missing"}, nil)
	}
	privacyProof := shellcore.PrivacyReleaseProof{
		HandoffID: req.HandoffID, AuthorityEpoch: privateEpoch, Shell: event.Boundary.Shell,
		Boundary: string(event.Boundary.Quality), ForwardOnly: true, ObservedAt: event.Boundary.ObservedAt,
	}
	if err := privacyProof.Validate(); err != nil {
		return failure.New(failure.PrivacyReleaseUnproven, map[string]string{"handoff_id": req.HandoffID, "reason": "release_proof_invalid"}, err)
	}
	privacy, ok := s.runtime.(privacyRuntime)
	if !ok {
		return failure.New(failure.PrivacyReleaseUnproven, map[string]string{"handoff_id": req.HandoffID, "reason": "provider_unconfigured"}, nil)
	}
	boundary := delegatedapp.ForwardBoundary{Proof: privacyProof}
	if err := privacy.ReleasePrivateObservation(ctx, ref, lease.handle, boundary); err != nil {
		return failure.New(failure.PrivacyReleaseUnproven, map[string]string{"handoff_id": req.HandoffID, "reason": "provider_release_failed"}, err)
	}
	state.PrivacyRelease = handoff.PrivacyReleaseProven
	state.CaptureState = handoff.CapturePublic
	return s.advance(ctx, state)
}

func boundaryKind(q shellcore.BoundaryQuality) handoff.BoundaryKind {
	switch q {
	case shellcore.BoundaryQualityShellPrompt:
		return handoff.BoundaryShell
	case shellcore.BoundaryQualityProcessBoundary:
		return handoff.BoundaryProcess
	case shellcore.BoundaryQualityHumanAttested:
		return handoff.BoundaryHumanAttested
	default:
		return handoff.BoundaryNone
	}
}
