package daemon_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	handoffapp "github.com/maemreyo/shellbeam/internal/app/interactivehandoff"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func handoffRequestForSession(sid, suffix string) handoff.Request {
	return handoff.Request{
		HandoffID:  "handoff-daemon-" + suffix,
		SessionID:  sid,
		Reason:     handoff.ReasonManualIntervention,
		Privacy:    handoff.PrivacyStandard,
		Completion: handoff.Completion{Kind: handoff.CompletionManualReady},
	}
}

func TestHandoffRequestRotatesAuthorityAndReplayDoesNotRequireProviderFreshness(t *testing.T) {
	st := openDelegatedStartStore(t)
	runtime := newDelegatedStartRuntime()
	svc := app.NewService(st, &fakeOwner{}, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100, DelegatedRuntime: runtime})
	start := delegatedStartRequest()
	start.OperationID = "op-handoff-daemon-request"
	view, err := svc.Start(t.Context(), start)
	if err != nil {
		t.Fatal(err)
	}
	req := handoffRequestForSession(view.SessionID, "request")
	beforeInspect := runtime.inspects.Load()
	state, err := svc.RequestHandoff(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if state.Phase != handoff.PhaseHumanConnecting || state.AuthorityEpoch != 2 || state.DesiredOwner != delegated.OwnerHuman || state.AgentIngress != handoff.IngressFenced || !state.TransferBoundary.Established || state.TransferBoundary.Kind != handoff.BoundaryProviderOrdered {
		t.Fatalf("state=%#v", state)
	}
	binding, err := st.LoadDelegatedBinding(t.Context(), operation.SessionID(view.SessionID))
	if err != nil {
		t.Fatal(err)
	}
	if binding.AuthorityEpoch != 2 || binding.DesiredOwner != delegated.OwnerHuman {
		t.Fatalf("binding=%#v", binding)
	}
	firstInspect := runtime.inspects.Load()
	if firstInspect <= beforeInspect {
		t.Fatalf("request did not prove provider freshness: before=%d after=%d", beforeInspect, firstInspect)
	}
	replayed, err := svc.RequestHandoff(t.Context(), req)
	if err != nil || replayed != state {
		t.Fatalf("replay=%#v err=%v", replayed, err)
	}
	if runtime.inspects.Load() != firstInspect {
		t.Fatalf("durable replay touched provider: before=%d after=%d", firstInspect, runtime.inspects.Load())
	}
}

func TestHandoffWaitUsesSharedDaemonCoordinatorEventChannel(t *testing.T) {
	st := openDelegatedStartStore(t)
	runtime := newDelegatedStartRuntime()
	svc := app.NewService(st, &fakeOwner{}, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100, DelegatedRuntime: runtime})
	start := delegatedStartRequest()
	start.OperationID = "op-handoff-daemon-wait"
	view, err := svc.Start(t.Context(), start)
	if err != nil {
		t.Fatal(err)
	}
	req := handoffRequestForSession(view.SessionID, "wait")
	if _, err := svc.RequestHandoff(t.Context(), req); err != nil {
		t.Fatal(err)
	}

	waitCtx, cancel := context.WithTimeout(t.Context(), 750*time.Millisecond)
	defer cancel()
	done := make(chan handoffapp.WaitResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := svc.WaitHandoff(waitCtx, handoffapp.WaitRequest{HandoffID: req.HandoffID, Yield: 2 * time.Second})
		if err != nil {
			errCh <- err
			return
		}
		done <- result
	}()
	time.Sleep(25 * time.Millisecond)
	aborted, err := svc.AbortHandoff(t.Context(), req.HandoffID)
	if err != nil {
		t.Fatal(err)
	}
	if aborted.Phase != handoff.PhaseAborted {
		t.Fatalf("abort=%#v", aborted)
	}
	select {
	case err := <-errCh:
		t.Fatalf("wait err=%v", err)
	case result := <-done:
		if result.TimedOut || result.State.Phase != handoff.PhaseAborted {
			t.Fatalf("wait=%#v", result)
		}
	case <-waitCtx.Done():
		t.Fatal("wait did not wake from shared handoff event")
	}
}

