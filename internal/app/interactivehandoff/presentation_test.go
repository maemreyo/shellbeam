package interactivehandoff

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
	terminal "github.com/maemreyo/shellbeam/internal/core/terminalpresentation"
)

type fakeHandoffPresenter struct {
	store *fakeStore
	calls *[]string
	err   error
	seen  []PresentationRequest
}

func (p *fakeHandoffPresenter) Present(_ context.Context, req PresentationRequest) error {
	p.seen = append(p.seen, req)
	if p.calls != nil {
		*p.calls = append(*p.calls, "present")
	}
	if p.store == nil || p.store.state.Phase != handoff.PhaseHumanConnecting || p.store.state.AgentIngress != handoff.IngressFenced || p.store.state.HumanIngress != handoff.IngressFenced || !p.store.state.TransferBoundary.Established {
		return errors.New("presentation happened before durable HumanConnecting")
	}
	return p.err
}

func TestRequestWithPresentationRunsOnlyAfterDurableHumanConnecting(t *testing.T) {
	store, runtime, fencer, _, calls, req := fixture(t)
	presenter := &fakeHandoffPresenter{store: store, calls: calls}
	svc := NewWithPresenter(store, runtime, fencer, presenter)
	hint := presentationHint(t)

	got, err := svc.RequestWithPresentation(t.Context(), req, &hint)
	if err != nil {
		t.Fatal(err)
	}
	if got.Phase != handoff.PhaseHumanConnecting || len(presenter.seen) != 1 || presenter.seen[0].HandoffID != req.HandoffID || presenter.seen[0].BridgeAffinity == nil || *presenter.seen[0].BridgeAffinity != hint {
		t.Fatalf("state=%#v presentation=%#v", got, presenter.seen)
	}
	wantTail := []string{"fence_agent", "advance:human_connecting", "present"}
	if len(*calls) < len(wantTail) || !reflect.DeepEqual((*calls)[len(*calls)-len(wantTail):], wantTail) {
		t.Fatalf("ordering=%v want tail=%v", *calls, wantTail)
	}
}

func TestRequestWithPresentationFenceFailureNeverPresents(t *testing.T) {
	store, runtime, fencer, _, calls, req := fixture(t)
	fencer.err = errors.New("fence failed")
	presenter := &fakeHandoffPresenter{store: store, calls: calls}
	svc := NewWithPresenter(store, runtime, fencer, presenter)
	if _, err := svc.RequestWithPresentation(t.Context(), req, nil); err == nil {
		t.Fatal("fence failure accepted")
	}
	if len(presenter.seen) != 0 {
		t.Fatalf("presentation ran before authority fence: %#v", presenter.seen)
	}
}

func TestRequestWithPresentationLaunchFailureDegradesToManualAttach(t *testing.T) {
	store, runtime, fencer, _, _, req := fixture(t)
	presenter := &fakeHandoffPresenter{store: store, err: failure.New(failure.TerminalLaunchUnknown, map[string]string{"provider_id": "ghostty", "reason": "client_not_proven"}, nil)}
	svc := NewWithPresenter(store, runtime, fencer, presenter)
	state, err := svc.RequestWithPresentation(t.Context(), req, nil)
	if err != nil {
		t.Fatalf("presentation failure escaped H2 request: %v", err)
	}
	public, err := svc.ProjectPublic(t.Context(), state)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"shellbeam", "session", "attach", "--handoff-id", req.HandoffID}
	if !reflect.DeepEqual(public.AttachArgv, want) || state.Phase != handoff.PhaseHumanConnecting {
		t.Fatalf("public=%#v state=%#v", public, state)
	}
}

func TestRequestWithPresentationReplayReconcilesWithoutRefencing(t *testing.T) {
	store, runtime, fencer, _, calls, req := fixture(t)
	presenter := &fakeHandoffPresenter{store: store, calls: calls}
	svc := NewWithPresenter(store, runtime, fencer, presenter)
	if _, err := svc.RequestWithPresentation(t.Context(), req, nil); err != nil {
		t.Fatal(err)
	}
	*calls = nil
	if _, err := svc.RequestWithPresentation(t.Context(), req, nil); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*calls, []string{"find", "present"}) || len(presenter.seen) != 2 {
		t.Fatalf("replay calls=%v presentations=%d", *calls, len(presenter.seen))
	}
}

func TestExactHumanClientPresentRequiresDurableExactRefAndProviderProof(t *testing.T) {
	store, runtime, _, svc, _, req := fixture(t)
	if _, err := svc.Request(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	if got, err := svc.ExactHumanClientPresent(t.Context(), req.HandoffID); err != nil || got {
		t.Fatalf("unbound exact client got=%v err=%v", got, err)
	}
	state := store.state
	state.HumanClient = &handoff.HumanClientRef{Ref: "hclient_1"}
	state.ProviderOwner = delegated.OwnerNone
	store.state = state
	runtime.human = delegatedapp.HumanClientObservation{ClientRef: delegatedapp.ProviderClientRef{Ref: "hclient_1"}, Present: true, ReadOnly: true, ObservedOwner: delegated.OwnerNone, ProviderGeneration: state.ProviderGeneration}
	if got, err := svc.ExactHumanClientPresent(t.Context(), req.HandoffID); err != nil || !got {
		t.Fatalf("exact client got=%v err=%v", got, err)
	}
	runtime.human.ClientRef.Ref = "other_client"
	if got, err := svc.ExactHumanClientPresent(t.Context(), req.HandoffID); err != nil || got {
		t.Fatalf("mismatched client became proof got=%v err=%v", got, err)
	}
	runtime.human.ClientRef.Ref = "hclient_1"
	runtime.human.ProviderGeneration = "other_generation"
	if got, err := svc.ExactHumanClientPresent(t.Context(), req.HandoffID); err != nil || got {
		t.Fatalf("stale generation became proof got=%v err=%v", got, err)
	}
}

func presentationHint(t *testing.T) terminal.BridgeAffinityHint {
	t.Helper()
	identity := terminal.TerminalIdentity{ProviderID: "ghostty", ProviderVersion: 1, Platform: terminal.PlatformDarwin, BundleID: "com.mitchellh.ghostty", ExecutableName: "ghostty"}
	hint, err := terminal.NewBridgeAffinityHint(identity, time.Date(2026, 8, 19, 17, 0, 0, 0, time.UTC), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return hint
}
