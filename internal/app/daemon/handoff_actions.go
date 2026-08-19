package daemon

import (
	"context"

	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	handoffapp "github.com/maemreyo/shellbeam/internal/app/interactivehandoff"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
)

func (s *Service) RequestHandoff(ctx context.Context, req handoff.Request) (handoff.State, error) {
	if s == nil || s.handoff == nil {
		return handoff.State{}, handoffUnavailable()
	}
	return s.handoff.Request(ctx, req)
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
