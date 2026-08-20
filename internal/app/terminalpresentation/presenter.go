package terminalpresentation

import (
	"context"
	"errors"

	handoffapp "github.com/maemreyo/shellbeam/internal/app/interactivehandoff"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/terminalpresentation"
)

type PresentationResolver interface {
	Resolve(context.Context, ResolveRequest) (ResolveResult, error)
}

type LaunchCoordinator interface {
	EnsurePresented(context.Context, string, core.Resolution, []string) (LaunchRecord, error)
}

type Presenter struct {
	resolver            PresentationResolver
	launch              LaunchCoordinator
	installedExecutable string
	fallback            *core.Evidence
}

func NewPresenter(resolver PresentationResolver, launch LaunchCoordinator, installedExecutable string, fallback *core.Evidence) *Presenter {
	return &Presenter{resolver: resolver, launch: launch, installedExecutable: installedExecutable, fallback: copyEvidence(fallback)}
}

func (p *Presenter) Present(ctx context.Context, req handoffapp.PresentationRequest) error {
	if p == nil || p.resolver == nil || p.launch == nil {
		return errors.New("terminal presenter unavailable")
	}
	if err := req.Validate(); err != nil {
		return err
	}
	resolveReq := ResolveRequest{Fallback: copyEvidence(p.fallback)}
	if req.BridgeAffinity != nil {
		evidence, err := req.BridgeAffinity.Evidence()
		if err != nil {
			return err
		}
		resolveReq.BridgeAffinity = &evidence
	}
	resolved, err := p.resolver.Resolve(ctx, resolveReq)
	if err != nil {
		return err
	}
	if resolved.Resolution.Selected == nil {
		return failure.New(failure.TerminalLauncherUnavailable, map[string]string{"reason": "no_terminal_selected"}, nil)
	}
	argv, err := BuildAttachArgv(p.installedExecutable, req.HandoffID)
	if err != nil {
		return err
	}
	_, err = p.launch.EnsurePresented(ctx, req.HandoffID, resolved.Resolution, argv)
	return err
}

func copyEvidence(in *core.Evidence) *core.Evidence {
	if in == nil {
		return nil
	}
	copy := *in
	return &copy
}
