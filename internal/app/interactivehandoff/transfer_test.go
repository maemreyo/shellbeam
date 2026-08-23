package interactivehandoff

import (
	"errors"
	"reflect"
	"testing"
	"time"

	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
)

func TestAttachLocalHumanOrdersReadOnlyProofProvenanceWritableAndPublish(t *testing.T) {
	store, _, _, svc, calls, req := fixture(t)
	if _, err := svc.Request(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	*calls = nil
	result, err := svc.AttachLocalHuman(t.Context(), req.HandoffID, delegatedapp.HumanAttachSpec{})
	if err != nil {
		t.Fatal(err)
	}
	if result.State.Phase != handoff.PhaseHumanOwned || result.State.ProviderOwner != delegated.OwnerHuman || result.State.HumanIngress != handoff.IngressWritable || result.State.AgentIngress != handoff.IngressFenced || result.State.HumanClient == nil || result.State.HumanClient.Ref != "hclient_1" {
		t.Fatalf("result=%#v", result)
	}
	want := []string{"find", "load_ref", "attach_human", "inspect_human", "inspect_human", "advance:human_connecting", "mark_human_provenance", "inspect_human", "human_writable", "inspect_human", "arm_human_control", "advance:human_owned"}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("calls=%v want=%v", *calls, want)
	}
	if store.provenance != "human_write_authority_granted" {
		t.Fatalf("provenance=%q", store.provenance)
	}
}

