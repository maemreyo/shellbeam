package interactivehandoff

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

type fakeStore struct {
	mu         sync.Mutex
	binding    delegated.Binding
	ref        delegated.ProviderRef
	req        handoff.Request
	state      handoff.State
	found      bool
	provenance string
	controls   map[string]fakeControl
	calls      *[]string
}

type fakeControl struct {
	signal    handoff.ControlSignal
	outcome   string
	completed bool
}

func (s *fakeStore) call(v string) {
	if s.calls != nil {
		*s.calls = append(*s.calls, v)
	}
}
func (s *fakeStore) FindHandoff(context.Context, string) (handoff.Request, handoff.State, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.call("find")
	return s.req, s.state, s.found, nil
}
func (s *fakeStore) ReserveHandoff(_ context.Context, req handoff.Request, state handoff.State) (handoff.State, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.call("reserve")
	if s.found {
		if s.req != req {
			return s.state, false, failure.New(failure.HandoffConflict, map[string]string{"handoff_id": req.HandoffID}, nil)
		}
		return s.state, false, nil
	}
	s.req = req
	s.state = state
	s.found = true
	s.binding.AuthorityEpoch = state.AuthorityEpoch
	s.binding.DesiredOwner = state.DesiredOwner
	return state, true, nil
}
func (s *fakeStore) AdvanceHandoff(_ context.Context, state handoff.State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.call("advance:" + string(state.Phase))
	s.state = state
	if s.binding.AuthorityEpoch != state.AuthorityEpoch || s.binding.DesiredOwner != state.DesiredOwner {
		s.binding.AuthorityEpoch = state.AuthorityEpoch
		s.binding.DesiredOwner = state.DesiredOwner
	}
	return nil
}
func (s *fakeStore) RecoverHandoff(context.Context, string) (handoff.State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.call("recover_handoff")
	if !s.found {
		return handoff.State{}, errors.New("missing")
	}
	return s.state, nil
}
func (s *fakeStore) LoadHandoff(context.Context, string) (handoff.Request, handoff.State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.call("load_handoff")
	if !s.found {
		return handoff.Request{}, handoff.State{}, errors.New("missing")
	}
	return s.req, s.state, nil
}
func (s *fakeStore) LoadDelegatedBinding(context.Context, operation.SessionID) (delegated.Binding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.call("load_binding")
	return s.binding, nil
}
func (s *fakeStore) LoadDelegatedProviderRef(context.Context, operation.SessionID) (delegated.ProviderRef, error) {
	s.call("load_ref")
	return s.ref, nil
}
func (s *fakeStore) ReserveControlSignal(_ context.Context, sig handoff.ControlSignal) (handoff.ControlSignal, string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.call("reserve_control:" + string(sig.Kind))
	if s.controls == nil {
		s.controls = map[string]fakeControl{}
	}
	if old, ok := s.controls[sig.ControlID]; ok {
		if old.signal != sig {
			return old.signal, old.outcome, false, failure.New(failure.HandoffConflict, map[string]string{"handoff_id": sig.HandoffID}, nil)
		}
		return old.signal, old.outcome, false, nil
	}
	if sig.AuthorityEpoch != s.state.AuthorityEpoch {
		return handoff.ControlSignal{}, "", false, failure.New(failure.StaleControlGeneration, nil, nil)
	}
	s.controls[sig.ControlID] = fakeControl{signal: sig}
	return sig, "", true, nil
}
func (s *fakeStore) CompleteControlSignal(_ context.Context, sig handoff.ControlSignal, outcome string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.call("complete_control:" + outcome)
	rec := s.controls[sig.ControlID]
	if rec.completed && rec.outcome != outcome {
		return rec.outcome, failure.New(failure.HandoffConflict, nil, nil)
	}
	rec.completed = true
	rec.outcome = outcome
	s.controls[sig.ControlID] = rec
	return outcome, nil
}
func (s *fakeStore) MarkHumanWriteAuthorityGranted(context.Context, operation.SessionID) error {
	s.call("mark_human_provenance")
	s.provenance = "human_write_authority_granted"
	return nil
}
func (s *fakeStore) LoadInputAuthorityProvenance(context.Context, operation.SessionID) (string, error) {
	s.call("load_human_provenance")
	return s.provenance, nil
}

