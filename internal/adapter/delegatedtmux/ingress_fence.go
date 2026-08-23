package delegatedtmux

import (
	"context"

	app "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	core "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
)

func (p *Provider) SetHumanWritable(ctx context.Context, ref core.ProviderRef, client app.ProviderClientRef, writable bool) error {
	state, err := p.humanProviderState(ctx, ref)
	if err != nil {
		return err
	}
	clientState, err := p.loadHumanClientState(state, client)
	if err != nil {
		return humanClientStateFailure(err)
	}
	facts, err := p.exactHumanClient(ctx, state, clientState)
	if err != nil {
		return err
	}
	wantReadOnly := !writable
	if facts.ReadOnly != wantReadOnly {
		if _, err := p.externalTmux(ctx, state, "switch-client", "-E", "-c", clientState.ClientName, "-r"); err != nil {
			return failure.New(failure.HandoffClientLost, map[string]string{"reason": "client_toggle"}, err)
		}
	}
	observed, err := p.exactHumanClient(ctx, state, clientState)
	if err != nil {
		return err
	}
	if observed.ReadOnly != wantReadOnly {
		return failure.New(failure.HumanClientNotProven, map[string]string{"reason": "client_toggle_unproven"}, nil)
	}
	return nil
}

func (p *Provider) FenceHumanIngress(ctx context.Context, ref core.ProviderRef, client app.ProviderClientRef, epoch core.AuthorityEpoch) (app.IngressFenceProof, error) {
	if err := epoch.Validate(); err != nil {
		return app.IngressFenceProof{}, failure.New(failure.InvalidInput, map[string]string{"field": "authority_epoch"}, err)
	}
	if err := p.SetHumanWritable(ctx, ref, client, false); err != nil {
		return app.IngressFenceProof{}, err
	}
	obs, err := p.InspectHumanClient(ctx, ref, client)
	if err != nil {
		return app.IngressFenceProof{}, err
	}
	if !obs.Present || !obs.ReadOnly || obs.ProviderGeneration == "" {
		return app.IngressFenceProof{}, failure.New(failure.HumanClientNotProven, map[string]string{"reason": "ingress_fence_unproven"}, nil)
	}
	return app.IngressFenceProof{ClientRef: client, AuthorityEpoch: epoch, ProviderGeneration: obs.ProviderGeneration, Fenced: true}, nil
}

func humanClientStateFailure(err error) error {
	return failure.New(failure.HumanClientNotProven, map[string]string{"reason": "client_state_invalid"}, err)
}
