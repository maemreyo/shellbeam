package interactivehandoff

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"

	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
)

type reconcileRuntime struct {
	*fakeRuntime
	humanErr error
}

func (r *reconcileRuntime) InspectHumanClient(ctx context.Context, ref delegated.ProviderRef, client delegatedapp.ProviderClientRef) (delegatedapp.HumanClientObservation, error) {
	r.call("inspect_human")
	if r.humanErr != nil {
		return delegatedapp.HumanClientObservation{}, r.humanErr
	}
	return r.human, nil
}

func reconcileFixture(t *testing.T) (*fakeStore, *reconcileRuntime, *fakeFencer, *Service, *([]string), handoff.Request) {
	t.Helper()
	store, base, fencer, _, calls, req := fixture(t)
	runtime := &reconcileRuntime{fakeRuntime: base}
	return store, runtime, fencer, New(store, runtime, fencer), calls, req
}

func makeHumanOwned(t *testing.T, store *fakeStore, runtime *reconcileRuntime, svc *Service, req handoff.Request) handoff.State {
	t.Helper()
	if _, err := svc.Request(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	got, err := svc.AttachLocalHuman(t.Context(), req.HandoffID, delegatedapp.HumanAttachSpec{Stdin: bytes.NewBuffer(nil), Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	if got.State.Phase != handoff.PhaseHumanOwned {
		t.Fatalf("state=%#v", got.State)
	}
	return got.State
}

func TestHandoffReconcileAgentFencingFinishesExactFence(t *testing.T) {
	store, runtime, fencer, svc, calls, req := reconcileFixture(t)
	initial := handoff.State{SchemaVersion: handoff.StateSchemaVersion, HandoffID: req.HandoffID, SessionID: req.SessionID, Phase: handoff.PhaseAgentFencing, AuthorityEpoch: 2, DesiredOwner: delegated.OwnerHuman, ProviderOwner: delegated.OwnerAgent, AgentIngress: handoff.IngressUnknown, HumanIngress: handoff.IngressFenced, TransferBoundary: handoff.TransferBoundary{Kind: handoff.BoundaryNone}, PrivacyState: handoff.PrivacyStateStandard, PrivacyRelease: handoff.PrivacyReleaseNotRequired, CaptureState: handoff.CapturePublic, ProviderGeneration: "gen-h2"}
	store.req, store.state, store.found = req, initial, true
	store.binding.AuthorityEpoch, store.binding.DesiredOwner = 2, delegated.OwnerHuman
	fencer.proof = AgentIngressProof{AuthorityEpoch: 2, ProviderGeneration: "gen-h2", Fenced: true}
	*calls = nil

	got, err := svc.Reconcile(t.Context(), req.HandoffID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Phase != handoff.PhaseHumanConnecting || got.AgentIngress != handoff.IngressFenced || !got.TransferBoundary.Established {
		t.Fatalf("state=%#v", got)
	}
	if runtime.attachCalls.Load() != 0 {
		t.Fatalf("reconcile spawned human client: %d", runtime.attachCalls.Load())
	}
}

func TestHandoffReconcileConnectingReadOnlyClientCompletesHumanOwnership(t *testing.T) {
	store, runtime, _, svc, _, req := reconcileFixture(t)
	if _, err := svc.Request(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	state := store.state
	state.ProviderOwner = delegated.OwnerNone
	state.HumanClient = &handoff.HumanClientRef{Ref: "hclient_1"}
	store.state = state
	runtime.human = delegatedapp.HumanClientObservation{ClientRef: delegatedapp.ProviderClientRef{Ref: "hclient_1"}, Present: true, ReadOnly: true, ObservedOwner: delegated.OwnerNone, ProviderGeneration: state.ProviderGeneration}

	got, err := svc.Reconcile(t.Context(), req.HandoffID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Phase != handoff.PhaseHumanOwned || got.HumanIngress != handoff.IngressWritable || got.ProviderOwner != delegated.OwnerHuman {
		t.Fatalf("state=%#v", got)
	}
}

func TestHandoffReconcileHumanOwnedRearmsControlWithoutRetogglingWritable(t *testing.T) {
	store, runtime, _, svc, calls, req := reconcileFixture(t)
	state := makeHumanOwned(t, store, runtime, svc, req)
	*calls = nil

	got, err := svc.Reconcile(t.Context(), req.HandoffID)
	if err != nil || got != state {
		t.Fatalf("state=%#v err=%v", got, err)
	}
	wantInspect, wantArm := false, false
	for _, call := range *calls {
		if call == "inspect_human" {
			wantInspect = true
		}
		if call == "arm_human_control" {
			wantArm = true
		}
		if call == "human_writable" {
			t.Fatalf("reconcile retoggled already-writable client: %v", *calls)
		}
	}
	if !wantInspect || !wantArm {
		t.Fatalf("calls=%v", *calls)
	}
}

func TestHandoffReconcileHumanOwnedExactClientMissingReturnsFencedConnecting(t *testing.T) {
	store, runtime, _, svc, _, req := reconcileFixture(t)
	state := makeHumanOwned(t, store, runtime, svc, req)
	runtime.humanErr = failure.New(failure.HandoffClientLost, map[string]string{"reason": "client_missing"}, nil)

	got, err := svc.Reconcile(t.Context(), req.HandoffID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Phase != handoff.PhaseHumanConnecting || got.HumanClient != nil || got.AgentIngress != handoff.IngressFenced || got.HumanIngress != handoff.IngressFenced || got.DesiredOwner != delegated.OwnerHuman || got.AuthorityEpoch != state.AuthorityEpoch {
		t.Fatalf("state=%#v", got)
	}
}

func TestHandoffReconcileHumanOwnedProofLossDoesNotFabricateFence(t *testing.T) {
	store, runtime, _, svc, _, req := reconcileFixture(t)
	state := makeHumanOwned(t, store, runtime, svc, req)
	runtime.humanErr = failure.New(failure.HandoffClientLost, map[string]string{"reason": "client_state_missing"}, errors.New("lost private client proof"))

	got, err := svc.Reconcile(t.Context(), req.HandoffID)
	if !errors.Is(err, failure.HandoffReclaimBlocked) {
		t.Fatalf("ambiguous proof err=%v", err)
	}
	if got.Phase != handoff.PhaseReclaimPending || got.HumanIngress != handoff.IngressUnknown || got.AgentIngress != handoff.IngressFenced || got.HumanClient == nil || got.TransferBoundary.Established {
		t.Fatalf("ambiguous proof was not fail-closed: %#v", got)
	}
	if got.AuthorityEpoch != state.AuthorityEpoch || got.DesiredOwner != delegated.OwnerHuman {
		t.Fatalf("proof loss changed authority lifetime: %#v", got)
	}
}

func TestHandoffReconcileHumanFencingCompletesReadyReclaim(t *testing.T) {
	store, runtime, _, svc, _, req := reconcileFixture(t)
	state := makeHumanOwned(t, store, runtime, svc, req)
	state.Phase = handoff.PhaseHumanFencing
	state.TransferBoundary = handoff.TransferBoundary{Kind: handoff.BoundaryHumanAttested, Established: true}
	store.state = state
	store.binding.AuthorityEpoch, store.binding.DesiredOwner = state.AuthorityEpoch, delegated.OwnerHuman
	runtime.obs.Owner = delegated.OwnerAgent

	got, err := svc.Reconcile(t.Context(), req.HandoffID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Phase != handoff.PhaseAgentOwned || got.DesiredOwner != delegated.OwnerAgent || got.AgentIngress != handoff.IngressWritable || got.HumanIngress != handoff.IngressFenced || got.AuthorityEpoch != state.AuthorityEpoch+1 {
		t.Fatalf("state=%#v", got)
	}
}

func TestHandoffReconcileControlObserverLossRefencesHumanBeforeServing(t *testing.T) {
	store, runtime, _, svc, _, req := reconcileFixture(t)
	state := makeHumanOwned(t, store, runtime, svc, req)
	runtime.armErr = errors.New("control observer lost")
	got, err := svc.Reconcile(t.Context(), req.HandoffID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Phase != handoff.PhaseHumanConnecting || got.HumanClient != nil || got.AgentIngress != handoff.IngressFenced || got.HumanIngress != handoff.IngressFenced || got.FailureCode != failure.HandoffReclaimBlocked || got.AuthorityEpoch != state.AuthorityEpoch {
		t.Fatalf("control-loss state=%#v", got)
	}
	if !runtime.human.ReadOnly || runtime.human.ObservedOwner != delegated.OwnerNone {
		t.Fatalf("human remained writable after control loss: %#v", runtime.human)
	}
}

func TestHandoffReconcileAfterHumanFenceBeforeReadOnlyAckDoesNotRefence(t *testing.T) {
	store, runtime, _, svc, calls, req := reconcileFixture(t)
	state := makeHumanOwned(t, store, runtime, svc, req)
	state.Phase = handoff.PhaseHumanFencing
	state.TransferBoundary = handoff.TransferBoundary{Kind: handoff.BoundaryHumanAttested, Established: true}
	state.HumanIngress = handoff.IngressFenced
	state.ProviderOwner = delegated.OwnerNone
	store.state = state
	store.binding.AuthorityEpoch, store.binding.DesiredOwner = state.AuthorityEpoch, delegated.OwnerHuman
	runtime.human.ReadOnly = true
	runtime.human.ObservedOwner = delegated.OwnerNone
	runtime.obs.Owner = delegated.OwnerAgent
	*calls = nil

	got, err := svc.Reconcile(t.Context(), req.HandoffID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Phase != handoff.PhaseAgentOwned || got.AuthorityEpoch != state.AuthorityEpoch+1 {
		t.Fatalf("state=%#v", got)
	}
	for _, call := range *calls {
		if call == "fence_human" {
			t.Fatalf("durably fenced human ingress was fenced twice: %v", *calls)
		}
	}
	if !slices.Contains(*calls, "prepare_readonly_control") {
		t.Fatalf("read-only preparation was not replayed: %v", *calls)
	}
}

func TestHandoffReconcileAgentOwnedLostResponseIsPureReplay(t *testing.T) {
	store, runtime, _, svc, calls, req := reconcileFixture(t)
	state := makeHumanOwned(t, store, runtime, svc, req)
	state.Phase = handoff.PhaseAgentOwned
	state.AuthorityEpoch++
	state.DesiredOwner = delegated.OwnerAgent
	state.ProviderOwner = delegated.OwnerAgent
	state.AgentIngress = handoff.IngressWritable
	state.HumanIngress = handoff.IngressFenced
	state.FailureCode = ""
	store.state = state
	store.binding.AuthorityEpoch, store.binding.DesiredOwner = state.AuthorityEpoch, delegated.OwnerAgent
	*calls = nil

	got, err := svc.Reconcile(t.Context(), req.HandoffID)
	if err != nil || got != state {
		t.Fatalf("state=%#v err=%v", got, err)
	}
	if len(*calls) != 1 || (*calls)[0] != "recover_handoff" {
		t.Fatalf("agent-owned replay touched provider: %v", *calls)
	}
	if runtime.signals != 0 {
		t.Fatalf("agent-owned replay signalled provider: %d", runtime.signals)
	}
}