type fakeRuntime struct {
	calls                   *[]string
	obs                     delegatedapp.Observation
	human                   delegatedapp.HumanClientObservation
	attach                  delegatedapp.HumanAttachResult
	armErr                  error
	fenceProviderGeneration string
	signals                 int
	attachCalls             atomic.Int32
	attachEntered           chan struct{}
	attachRelease           chan struct{}
	attachOnce              sync.Once
}

func (r *fakeRuntime) call(v string) {
	if r.calls != nil {
		*r.calls = append(*r.calls, v)
	}
}
func (r *fakeRuntime) Inspect(context.Context, delegated.ProviderRef) (delegatedapp.Observation, error) {
	r.call("inspect_agent")
	return r.obs, nil
}
func (r *fakeRuntime) AttachHuman(ctx context.Context, _ delegated.ProviderRef, _ delegatedapp.HumanAttachSpec) (delegatedapp.HumanAttachResult, error) {
	r.call("attach_human")
	r.attachCalls.Add(1)
	if r.attachEntered != nil {
		r.attachOnce.Do(func() { close(r.attachEntered) })
	}
	if r.attachRelease != nil {
		select {
		case <-r.attachRelease:
		case <-ctx.Done():
			return delegatedapp.HumanAttachResult{}, ctx.Err()
		}
	}
	return r.attach, nil
}
func (r *fakeRuntime) SetHumanWritable(_ context.Context, _ delegated.ProviderRef, _ delegatedapp.ProviderClientRef, w bool) error {
	if w {
		r.call("human_writable")
		r.human.ReadOnly = false
		r.human.ObservedOwner = delegated.OwnerHuman
	} else {
		r.call("human_readonly")
		r.human.ReadOnly = true
		r.human.ObservedOwner = delegated.OwnerNone
	}
	return nil
}
func (r *fakeRuntime) FenceHumanIngress(_ context.Context, _ delegated.ProviderRef, c delegatedapp.ProviderClientRef, e delegated.AuthorityEpoch) (delegatedapp.IngressFenceProof, error) {
	r.call("fence_human")
	r.human.ReadOnly = true
	r.human.ObservedOwner = delegated.OwnerNone
	generation := r.human.ProviderGeneration
	if r.fenceProviderGeneration != "" {
		generation = r.fenceProviderGeneration
	}
	return delegatedapp.IngressFenceProof{ClientRef: c, AuthorityEpoch: e, ProviderGeneration: generation, Fenced: true}, nil
}
func (r *fakeRuntime) InspectHumanClient(context.Context, delegated.ProviderRef, delegatedapp.ProviderClientRef) (delegatedapp.HumanClientObservation, error) {
	r.call("inspect_human")
	return r.human, nil
}
func (r *fakeRuntime) ArmWritableHumanControl(context.Context, delegated.ProviderRef, delegatedapp.ProviderClientRef, delegatedapp.HumanControlSpec) error {
	r.call("arm_human_control")
	return r.armErr
}
func (r *fakeRuntime) PrepareReadOnlyLocalControl(context.Context, delegated.ProviderRef, delegatedapp.ProviderClientRef) error {
	r.call("prepare_readonly_control")
	return nil
}
func (r *fakeRuntime) Signal(context.Context, delegated.ProviderRef, string) error {
	r.call("signal")
	r.signals++
	return nil
}

type fakeFencer struct {
	calls *[]string
	proof AgentIngressProof
	err   error
}

func (f *fakeFencer) FenceAgentIngress(context.Context, string, delegated.AuthorityEpoch) (AgentIngressProof, error) {
	if f.calls != nil {
		*f.calls = append(*f.calls, "fence_agent")
	}
	return f.proof, f.err
}