func TestAttachControlFailureRefencesBeforeReturning(t *testing.T) {
	store, runtime, _, svc, _, req := fixture(t)
	if _, err := svc.Request(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	runtime.armErr = errTestControl
	if _, err := svc.AttachLocalHuman(t.Context(), req.HandoffID, delegatedapp.HumanAttachSpec{}); err == nil {
		t.Fatal("arm failure accepted")
	}
	if !runtime.human.ReadOnly || runtime.human.ObservedOwner != delegated.OwnerNone || store.state.Phase != handoff.PhaseHumanConnecting || store.state.HumanIngress != handoff.IngressFenced {
		t.Fatalf("human=%#v state=%#v", runtime.human, store.state)
	}
}

var errTestControl = &testError{"control unavailable"}

type testError struct{ s string }

func (e *testError) Error() string { return e.s }

func TestAttachReplayAfterWritableProviderContinuesWithoutSecondAttachOrToggle(t *testing.T) {
	store, runtime, _, svc, calls, req := fixture(t)
	if _, err := svc.Request(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	store.state.HumanClient = &handoff.HumanClientRef{Ref: "hclient_1"}
	store.provenance = "human_write_authority_granted"
	runtime.human.ReadOnly = false
	runtime.human.ObservedOwner = delegated.OwnerHuman
	*calls = nil
	result, err := svc.AttachLocalHuman(t.Context(), req.HandoffID, delegatedapp.HumanAttachSpec{})
	if err != nil {
		t.Fatal(err)
	}
	if result.State.Phase != handoff.PhaseHumanOwned {
		t.Fatalf("state=%#v", result.State)
	}
	for _, call := range *calls {
		if call == "attach_human" || call == "human_writable" {
			t.Fatalf("replay repeated provider mutation: %v", *calls)
		}
	}
}

func TestAttachRejectsAlreadyWritableClientWithoutDurableHumanProvenance(t *testing.T) {
	store, runtime, _, svc, _, req := fixture(t)
	if _, err := svc.Request(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	store.state.HumanClient = &handoff.HumanClientRef{Ref: "hclient_1"}
	runtime.human.ReadOnly = false
	runtime.human.ObservedOwner = delegated.OwnerHuman
	if _, err := svc.AttachLocalHuman(t.Context(), req.HandoffID, delegatedapp.HumanAttachSpec{}); !errors.Is(err, failure.HumanClientNotProven) {
		t.Fatalf("writable client without provenance err=%v", err)
	}
}

func TestConcurrentAttachRetryCreatesOneProviderClientAndConverges(t *testing.T) {
	_, runtime, _, svc, _, req := fixture(t)
	if _, err := svc.Request(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	runtime.calls = nil // concurrent behavior test; avoid call-log slice as shared state
	runtime.attachEntered = make(chan struct{})
	runtime.attachRelease = make(chan struct{})

	type result struct {
		attach LocalAttachResult
		err    error
	}
	results := make(chan result, 2)
	attach := func() {
		got, err := svc.AttachLocalHuman(t.Context(), req.HandoffID, delegatedapp.HumanAttachSpec{})
		results <- result{attach: got, err: err}
	}
	go attach()
	select {
	case <-runtime.attachEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("first attach never reached provider")
	}
	go attach()
	time.Sleep(125 * time.Millisecond)
	if got := runtime.attachCalls.Load(); got != 1 {
		close(runtime.attachRelease)
		t.Fatalf("concurrent retry created %d provider clients before first attach resolved; want 1", got)
	}
	close(runtime.attachRelease)
	for i := 0; i < 2; i++ {
		select {
		case got := <-results:
			if got.err != nil || got.attach.State.Phase != handoff.PhaseHumanOwned || got.attach.State.HumanClient == nil || got.attach.State.HumanClient.Ref != "hclient_1" {
				t.Fatalf("attach retry %d result=%#v err=%v", i, got.attach, got.err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("concurrent attach retry did not converge")
		}
	}
	if got := runtime.attachCalls.Load(); got != 1 {
		t.Fatalf("provider attach calls=%d want 1", got)
	}
}

func TestLocalBootstrapReturnsOnlyDurableProviderRefAndConnectingState(t *testing.T) {
	store, runtime, _, svc, calls, req := fixture(t)
	state, err := svc.Request(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	*calls = nil
	got, err := svc.BootstrapLocalHuman(t.Context(), req.HandoffID)
	if err != nil {
		t.Fatal(err)
	}
	if got.HandoffID != req.HandoffID || got.ProviderRef != store.ref || got.State != state {
		t.Fatalf("bootstrap=%#v", got)
	}
	if runtime.attachCalls.Load() != 0 {
		t.Fatalf("bootstrap attached human: %d", runtime.attachCalls.Load())
	}
	if !reflect.DeepEqual(*calls, []string{"find", "load_ref"}) {
		t.Fatalf("bootstrap calls=%v", *calls)
	}
}

func TestBindLocalHumanUsesPrecreatedReadOnlyClientWithoutAttachAndPublishesHumanOwned(t *testing.T) {
	_, runtime, _, svc, calls, req := fixture(t)
	if _, err := svc.Request(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	*calls = nil
	client := delegatedapp.ProviderClientRef{Ref: "hclient_1"}
	got, err := svc.BindLocalHuman(t.Context(), req.HandoffID, client)
	if err != nil {
		t.Fatal(err)
	}
	if got.Phase != handoff.PhaseHumanOwned || got.HumanClient == nil || got.HumanClient.Ref != client.Ref || got.HumanIngress != handoff.IngressWritable || got.ProviderOwner != delegated.OwnerHuman {
		t.Fatalf("state=%#v", got)
	}
	if runtime.attachCalls.Load() != 0 {
		t.Fatalf("bind invoked attach: %d", runtime.attachCalls.Load())
	}
	want := []string{"find", "load_ref", "inspect_human", "advance:human_connecting", "mark_human_provenance", "inspect_human", "human_writable", "inspect_human", "arm_human_control", "advance:human_owned"}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("calls=%v want=%v", *calls, want)
	}
}

func TestBindLocalHumanExactReplayDoesNotRetoggleOrReattach(t *testing.T) {
	_, runtime, _, svc, calls, req := fixture(t)
	if _, err := svc.Request(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	client := delegatedapp.ProviderClientRef{Ref: "hclient_1"}
	first, err := svc.BindLocalHuman(t.Context(), req.HandoffID, client)
	if err != nil {
		t.Fatal(err)
	}
	*calls = nil
	second, err := svc.BindLocalHuman(t.Context(), req.HandoffID, client)
	if err != nil || second != first {
		t.Fatalf("replay=%#v first=%#v err=%v", second, first, err)
	}
	if runtime.attachCalls.Load() != 0 {
		t.Fatalf("replay attached: %d", runtime.attachCalls.Load())
	}
	for _, call := range *calls {
		if call == "human_writable" || call == "mark_human_provenance" || call == "arm_human_control" {
			t.Fatalf("replay repeated mutation: %v", *calls)
		}
	}
}

func TestBindLocalHumanDifferentClientConflictsAfterDurableBind(t *testing.T) {
	_, _, _, svc, _, req := fixture(t)
	if _, err := svc.Request(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.BindLocalHuman(t.Context(), req.HandoffID, delegatedapp.ProviderClientRef{Ref: "hclient_1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.BindLocalHuman(t.Context(), req.HandoffID, delegatedapp.ProviderClientRef{Ref: "hclient_other"}); !errors.Is(err, failure.HandoffConflict) {
		t.Fatalf("different client err=%v", err)
	}
}
