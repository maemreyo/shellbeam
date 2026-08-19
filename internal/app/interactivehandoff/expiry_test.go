package interactivehandoff

import (
	"context"
	"errors"
	"testing"

	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
)

type expiryRuntime struct {
	*fakeRuntime
	fenceErr error
}

func (r *expiryRuntime) FenceHumanIngress(ctx context.Context, ref delegated.ProviderRef, client delegatedapp.ProviderClientRef, epoch delegated.AuthorityEpoch) (delegatedapp.IngressFenceProof, error) {
	if r.fenceErr != nil {
		r.call("fence_human")
		return delegatedapp.IngressFenceProof{}, r.fenceErr
	}
	return r.fakeRuntime.FenceHumanIngress(ctx, ref, client, epoch)
}

func expiryFixture(t *testing.T) (*fakeStore, *expiryRuntime, *fakeFencer, *Service, handoff.Request) {
	t.Helper()
	store, base, fencer, _, _, req := fixture(t)
	runtime := &expiryRuntime{fakeRuntime: base}
	return store, runtime, fencer, New(store, runtime, fencer), req
}

func TestHandoffExpireConnectingRevokesEpochWithoutKillingSession(t *testing.T) {
	store, runtime, _, svc, req := expiryFixture(t)
	if _, err := svc.Request(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	before := store.state.AuthorityEpoch
	got, err := svc.Expire(t.Context(), req.HandoffID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Phase != handoff.PhaseAborted || got.AuthorityEpoch != before+1 || got.DesiredOwner != delegated.OwnerNone || got.AgentIngress != handoff.IngressFenced || got.HumanIngress != handoff.IngressFenced || got.FailureCode != failure.HandoffExpired {
		t.Fatalf("expired=%#v", got)
	}
	if runtime.signals != 0 {
		t.Fatalf("expiry signalled delegated session: %d", runtime.signals)
	}
}

func TestHandoffExpireHumanOwnedFencesExactClientBeforeAbort(t *testing.T) {
	store, runtime, _, svc, req := expiryFixture(t)
	if _, err := svc.Request(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AttachLocalHuman(t.Context(), req.HandoffID, delegatedapp.HumanAttachSpec{}); err != nil {
		t.Fatal(err)
	}
	before := store.state.AuthorityEpoch
	got, err := svc.Expire(t.Context(), req.HandoffID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Phase != handoff.PhaseAborted || got.AuthorityEpoch != before+1 || got.HumanIngress != handoff.IngressFenced || got.FailureCode != failure.HandoffExpired || !runtime.human.ReadOnly || runtime.human.ObservedOwner != delegated.OwnerNone {
		t.Fatalf("expired=%#v human=%#v", got, runtime.human)
	}
	if runtime.signals != 0 {
		t.Fatalf("expiry killed delegated session: %d", runtime.signals)
	}
}

func TestHandoffExpireAmbiguousHumanFenceRemainsBlockedAndRecoveryRetriesFence(t *testing.T) {
	store, runtime, _, svc, req := expiryFixture(t)
	if _, err := svc.Request(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AttachLocalHuman(t.Context(), req.HandoffID, delegatedapp.HumanAttachSpec{}); err != nil {
		t.Fatal(err)
	}
	before := store.state.AuthorityEpoch
	runtime.fenceErr = failure.New(failure.HandoffClientLost, map[string]string{"reason": "client_state_missing"}, errors.New("ambiguous client proof"))
	got, err := svc.Expire(t.Context(), req.HandoffID)
	if !errors.Is(err, failure.HandoffReclaimBlocked) {
		t.Fatalf("ambiguous expiry err=%v", err)
	}
	if got.Phase != handoff.PhaseReclaimPending || got.AuthorityEpoch != before+1 || got.DesiredOwner != delegated.OwnerNone || got.AgentIngress != handoff.IngressFenced || got.HumanIngress != handoff.IngressUnknown || got.FailureCode != failure.HandoffExpired || got.TransferBoundary.Established {
		t.Fatalf("blocked expiry=%#v", got)
	}

	runtime.fenceErr = nil
	recovered, err := svc.Reconcile(t.Context(), req.HandoffID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Phase != handoff.PhaseAborted || recovered.HumanIngress != handoff.IngressFenced || recovered.FailureCode != failure.HandoffExpired || recovered.AuthorityEpoch != got.AuthorityEpoch {
		t.Fatalf("recovered expiry=%#v", recovered)
	}
}

func TestHandoffExpireAgentOwnedReplayIsNoop(t *testing.T) {
	store, _, _, svc, req := expiryFixture(t)
	store.req = req
	store.state = handoff.State{SchemaVersion: handoff.StateSchemaVersion, HandoffID: req.HandoffID, SessionID: req.SessionID, Phase: handoff.PhaseAgentOwned, AuthorityEpoch: store.binding.AuthorityEpoch, DesiredOwner: delegated.OwnerAgent, ProviderOwner: delegated.OwnerAgent, AgentIngress: handoff.IngressWritable, HumanIngress: handoff.IngressFenced, TransferBoundary: handoff.TransferBoundary{Kind: handoff.BoundaryNone}, PrivacyState: handoff.PrivacyStateStandard, PrivacyRelease: handoff.PrivacyReleaseNotRequired, CaptureState: handoff.CapturePublic, ProviderGeneration: "gen-h2"}
	store.found = true
	before := store.state
	got, err := svc.Expire(t.Context(), req.HandoffID)
	if err != nil || got != before {
		t.Fatalf("got=%#v err=%v", got, err)
	}
}