func fixture(t *testing.T) (*fakeStore, *fakeRuntime, *fakeFencer, *Service, *[]string, handoff.Request) {
	t.Helper()
	calls := []string{}
	now := time.Date(2026, 8, 19, 7, 0, 0, 0, time.UTC)
	binding := delegated.Binding{SchemaVersion: delegated.BindingSchemaVersion, SessionID: "session-h2", OperationID: "op-h2", SessionMode: delegated.ModeDelegatedInteractive, AuthorityEpoch: 1, DesiredOwner: delegated.OwnerAgent, ProviderID: "tmux_control_mode", ProviderVersion: 1, Lifecycle: delegated.LifecycleLive, CreatedAt: now, UpdatedAt: now}
	ref := delegated.ProviderRef{SchemaVersion: delegated.ProviderRefSchemaVersion, SessionID: binding.SessionID, ProviderID: binding.ProviderID, ProviderVersion: 1, Ref: "provider_ref_h2", CreatedAt: now, UpdatedAt: now}
	store := &fakeStore{binding: binding, ref: ref, calls: &calls}
	runtime := &fakeRuntime{calls: &calls, obs: delegatedapp.Observation{Provider: binding.ProviderIdentity(), ProviderCurrent: true, ProviderGeneration: "gen-h2", Owner: delegated.OwnerAgent}, human: delegatedapp.HumanClientObservation{ClientRef: delegatedapp.ProviderClientRef{Ref: "hclient_1"}, Present: true, ReadOnly: true, ObservedOwner: delegated.OwnerNone, ProviderGeneration: "gen-h2"}, attach: delegatedapp.HumanAttachResult{ClientRef: delegatedapp.ProviderClientRef{Ref: "hclient_1"}, ObservedOwner: delegated.OwnerNone}}
	fencer := &fakeFencer{calls: &calls, proof: AgentIngressProof{AuthorityEpoch: 2, ProviderGeneration: "gen-h2", Fenced: true}}
	svc := New(store, runtime, fencer)
	req := handoff.Request{HandoffID: "handoff-h2", SessionID: binding.SessionID, Reason: handoff.ReasonManualIntervention, Privacy: handoff.PrivacyStandard, Completion: handoff.Completion{Kind: handoff.CompletionManualReady}}
	return store, runtime, fencer, svc, &calls, req
}

func TestRequestRotatesEpochThenProvesAgentFenceBeforeHumanConnecting(t *testing.T) {
	store, _, _, svc, calls, req := fixture(t)
	got, err := svc.Request(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if got.Phase != handoff.PhaseHumanConnecting || got.AuthorityEpoch != 2 || got.DesiredOwner != delegated.OwnerHuman || got.AgentIngress != handoff.IngressFenced || got.HumanIngress != handoff.IngressFenced || got.TransferBoundary.Kind != handoff.BoundaryProviderOrdered || !got.TransferBoundary.Established {
		t.Fatalf("state=%#v", got)
	}
	want := []string{"find", "load_binding", "load_ref", "inspect_agent", "reserve", "fence_agent", "advance:human_connecting"}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("calls=%v want=%v", *calls, want)
	}
	if store.binding.AuthorityEpoch != 2 || store.binding.DesiredOwner != delegated.OwnerHuman {
		t.Fatalf("binding=%#v", store.binding)
	}
}

func TestRequestReplayPrecedesProviderFreshness(t *testing.T) {
	store, runtime, _, svc, calls, req := fixture(t)
	state, err := svc.Request(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	*calls = nil
	runtime.obs.ProviderCurrent = false
	got, err := svc.Request(t.Context(), req)
	if err != nil || got != state {
		t.Fatalf("replay=%#v err=%v", got, err)
	}
	if !reflect.DeepEqual(*calls, []string{"find"}) {
		t.Fatalf("replay touched provider/store mutation: %v", *calls)
	}
	changed := req
	changed.Reason = handoff.ReasonHumanConfirmation
	if _, err := svc.Request(t.Context(), changed); !errors.Is(err, failure.HandoffConflict) {
		t.Fatalf("changed request err=%v", err)
	}
	_ = store
}

func TestRequestFenceFailureLeavesDurableFailClosedAgentFencing(t *testing.T) {
	store, _, fencer, svc, _, req := fixture(t)
	fencer.err = errors.New("fence failed")
	if _, err := svc.Request(t.Context(), req); err == nil {
		t.Fatal("fence failure accepted")
	}
	if store.state.Phase != handoff.PhaseAgentFencing || store.binding.DesiredOwner != delegated.OwnerHuman || store.binding.AuthorityEpoch != 2 || store.state.AgentIngress == handoff.IngressWritable {
		t.Fatalf("state=%#v binding=%#v", store.state, store.binding)
	}
}
