package interactivehandoff

import (
	"context"
	"fmt"
	"sync"
	"time"

	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	terminal "github.com/maemreyo/shellbeam/internal/core/terminalpresentation"
)

type Service struct {
	store     Store
	runtime   Runtime
	fencer    AgentIngressFencer
	presenter Presenter

	mu             sync.Mutex
	changed        chan struct{}
	attachments    map[string]delegatedapp.HumanAttachResult
	mutationShards [64]sync.Mutex
}

func New(store Store, runtime Runtime, fencer AgentIngressFencer) *Service {
	return NewWithPresenter(store, runtime, fencer, nil)
}

func NewWithPresenter(store Store, runtime Runtime, fencer AgentIngressFencer, presenter Presenter) *Service {
	return &Service{store: store, runtime: runtime, fencer: fencer, presenter: presenter, changed: make(chan struct{}), attachments: map[string]delegatedapp.HumanAttachResult{}}
}

// SetPresenter binds the optional H3 presentation service during daemon composition,
// before the coordinator is exposed to concurrent requests.
func (s *Service) SetPresenter(presenter Presenter) {
	if s != nil {
		s.presenter = presenter
	}
}

func (s *Service) Request(ctx context.Context, req handoff.Request) (handoff.State, error) {
	return s.request(ctx, req, nil)
}

func (s *Service) RequestWithPresentation(ctx context.Context, req handoff.Request, hint *terminal.BridgeAffinityHint) (handoff.State, error) {
	if hint != nil {
		if err := hint.Validate(); err != nil {
			return handoff.State{}, err
		}
	}
	return s.request(ctx, req, hint)
}

func (s *Service) request(ctx context.Context, req handoff.Request, hint *terminal.BridgeAffinityHint) (handoff.State, error) {
	if err := req.ValidateH2(); err != nil {
		return handoff.State{}, err
	}
	unlock := s.lockMutation(req.HandoffID)
	defer unlock()
	storedReq, state, found, err := s.store.FindHandoff(ctx, req.HandoffID)
	if err != nil {
		return handoff.State{}, failure.Normalize(err)
	}
	if found {
		if storedReq != req {
			return handoff.State{}, failure.New(failure.HandoffConflict, map[string]string{"handoff_id": req.HandoffID}, nil)
		}
		if state.Phase == handoff.PhaseAgentFencing {
			state, err = s.finishAgentFence(ctx, state)
			if err != nil {
				return handoff.State{}, err
			}
		}
		return s.presentAfterH2(ctx, state, hint)
	}
	binding, ref, obs, err := s.preflightAgentOwned(ctx, req.SessionID)
	if err != nil {
		return handoff.State{}, err
	}
	initial := handoff.State{
		SchemaVersion: handoff.StateSchemaVersion, HandoffID: req.HandoffID, SessionID: req.SessionID,
		Phase: handoff.PhaseAgentFencing, AuthorityEpoch: binding.AuthorityEpoch + 1,
		DesiredOwner: delegated.OwnerHuman, ProviderOwner: delegated.OwnerAgent,
		AgentIngress: handoff.IngressUnknown, HumanIngress: handoff.IngressFenced,
		TransferBoundary: handoff.TransferBoundary{Kind: handoff.BoundaryNone},
		PrivacyState:     handoff.PrivacyStateStandard, PrivacyRelease: handoff.PrivacyReleaseNotRequired, CaptureState: handoff.CapturePublic,
		ProviderGeneration: obs.ProviderGeneration,
	}
	_ = ref
	state, _, err = s.store.ReserveHandoff(ctx, req, initial)
	if err != nil {
		return handoff.State{}, failure.Normalize(err)
	}
	if state.Phase == handoff.PhaseAgentFencing {
		state, err = s.finishAgentFence(ctx, state)
		if err != nil {
			return handoff.State{}, err
		}
	}
	return s.presentAfterH2(ctx, state, hint)
}

func (s *Service) presentAfterH2(ctx context.Context, state handoff.State, hint *terminal.BridgeAffinityHint) (handoff.State, error) {
	if s.presenter == nil || state.Phase != handoff.PhaseHumanConnecting || state.HumanClient != nil {
		return state, nil
	}
	err := s.presenter.Present(ctx, PresentationRequest{HandoffID: state.HandoffID, BridgeAffinity: hint})
	if err == nil {
		return state, nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return state, ctxErr
	}
	switch failure.Public(err).Code {
	case failure.TerminalLauncherUnavailable, failure.TerminalLaunchFailed, failure.TerminalLaunchUnknown, failure.TerminalIdentityAmbiguous:
		return state, nil
	default:
		return state, failure.Normalize(err)
	}
}

func (s *Service) ExactHumanClientPresent(ctx context.Context, handoffID string) (bool, error) {
	_, state, err := s.store.LoadHandoff(ctx, handoffID)
	if err != nil {
		return false, failure.Normalize(err)
	}
	if state.HumanClient == nil {
		return false, nil
	}
	ref, err := s.store.LoadDelegatedProviderRef(ctx, operation.SessionID(state.SessionID))
	if err != nil {
		return false, failure.Normalize(err)
	}
	want := delegatedapp.ProviderClientRef{Ref: state.HumanClient.Ref}
	obs, err := s.runtime.InspectHumanClient(ctx, ref, want)
	if err != nil {
		return false, failure.Normalize(err)
	}
	if !obs.Present || obs.ClientRef.Ref != want.Ref || obs.ProviderGeneration != state.ProviderGeneration {
		return false, nil
	}
	return true, nil
}

