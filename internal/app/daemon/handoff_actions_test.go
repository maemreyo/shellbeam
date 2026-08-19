package daemon_test

import (
	"context"
	"sync"
	"testing"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
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
