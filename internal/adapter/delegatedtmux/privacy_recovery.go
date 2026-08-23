package delegatedtmux

import (
	"context"
	"errors"
	"os"

	app "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	core "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
)

func (p *Provider) activePrivacy(ref core.ProviderRef, state privateState) (privacyState, bool, error) {
	privacy, err := p.privacy.load(ref.Ref)
	if errors.Is(err, os.ErrNotExist) {
		return privacyState{}, false, nil
	}
	if err != nil {
		return privacyState{}, false, privacyBarrierFailure("", "privacy_state_invalid", err)
	}
	if privacy.SessionID != ref.SessionID || privacy.ProviderGeneration != state.ProviderGeneration {
		return privacyState{}, false, privacyBarrierFailure(privacy.HandoffID, "privacy_provider_mismatch", nil)
	}
	return privacy, privacy.Active, nil
}

func (p *Provider) ensureCurrentObserverPrivate(ctx context.Context, ref core.ProviderRef, state privateState) error {
	p.mu.Lock()
	control := p.controls[ref.Ref]
	p.mu.Unlock()
	if control != nil && control.isPrivateObservation() {
		facts, err := p.queryFacts(ctx, control, state.TmuxSession)
		if err == nil && p.verifyFacts(ctx, control, state, facts) == nil {
			return nil
		}
	}
	var sink app.OutputSink
	if control != nil {
		_, sink = control.targetSnapshot()
	}
	if sink == nil {
		return privacyBarrierFailure("", "observer_target_missing", nil)
	}
	return p.replaceWithPrivateObserver(ctx, ref, state, control, sink)
}

func (p *Provider) replaceWithPrivateObserver(ctx context.Context, ref core.ProviderRef, state privateState, old *controlClient, sink app.OutputSink) error {
	privateControl, err := p.startPrivateControl(ctx, state.SocketPath, state.TmuxSession)
	if err != nil {
		return privacyBarrierFailure("", "private_reattach", err)
	}
	keep := false
	defer func() {
		if !keep {
			_ = privateControl.close()
		}
	}()
	facts, err := p.queryFacts(ctx, privateControl, state.TmuxSession)
	if err != nil {
		return privacyBarrierFailure("", "private_reattach_proof", err)
	}
	if err := p.verifyFacts(ctx, privateControl, state, facts); err != nil {
		return err
	}
	privateControl.shareOutputCounter(old)
	if err := privateControl.setTarget(state.PaneID, sink); err != nil {
		return privacyBarrierFailure("", "private_target", err)
	}
	p.mu.Lock()
	current := p.controls[ref.Ref]
	if old != nil && current != old {
		p.mu.Unlock()
		return privacyBarrierFailure("", "observer_changed", nil)
	}
	if old == nil && current != nil {
		p.mu.Unlock()
		return privacyBarrierFailure("", "observer_changed", nil)
	}
	p.controls[ref.Ref] = privateControl
	p.mu.Unlock()
	keep = true
	if old != nil {
		_ = old.close()
	}
	return nil
}