func (s *Service) lockMutation(handoffID string) func() {
	const (
		offset64 = uint64(1469598103934665603)
		prime64  = uint64(1099511628211)
	)
	hash := offset64
	for i := 0; i < len(handoffID); i++ {
		hash ^= uint64(handoffID[i])
		hash *= prime64
	}
	shard := &s.mutationShards[hash%uint64(len(s.mutationShards))]
	shard.Lock()
	return shard.Unlock
}

func (s *Service) Inspect(ctx context.Context, handoffID string) (handoff.State, error) {
	_, state, found, err := s.store.FindHandoff(ctx, handoffID)
	if err != nil {
		return handoff.State{}, failure.Normalize(err)
	}
	if !found {
		return handoff.State{}, failure.New(failure.HandoffNotPending, map[string]string{"handoff_id": handoffID}, nil)
	}
	return state, nil
}

func (s *Service) preflightAgentOwned(ctx context.Context, sessionID string) (delegated.Binding, delegated.ProviderRef, delegatedapp.Observation, error) {
	binding, err := s.store.LoadDelegatedBinding(ctx, operation.SessionID(sessionID))
	if err != nil {
		return delegated.Binding{}, delegated.ProviderRef{}, delegatedapp.Observation{}, failure.Normalize(err)
	}
	if binding.Lifecycle != delegated.LifecycleLive || binding.DesiredOwner != delegated.OwnerAgent {
		return delegated.Binding{}, delegated.ProviderRef{}, delegatedapp.Observation{}, failure.New(failure.SessionControlNotOwned, map[string]string{"session_id": sessionID, "owner": string(binding.DesiredOwner), "required_owner": string(delegated.OwnerAgent), "current_epoch": fmt.Sprint(binding.AuthorityEpoch)}, nil)
	}
	ref, err := s.store.LoadDelegatedProviderRef(ctx, operation.SessionID(sessionID))
	if err != nil {
		return delegated.Binding{}, delegated.ProviderRef{}, delegatedapp.Observation{}, failure.Normalize(err)
	}
	obs, err := s.runtime.Inspect(ctx, ref)
	if err != nil {
		return delegated.Binding{}, delegated.ProviderRef{}, delegatedapp.Observation{}, failure.Normalize(err)
	}
	if obs.Provider != binding.ProviderIdentity() || !obs.ProviderCurrent || obs.Owner != delegated.OwnerAgent || obs.ProviderGeneration == "" {
		return delegated.Binding{}, delegated.ProviderRef{}, delegatedapp.Observation{}, failure.New(failure.SessionControlNotOwned, map[string]string{"session_id": sessionID, "owner": string(obs.Owner), "required_owner": string(delegated.OwnerAgent), "current_epoch": fmt.Sprint(binding.AuthorityEpoch)}, nil)
	}
	return binding, ref, obs, nil
}

func (s *Service) finishAgentFence(ctx context.Context, state handoff.State) (handoff.State, error) {
	proof, err := s.fencer.FenceAgentIngress(ctx, state.SessionID, state.AuthorityEpoch)
	if err != nil {
		return handoff.State{}, failure.Normalize(err)
	}
	if !proof.Fenced || proof.AuthorityEpoch != state.AuthorityEpoch || proof.ProviderGeneration == "" || proof.ProviderGeneration != state.ProviderGeneration {
		return handoff.State{}, failure.New(failure.HandoffReclaimBlocked, map[string]string{"handoff_id": state.HandoffID, "reason": "agent_ingress_fence_unproven"}, nil)
	}
	state.Phase = handoff.PhaseHumanConnecting
	state.AgentIngress = handoff.IngressFenced
	state.TransferBoundary = handoff.TransferBoundary{Kind: handoff.BoundaryProviderOrdered, Established: true}
	if err := s.advance(ctx, state); err != nil {
		return handoff.State{}, err
	}
	return state, nil
}

func (s *Service) advance(ctx context.Context, state handoff.State) error {
	if err := state.ValidateH2(); err != nil {
		return err
	}
	if err := s.store.AdvanceHandoff(ctx, state); err != nil {
		return failure.Normalize(err)
	}
	s.notify()
	return nil
}

func (s *Service) notify() {
	s.mu.Lock()
	close(s.changed)
	s.changed = make(chan struct{})
	s.mu.Unlock()
}

func (s *Service) changeChannel() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.changed
}

// ProjectPublic converts canonical authority state into the bounded public view.
// Production stores provide durable record timestamps; test-only stores may omit
// that optional metadata capability and therefore project timestamps as absent.
func (s *Service) ProjectPublic(ctx context.Context, state handoff.State) (handoff.PublicState, error) {
	var createdAt, updatedAt time.Time
	if timestamps, ok := s.store.(interface {
		LoadHandoffTimestamps(context.Context, string) (time.Time, time.Time, error)
	}); ok {
		var err error
		createdAt, updatedAt, err = timestamps.LoadHandoffTimestamps(ctx, state.HandoffID)
		if err != nil {
			return handoff.PublicState{}, failure.Normalize(err)
		}
	}
	return handoff.ProjectPublicState(state, createdAt, updatedAt)
}
