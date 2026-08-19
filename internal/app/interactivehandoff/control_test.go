package interactivehandoff

import (
	"errors"
	"reflect"
	"testing"

	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
)

func humanOwnedFixture(t *testing.T) (*fakeStore, *fakeRuntime, *Service, *[]string, handoff.Request) {
	store, runtime, _, svc, calls, req := fixture(t)
	if _, err := svc.Request(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AttachLocalHuman(t.Context(), req.HandoffID, delegatedapp.HumanAttachSpec{}); err != nil {
		t.Fatal(err)
	}
	*calls = nil
	return store, runtime, svc, calls, req
}

func TestReadyFencesHumanBeforePublishingNextEpochAgentAuthority(t *testing.T) {
	store, _, svc, calls, req := humanOwnedFixture(t)
	sig := handoff.ControlSignal{HandoffID: req.HandoffID, AuthorityEpoch: 2, ControlID: "ready-1", Kind: handoff.HumanControlReady}
	result, err := svc.HumanControl(t.Context(), sig)
	if err != nil {
		t.Fatal(err)
	}
	if result.State.Phase != handoff.PhaseAgentOwned || result.State.AuthorityEpoch != 3 || result.State.DesiredOwner != delegated.OwnerAgent || result.State.AgentIngress != handoff.IngressWritable || result.State.HumanIngress != handoff.IngressFenced || result.State.TransferBoundary.Kind != handoff.BoundaryHumanAttested {
		t.Fatalf("state=%#v", result.State)
	}
	want := []string{"reserve_control:ready", "load_handoff", "advance:human_fencing", "load_ref", "fence_human", "advance:human_fencing", "prepare_readonly_control", "inspect_agent", "load_binding", "advance:agent_owned", "complete_control:ready"}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("calls=%v want=%v", *calls, want)
	}
	if store.binding.AuthorityEpoch != 3 || store.binding.DesiredOwner != delegated.OwnerAgent {
		t.Fatalf("binding=%#v", store.binding)
	}
}

func TestDuplicateReadyReplaysWithoutProviderMutation(t *testing.T) {
	_, _, svc, calls, req := humanOwnedFixture(t)
	sig := handoff.ControlSignal{HandoffID: req.HandoffID, AuthorityEpoch: 2, ControlID: "ready-dup", Kind: handoff.HumanControlReady}
	first, err := svc.HumanControl(t.Context(), sig)
	if err != nil {
		t.Fatal(err)
	}
	*calls = nil
	second, err := svc.HumanControl(t.Context(), sig)
	if err != nil || second.State != first.State || second.Outcome != "ready" {
		t.Fatalf("replay=%#v err=%v", second, err)
	}
	if !reflect.DeepEqual(*calls, []string{"reserve_control:ready", "load_handoff"}) {
		t.Fatalf("replay calls=%v", *calls)
	}
}

func TestAbortFencesHumanAndNeverSignalsDelegatedProcess(t *testing.T) {
	store, runtime, svc, calls, req := humanOwnedFixture(t)
	state, err := svc.Abort(t.Context(), req.HandoffID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Phase != handoff.PhaseAborted || state.DesiredOwner != delegated.OwnerNone || state.AgentIngress != handoff.IngressFenced || state.HumanIngress != handoff.IngressFenced || state.AuthorityEpoch != 3 {
		t.Fatalf("state=%#v", state)
	}
	if runtime.signals != 0 {
		t.Fatalf("abort signalled delegated process: %d", runtime.signals)
	}
	want := []string{"find", "load_ref", "fence_human", "prepare_readonly_control", "advance:aborted"}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("calls=%v want=%v", *calls, want)
	}
	if store.binding.DesiredOwner != delegated.OwnerNone {
		t.Fatalf("binding=%#v", store.binding)
	}
}

func TestReadyRejectsProviderIdentityMismatchBeforeAgentAuthority(t *testing.T) {
	store, runtime, svc, _, req := humanOwnedFixture(t)
	runtime.obs.Provider = delegated.ProviderIdentity{ID: "other", Version: 1}
	sig := handoff.ControlSignal{HandoffID: req.HandoffID, AuthorityEpoch: 2, ControlID: "ready-mismatch", Kind: handoff.HumanControlReady}
	if _, err := svc.HumanControl(t.Context(), sig); !errors.Is(err, failure.DelegatedProviderMismatch) {
		t.Fatalf("ready mismatch err=%v", err)
	}
	if store.state.Phase == handoff.PhaseAgentOwned || store.binding.DesiredOwner == delegated.OwnerAgent {
		t.Fatalf("mismatch granted agent state=%#v binding=%#v", store.state, store.binding)
	}
}

