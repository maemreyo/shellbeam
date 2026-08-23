package delegatedsession

import (
	"context"
	"errors"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
)

type reattachSink struct{}

func (*reattachSink) Append([]byte) error { return nil }

type reattachProvider struct {
	countingProvider
	identity core.ProviderIdentity
	obs      Observation
	err      error
	calls    int
}

func (p *reattachProvider) Identity() core.ProviderIdentity { return p.identity }
func (p *reattachProvider) Reattach(context.Context, core.ProviderRef, OutputSink) (Observation, error) {
	p.calls++
	return p.obs, p.err
}

func reattachBinding() (core.Binding, core.ProviderRef) {
	now := time.Date(2026, 8, 19, 2, 30, 0, 0, time.UTC)
	binding := core.Binding{SchemaVersion: core.BindingSchemaVersion, SessionID: "session_recover", OperationID: "op_recover", SessionMode: core.ModeDelegatedInteractive, AuthorityEpoch: 3, DesiredOwner: core.OwnerAgent, ProviderID: "tmux_control_mode", ProviderVersion: 1, Lifecycle: core.LifecycleLive, CreatedAt: now, UpdatedAt: now}
	ref := core.ProviderRef{SchemaVersion: core.ProviderRefSchemaVersion, SessionID: binding.SessionID, ProviderID: binding.ProviderID, ProviderVersion: binding.ProviderVersion, Ref: "dtmux_recover", CreatedAt: now, UpdatedAt: now}
	return binding, ref
}

func TestDelegatedReconcileReattachBoundReturnsExactAgentAuthorityOnlyFromCurrentMatchingProvider(t *testing.T) {
	binding, ref := reattachBinding()
	provider := &reattachProvider{identity: binding.ProviderIdentity(), obs: Observation{Provider: binding.ProviderIdentity(), ProviderCurrent: true, ProviderGeneration: "gen_exact", Owner: core.OwnerAgent}}
	result, err := New(provider).ReattachBound(t.Context(), ReattachRequest{Binding: binding, ProviderRef: ref, Output: &reattachSink{}})
	if err != nil {
		t.Fatal(err)
	}
	if provider.calls != 1 || result.Observation.ProviderGeneration != "gen_exact" || result.Authority.Epoch != binding.AuthorityEpoch || result.Authority.Owner != core.OwnerAgent || result.Authority.Fenced {
		t.Fatalf("result=%#v calls=%d", result, provider.calls)
	}
}

func TestDelegatedReconcileReattachBoundAllowsProviderProvenTerminalWithoutInventingAgentAuthority(t *testing.T) {
	binding, ref := reattachBinding()
	zero := 0
	provider := &reattachProvider{identity: binding.ProviderIdentity(), obs: Observation{Provider: binding.ProviderIdentity(), ProviderCurrent: true, ProviderGeneration: "gen_terminal", Owner: core.OwnerNone, Terminal: true, ExitCode: &zero, OutputBytes: 7}}
	result, err := New(provider).ReattachBound(t.Context(), ReattachRequest{Binding: binding, ProviderRef: ref, Output: &reattachSink{}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Observation.Terminal || !result.Authority.Fenced || result.Authority.Owner != core.OwnerNone {
		t.Fatalf("terminal result=%#v", result)
	}
}

func TestDelegatedReconcileReattachBoundFailsClosedOnBindingRefProviderOrAuthorityMismatch(t *testing.T) {
	binding, ref := reattachBinding()
	cases := map[string]struct {
		mutate   func(*core.Binding, *core.ProviderRef, *reattachProvider)
		wantCode failure.Code
		wantCall int
	}{
		"ref":         {func(_ *core.Binding, r *core.ProviderRef, _ *reattachProvider) { r.SessionID = "other" }, failure.DelegatedProviderMismatch, 0},
		"provider":    {func(_ *core.Binding, _ *core.ProviderRef, p *reattachProvider) { p.identity.Version = 2 }, failure.DelegatedProviderMismatch, 0},
		"observation": {func(_ *core.Binding, _ *core.ProviderRef, p *reattachProvider) { p.obs.Provider.Version = 2 }, failure.DelegatedProviderMismatch, 1},
		"owner":       {func(_ *core.Binding, _ *core.ProviderRef, p *reattachProvider) { p.obs.Owner = core.OwnerHuman }, failure.DelegatedReconcileBlocked, 1},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			b, r := binding, ref
			p := &reattachProvider{identity: b.ProviderIdentity(), obs: Observation{Provider: b.ProviderIdentity(), ProviderCurrent: true, ProviderGeneration: "gen_exact", Owner: core.OwnerAgent}}
			tc.mutate(&b, &r, p)
			_, err := New(p).ReattachBound(t.Context(), ReattachRequest{Binding: b, ProviderRef: r, Output: &reattachSink{}})
			if !errors.Is(err, tc.wantCode) || p.calls != tc.wantCall {
				t.Fatalf("err=%v calls=%d want=%s/%d", err, p.calls, tc.wantCode, tc.wantCall)
			}
		})
	}
}

func TestDelegatedReconcileReattachBoundAllowsFencedContinuityForHandoffOwnedBinding(t *testing.T) {
	for _, owner := range []core.Owner{core.OwnerHuman, core.OwnerNone} {
		t.Run(string(owner), func(t *testing.T) {
			binding, ref := reattachBinding()
			binding.DesiredOwner = owner
			provider := &reattachProvider{identity: binding.ProviderIdentity(), obs: Observation{Provider: binding.ProviderIdentity(), ProviderCurrent: true, ProviderGeneration: "gen_handoff", Owner: core.OwnerAgent}}
			result, err := New(provider).ReattachBound(t.Context(), ReattachRequest{Binding: binding, ProviderRef: ref, Output: &reattachSink{}})
			if err != nil {
				t.Fatal(err)
			}
			if provider.calls != 1 || !result.Authority.Fenced || result.Authority.Owner != core.OwnerNone || result.Authority.Epoch != binding.AuthorityEpoch || result.Observation.ProviderGeneration != "gen_handoff" {
				t.Fatalf("owner=%q result=%#v calls=%d", owner, result, provider.calls)
			}
		})
	}
}
