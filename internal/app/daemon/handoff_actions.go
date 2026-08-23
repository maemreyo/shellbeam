package daemon

import (
	"context"

	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	handoffapp "github.com/maemreyo/shellbeam/internal/app/interactivehandoff"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
	terminalpresentation "github.com/maemreyo/shellbeam/internal/core/terminalpresentation"
)

func (s *Service) RequestHandoff(ctx context.Context, req handoff.Request) (handoff.State, error) {
	if s == nil || s.handoff == nil {
		return handoff.State{}, handoffUnavailable()
	}
	return s.handoff.Request(ctx, req)
}

func (s *Service) RequestHandoffWithPresentation(ctx context.Context, req handoff.Request, hint *terminalpresentation.BridgeAffinityHint) (handoff.State, error) {
	if s == nil || s.handoff == nil {
		return handoff.State{}, handoffUnavailable()
	}
	presentation, ok := s.handoff.(handoffPresentationCoordinator)
	if !ok {
		return s.handoff.Request(ctx, req)
	}
	return presentation.RequestWithPresentation(ctx, req, hint)
}

func (s *Service) WaitHandoff(ctx context.Context, req handoffapp.WaitRequest) (handoffapp.WaitResult, error) {
	if s == nil || s.handoff == nil {
		return handoffapp.WaitResult{}, handoffUnavailable()
	}
	return s.handoff.Wait(ctx, req)
}

func (s *Service) AbortHandoff(ctx context.Context, id string) (handoff.State, error) {
	if s == nil || s.handoff == nil {
		return handoff.State{}, handoffUnavailable()
	}
	return s.handoff.Abort(ctx, id)
}

func (s *Service) InspectHandoff(ctx context.Context, id string) (handoff.State, error) {
	if s == nil || s.handoff == nil {
		return handoff.State{}, handoffUnavailable()
	}
	return s.handoff.Inspect(ctx, id)
}

func (s *Service) ExpireHandoff(ctx context.Context, id string) (handoff.State, error) {
	if s == nil || s.handoff == nil {
		return handoff.State{}, handoffUnavailable()
	}
	return s.handoff.Expire(ctx, id)
}

func (s *Service) BootstrapLocalHuman(ctx context.Context, id string) (handoffapp.LocalBootstrap, error) {
	if s == nil || s.handoff == nil {
		return handoffapp.LocalBootstrap{}, handoffUnavailable()
	}
	return s.handoff.BootstrapLocalHuman(ctx, id)
}

func (s *Service) BindLocalHuman(ctx context.Context, id string, client delegatedapp.ProviderClientRef) (handoff.State, error) {
	if s == nil || s.handoff == nil {
		return handoff.State{}, handoffUnavailable()
	}
	return s.handoff.BindLocalHuman(ctx, id, client)
}

func (s *Service) AttachLocalHuman(ctx context.Context, id string, spec delegatedapp.HumanAttachSpec) (handoffapp.LocalAttachResult, error) {
	if s == nil || s.handoff == nil {
		return handoffapp.LocalAttachResult{}, handoffUnavailable()
	}
	return s.handoff.AttachLocalHuman(ctx, id, spec)
}

func (s *Service) HandoffHumanControl(ctx context.Context, signal handoff.ControlSignal) (handoffapp.ControlResult, error) {
	if s == nil || s.handoff == nil {
		return handoffapp.ControlResult{}, handoffUnavailable()
	}
	return s.handoff.HumanControl(ctx, signal)
}

func handoffUnavailable() error {
	return failure.New(failure.FeatureUnavailable, map[string]string{"feature": "interactive_handoff"}, nil)
}

func (s *Service) RequestHandoffPublic(ctx context.Context, req handoff.Request) (handoff.PublicState, error) {
	state, err := s.RequestHandoff(ctx, req)
	if err != nil {
		return handoff.PublicState{}, err
	}
	return s.handoff.ProjectPublic(ctx, state)
}

func (s *Service) RequestHandoffPublicWithPresentation(ctx context.Context, req handoff.Request, hint *terminalpresentation.BridgeAffinityHint) (handoff.PublicState, error) {
	state, err := s.RequestHandoffWithPresentation(ctx, req, hint)
	if err != nil {
		return handoff.PublicState{}, err
	}
	return s.handoff.ProjectPublic(ctx, state)
}

func (s *Service) WaitHandoffPublic(ctx context.Context, req handoffapp.WaitRequest) (handoff.PublicState, bool, error) {
	result, err := s.WaitHandoff(ctx, req)
	if err != nil {
		return handoff.PublicState{}, false, err
	}
	state, err := s.handoff.ProjectPublic(ctx, result.State)
	return state, result.TimedOut, err
}

func (s *Service) AbortHandoffPublic(ctx context.Context, id string) (handoff.PublicState, error) {
	state, err := s.AbortHandoff(ctx, id)
	if err != nil {
		return handoff.PublicState{}, err
	}
	return s.handoff.ProjectPublic(ctx, state)
}

func (s *Service) InspectHandoffPublic(ctx context.Context, id string) (handoff.PublicState, error) {
	state, err := s.InspectHandoff(ctx, id)
	if err != nil {
		return handoff.PublicState{}, err
	}
	return s.handoff.ProjectPublic(ctx, state)
}
