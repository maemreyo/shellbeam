package delegatedsession

import (
	"context"
	"fmt"

	core "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
)

type ReattachRequest struct {
	Binding     core.Binding
	ProviderRef core.ProviderRef
	Output      OutputSink
}

type ReattachResult struct {
	Observation Observation
	Authority   core.EffectiveAuthority
}

func (s *Service) ReattachBound(ctx context.Context, req ReattachRequest) (ReattachResult, error) {
	if s == nil || s.provider == nil {
		return ReattachResult{}, delegatedUnavailable()
	}
	if err := req.Binding.Validate(); err != nil {
		return ReattachResult{}, failure.New(failure.DelegatedReconcileBlocked, map[string]string{"session_id": req.Binding.SessionID, "reason": "binding_invalid"}, err)
	}
	if err := req.ProviderRef.Validate(); err != nil {
		return ReattachResult{}, failure.New(failure.DelegatedProviderMismatch, map[string]string{"session_id": req.Binding.SessionID, "reason": "provider_ref_invalid"}, err)
	}
	if req.Binding.Lifecycle != core.LifecycleProvisioning && req.Binding.Lifecycle != core.LifecycleLive {
		return ReattachResult{}, failure.New(failure.DelegatedReconcileBlocked, map[string]string{"session_id": req.Binding.SessionID, "reason": "binding_not_recoverable"}, nil)
	}
	identity := req.Binding.ProviderIdentity()
	if req.ProviderRef.SessionID != req.Binding.SessionID || req.ProviderRef.ProviderID != identity.ID || req.ProviderRef.ProviderVersion != identity.Version || !req.ProviderRef.CreatedAt.Equal(req.Binding.CreatedAt) {
		return ReattachResult{}, failure.New(failure.DelegatedProviderMismatch, map[string]string{"session_id": req.Binding.SessionID, "provider_id": req.ProviderRef.ProviderID, "reason": "provider_ref_binding"}, nil)
	}
	if got := s.provider.Identity(); got != identity {
		return ReattachResult{}, failure.New(failure.DelegatedProviderMismatch, map[string]string{"session_id": req.Binding.SessionID, "provider_id": got.ID, "provider_version": fmt.Sprint(got.Version), "expected_provider_id": identity.ID, "expected_provider_version": fmt.Sprint(identity.Version)}, nil)
	}
	obs, err := s.provider.Reattach(ctx, req.ProviderRef, req.Output)
	if err != nil {
		return ReattachResult{}, err
	}
	if obs.Provider != identity {
		return ReattachResult{}, failure.New(failure.DelegatedProviderMismatch, map[string]string{"session_id": req.Binding.SessionID, "provider_id": obs.Provider.ID, "provider_version": fmt.Sprint(obs.Provider.Version), "expected_provider_id": identity.ID, "expected_provider_version": fmt.Sprint(identity.Version)}, nil)
	}
	if !obs.ProviderCurrent || obs.ProviderGeneration == "" {
		return ReattachResult{}, failure.New(failure.DelegatedReconcileBlocked, map[string]string{"session_id": req.Binding.SessionID, "provider_id": identity.ID, "current_epoch": fmt.Sprint(req.Binding.AuthorityEpoch), "reason": "provider_current_unproven"}, nil)
	}
	authority := core.ReconcileAuthority(core.ReconcileInput{Epoch: req.Binding.AuthorityEpoch, DesiredOwner: req.Binding.DesiredOwner, ObservedOwner: obs.Owner, ProviderIdentity: obs.Provider, ProviderCurrent: obs.ProviderCurrent})
	if obs.Terminal {
		if obs.Owner != core.OwnerNone {
			return ReattachResult{}, failure.New(failure.DelegatedReconcileBlocked, map[string]string{"session_id": req.Binding.SessionID, "provider_id": identity.ID, "current_epoch": fmt.Sprint(req.Binding.AuthorityEpoch), "reason": "terminal_owner"}, nil)
		}
		return ReattachResult{Observation: obs, Authority: authority}, nil
	}
	if authority.Fenced || authority.Owner != core.OwnerAgent {
		if obs.Owner.Validate() == nil && req.Binding.DesiredOwner != core.OwnerAgent {
			// H2 may durably own or revoke the authority generation while the H1
			// provider control observer still identifies the delegated pane as
			// agent-side. Reattach transport/session continuity in a fenced state;
			// only the handoff reconciler may grant the next actor.
			return ReattachResult{Observation: obs, Authority: authority}, nil
		}
		return ReattachResult{}, failure.New(failure.DelegatedReconcileBlocked, map[string]string{"session_id": req.Binding.SessionID, "provider_id": identity.ID, "current_epoch": fmt.Sprint(req.Binding.AuthorityEpoch), "reason": "provider_authority_unproven"}, nil)
	}
	return ReattachResult{Observation: obs, Authority: authority}, nil
}