type blockingHandoffWriteRuntime struct {
	*delegatedStartRuntime
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func newBlockingHandoffWriteRuntime() *blockingHandoffWriteRuntime {
	return &blockingHandoffWriteRuntime{delegatedStartRuntime: newDelegatedStartRuntime(), entered: make(chan struct{}), release: make(chan struct{})}
}

func (r *blockingHandoffWriteRuntime) Write(ctx context.Context, ref delegated.ProviderRef, data []byte) error {
	r.once.Do(func() { close(r.entered) })
	select {
	case <-r.release:
		r.writes.Add(1)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestHandoffAgentFenceWaitsForAlreadyAdmittedMutationDelivery(t *testing.T) {
	st := openDelegatedStartStore(t)
	runtime := newBlockingHandoffWriteRuntime()
	svc := app.NewService(st, &fakeOwner{}, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100, DelegatedRuntime: runtime})
	start := delegatedStartRequest()
	start.OperationID = "op-handoff-inflight-write"
	view, err := svc.Start(t.Context(), start)
	if err != nil {
		t.Fatal(err)
	}

	writeDone := make(chan error, 1)
	go func() {
		_, err := svc.Write(t.Context(), app.WriteRequest{SessionID: view.SessionID, AuthorityEpoch: 1, InputOffset: 0, Chars: "accepted-before-fence"})
		writeDone <- err
	}()
	select {
	case <-runtime.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("agent write never reached provider")
	}

	req := handoffRequestForSession(view.SessionID, "inflight")
	type requestResult struct {
		state handoff.State
		err   error
	}
	requestDone := make(chan requestResult, 1)
	go func() {
		state, err := svc.RequestHandoff(t.Context(), req)
		requestDone <- requestResult{state: state, err: err}
	}()

	select {
	case got := <-requestDone:
		t.Fatalf("handoff fence completed while old-authority provider delivery was in flight: %#v", got)
	case <-time.After(125 * time.Millisecond):
	}
	close(runtime.release)
	if err := <-writeDone; err != nil {
		t.Fatalf("pre-fence write failed: %v", err)
	}
	select {
	case got := <-requestDone:
		if got.err != nil || got.state.Phase != handoff.PhaseHumanConnecting || got.state.AgentIngress != handoff.IngressFenced {
			t.Fatalf("handoff after drain=%#v err=%v", got.state, got.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handoff fence did not complete after old-authority delivery drained")
	}
}

type h2LocalDaemonRuntime struct {
	*delegatedStartRuntime
	human delegatedapp.HumanClientObservation
}

func newH2LocalDaemonRuntime() *h2LocalDaemonRuntime {
	return &h2LocalDaemonRuntime{delegatedStartRuntime: newDelegatedStartRuntime()}
}
func (r *h2LocalDaemonRuntime) AttachHuman(context.Context, delegated.ProviderRef, delegatedapp.HumanAttachSpec) (delegatedapp.HumanAttachResult, error) {
	return delegatedapp.HumanAttachResult{}, errors.New("daemon local bind must not attach")
}
func (r *h2LocalDaemonRuntime) SetHumanWritable(_ context.Context, _ delegated.ProviderRef, client delegatedapp.ProviderClientRef, writable bool) error {
	r.human.ClientRef = client
	r.human.Present = true
	r.human.ReadOnly = !writable
	if writable {
		r.human.ObservedOwner = delegated.OwnerHuman
	} else {
		r.human.ObservedOwner = delegated.OwnerNone
	}
	return nil
}
func (r *h2LocalDaemonRuntime) FenceHumanIngress(_ context.Context, _ delegated.ProviderRef, client delegatedapp.ProviderClientRef, epoch delegated.AuthorityEpoch) (delegatedapp.IngressFenceProof, error) {
	r.human.ClientRef, r.human.ReadOnly, r.human.ObservedOwner = client, true, delegated.OwnerNone
	return delegatedapp.IngressFenceProof{ClientRef: client, AuthorityEpoch: epoch, ProviderGeneration: r.human.ProviderGeneration, Fenced: true}, nil
}
func (r *h2LocalDaemonRuntime) InspectHumanClient(context.Context, delegated.ProviderRef, delegatedapp.ProviderClientRef) (delegatedapp.HumanClientObservation, error) {
	return r.human, nil
}
func (*h2LocalDaemonRuntime) ArmWritableHumanControl(context.Context, delegated.ProviderRef, delegatedapp.ProviderClientRef, delegatedapp.HumanControlSpec) error {
	return nil
}
func (*h2LocalDaemonRuntime) WaitWritableHumanControl(context.Context, delegated.ProviderRef, delegatedapp.ProviderClientRef, delegatedapp.HumanControlSpec) (handoff.HumanControlKind, error) {
	return handoff.HumanControlStatus, nil
}
func (*h2LocalDaemonRuntime) PrepareReadOnlyLocalControl(context.Context, delegated.ProviderRef, delegatedapp.ProviderClientRef) error {
	return nil
}

func TestLocalHandoffBootstrapAndBindDelegateToSharedCoordinator(t *testing.T) {
	store := openDelegatedStartStore(t)
	runtime := newH2LocalDaemonRuntime()
	svc := app.NewService(store, &fakeOwner{}, app.Options{Incarnation: "h2-local", Shell: "/bin/sh", MaxQueuedInputBytes: 100, DelegatedRuntime: runtime})
	started, err := svc.Start(t.Context(), func() app.StartRequest {
		v := delegatedStartRequest()
		v.OperationID = "op-h2-local-bootstrap"
		return v
	}())
	if err != nil {
		t.Fatal(err)
	}
	req := handoff.Request{HandoffID: "handoff-local-bootstrap", SessionID: started.SessionID, Reason: handoff.ReasonManualIntervention, Privacy: handoff.PrivacyStandard, Completion: handoff.Completion{Kind: handoff.CompletionManualReady}}
	state, err := svc.RequestHandoff(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	boot, err := svc.BootstrapLocalHuman(t.Context(), req.HandoffID)
	if err != nil {
		t.Fatal(err)
	}
	if boot.HandoffID != req.HandoffID || boot.State != state || boot.ProviderRef.SessionID != started.SessionID {
		t.Fatalf("bootstrap=%#v", boot)
	}
	client := delegatedapp.ProviderClientRef{Ref: "hclient_daemon_local"}
	runtime.human = delegatedapp.HumanClientObservation{ClientRef: client, Present: true, ReadOnly: true, ObservedOwner: delegated.OwnerNone, ProviderGeneration: "gen_test"}
	bound, err := svc.BindLocalHuman(t.Context(), req.HandoffID, client)
	if err != nil {
		t.Fatal(err)
	}
	if bound.Phase != handoff.PhaseHumanOwned || bound.HumanClient == nil || bound.HumanClient.Ref != client.Ref {
		t.Fatalf("bound=%#v", bound)
	}
}

func TestPublicHandoffProjectionUsesDurableTimestampsAndOmitsProviderPrivateState(t *testing.T) {
	store := openDelegatedStartStore(t)
	runtime := newH2LocalDaemonRuntime()
	svc := app.NewService(store, &fakeOwner{}, app.Options{Incarnation: "h2-public", Shell: "/bin/sh", MaxQueuedInputBytes: 100, DelegatedRuntime: runtime})
	startReq := delegatedStartRequest()
	startReq.OperationID = "op-h2-public-projection"
	started, err := svc.Start(t.Context(), startReq)
	if err != nil {
		t.Fatal(err)
	}
	req := handoff.Request{HandoffID: "handoff-public-projection", SessionID: started.SessionID, Reason: handoff.ReasonManualIntervention, Privacy: handoff.PrivacyStandard, Completion: handoff.Completion{Kind: handoff.CompletionManualReady}}
	public, err := svc.RequestHandoffPublic(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if public.CreatedAt == nil || public.UpdatedAt == nil || public.UpdatedAt.Before(*public.CreatedAt) {
		t.Fatalf("timestamps=%v %v", public.CreatedAt, public.UpdatedAt)
	}
	if public.Status != handoff.StatusHumanConnecting || public.HandoffID != req.HandoffID || public.SessionID != started.SessionID || len(public.AttachArgv) != 5 || public.AttachArgv[4] != req.HandoffID {
		t.Fatalf("public=%#v", public)
	}
	wire, err := json.Marshal(public)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"gen_test", "provider_generation", "human_client", "client_ref", "provider_ref", "tmux_control_mode"} {
		if bytes.Contains(wire, []byte(forbidden)) {
			t.Fatalf("public projection leaked %q: %s", forbidden, wire)
		}
	}
}