func TestResumeRetryContinuesFromHumanConnectingWithoutRepeatingEpochRotation(t *testing.T) {
	store, runtime, svc, calls, req := humanOwnedFixture(t)
	state, err := svc.Abort(t.Context(), req.HandoffID)
	if err != nil {
		t.Fatal(err)
	}
	state.AuthorityEpoch++
	state.DesiredOwner = delegated.OwnerHuman
	state.Phase = handoff.PhaseHumanConnecting
	state.HumanClient = nil
	state.TransferBoundary = handoff.TransferBoundary{Kind: handoff.BoundaryProviderOrdered, Established: true}
	store.state = state
	store.binding.AuthorityEpoch = state.AuthorityEpoch
	store.binding.DesiredOwner = delegated.OwnerHuman
	runtime.human.ReadOnly = true
	runtime.human.ObservedOwner = delegated.OwnerNone
	*calls = nil
	sig := handoff.ControlSignal{HandoffID: req.HandoffID, AuthorityEpoch: state.AuthorityEpoch - 1, ControlID: "resume-partial", Kind: handoff.HumanControlResume}
	if store.controls == nil {
		store.controls = map[string]fakeControl{}
	}
	store.controls[sig.ControlID] = fakeControl{signal: sig}
	result, err := svc.HumanControl(t.Context(), sig)
	if err != nil {
		t.Fatal(err)
	}
	if result.State.Phase != handoff.PhaseHumanConnecting || result.State.AuthorityEpoch != state.AuthorityEpoch || result.State.HumanClient != nil {
		t.Fatalf("resume=%#v", result)
	}
	for _, call := range *calls {
		if call == "human_writable" || call == "inspect_human" || call == "arm_human_control" {
			t.Fatalf("partial resume repeated provider mutation: %v", *calls)
		}
	}
}

func TestAbortRejectsStaleProviderGenerationFenceProof(t *testing.T) {
	store, runtime, svc, _, req := humanOwnedFixture(t)
	runtime.fenceProviderGeneration = "stale-provider-generation"
	if _, err := svc.Abort(t.Context(), req.HandoffID); !errors.Is(err, failure.HandoffReclaimBlocked) {
		t.Fatalf("abort stale generation err=%v", err)
	}
	if store.state.Phase == handoff.PhaseAborted || store.binding.DesiredOwner == delegated.OwnerNone {
		t.Fatalf("stale fence published abort state=%#v binding=%#v", store.state, store.binding)
	}
}

func TestResumeAfterDetachedLocalControlReturnsFreshHumanConnectingWithoutOldClientMutation(t *testing.T) {
	store, runtime, svc, calls, req := humanOwnedFixture(t)
	aborted, err := svc.Abort(t.Context(), req.HandoffID)
	if err != nil {
		t.Fatal(err)
	}
	if aborted.Phase != handoff.PhaseAborted || aborted.HumanClient == nil {
		t.Fatalf("aborted=%#v", aborted)
	}
	// H0 read-only fallback detaches before the local control prompt. The old
	// provider client is therefore gone and must never be made writable again.
	runtime.human.Present = false
	*calls = nil
	sig := handoff.ControlSignal{HandoffID: req.HandoffID, AuthorityEpoch: aborted.AuthorityEpoch, ControlID: "resume-detached", Kind: handoff.HumanControlResume}
	result, err := svc.HumanControl(t.Context(), sig)
	if err != nil {
		t.Fatal(err)
	}
	if result.State.Phase != handoff.PhaseHumanConnecting || result.State.AuthorityEpoch != aborted.AuthorityEpoch+1 || result.State.DesiredOwner != delegated.OwnerHuman || result.State.HumanClient != nil || result.State.HumanIngress != handoff.IngressFenced || result.State.AgentIngress != handoff.IngressFenced {
		t.Fatalf("resume=%#v", result.State)
	}
	for _, call := range *calls {
		if call == "inspect_human" || call == "human_writable" || call == "arm_human_control" {
			t.Fatalf("detached resume touched old client: %v", *calls)
		}
	}
	if store.binding.AuthorityEpoch != result.State.AuthorityEpoch || store.binding.DesiredOwner != delegated.OwnerHuman {
		t.Fatalf("binding=%#v", store.binding)
	}
}
