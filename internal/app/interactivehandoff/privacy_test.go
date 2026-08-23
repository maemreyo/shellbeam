package interactivehandoff

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
	shellcore "github.com/maemreyo/shellbeam/internal/core/shellintegration"
)

type h4Runtime struct {
	*fakeRuntime
	mu            sync.Mutex
	armErr        error
	proveErr      error
	releaseErr    error
	currentSpec   delegatedapp.PrivacySpec
	currentHandle delegatedapp.PrivacyHandle
	lastBoundary  delegatedapp.ForwardBoundary
	armEpochs     []delegated.AuthorityEpoch
	releases      int
}

func (r *h4Runtime) AttachHuman(ctx context.Context, ref delegated.ProviderRef, spec delegatedapp.HumanAttachSpec) (delegatedapp.HumanAttachResult, error) {
	result, err := r.fakeRuntime.AttachHuman(ctx, ref, spec)
	if err == nil {
		r.human.Present = true
		r.human.ClientRef = result.ClientRef
		r.human.ReadOnly = true
		r.human.ObservedOwner = delegated.OwnerNone
	}
	return result, err
}

func (r *h4Runtime) ArmPrivateObservation(_ context.Context, _ delegated.ProviderRef, spec delegatedapp.PrivacySpec) (delegatedapp.PrivacyHandle, error) {
	r.call("arm_private")
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.armErr != nil {
		return delegatedapp.PrivacyHandle{}, r.armErr
	}
	r.currentSpec = spec
	r.currentHandle = delegatedapp.PrivacyHandle{OpaqueRef: fmt.Sprintf("privacy_epoch_%d", spec.AuthorityEpoch), Generation: "gen-h2"}
	r.armEpochs = append(r.armEpochs, spec.AuthorityEpoch)
	return r.currentHandle, nil
}

func (r *h4Runtime) ProvePrivateObservation(_ context.Context, _ delegated.ProviderRef, handle delegatedapp.PrivacyHandle) (delegatedapp.PrivateObservationProof, error) {
	r.call("prove_private")
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.proveErr != nil {
		return delegatedapp.PrivateObservationProof{}, r.proveErr
	}
	if handle != r.currentHandle {
		return delegatedapp.PrivateObservationProof{}, failure.New(failure.PrivateOutputBarrierFailed, map[string]string{"reason": "stale_test_handle"}, nil)
	}
	return delegatedapp.PrivateObservationProof{Handle: handle, ProviderGeneration: "gen-h2", PrivateFromFirstByte: true, ObservedAt: time.Now().UTC()}, nil
}

func (r *h4Runtime) ReleasePrivateObservation(_ context.Context, _ delegated.ProviderRef, handle delegatedapp.PrivacyHandle, boundary delegatedapp.ForwardBoundary) error {
	r.call("release_private")
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.releaseErr != nil {
		return r.releaseErr
	}
	if handle != r.currentHandle {
		return failure.New(failure.PrivacyReleaseUnproven, map[string]string{"reason": "stale_test_handle"}, nil)
	}
	if err := boundary.ValidateFor(r.currentSpec); err != nil {
		return failure.New(failure.PrivacyReleaseUnproven, nil, err)
	}
	r.lastBoundary = boundary
	r.releases++
	return nil
}

func (s *fakeStore) MarkPrivateCapture(_ context.Context, _ string) error {
	s.call("mark_private_capture")
	return nil
}

type failingPrivateCaptureStore struct {
	*fakeStore
	err error
}

func (s *failingPrivateCaptureStore) MarkPrivateCapture(context.Context, string) error { return s.err }

func secretFixture(t *testing.T, automatic bool) (*fakeStore, *h4Runtime, *fakeFencer, *Service, *fakeReadinessPreparer, *[]string, handoff.Request) {
	t.Helper()
	store, baseRuntime, fencer, _, calls, req := fixture(t)
	runtime := &h4Runtime{fakeRuntime: baseRuntime}
	svc := New(store, runtime, fencer)
	svc.EnableH4()
	readiness := newFakeReadinessPreparer(calls)
	if automatic {
		svc.SetReadiness(readiness)
		req.Completion = handoff.Completion{Kind: handoff.CompletionEnvironmentExportedNonempty, Name: "CONTROL_PLANE_API_KEY"}
		req.Reason = handoff.ReasonCredentialRequired
	}
	req.Privacy = handoff.PrivacySecret
	return store, runtime, fencer, svc, readiness, calls, req
}

func TestSecretAutomaticStartOrdersFenceReadinessPrivateProofBeforeHumanWrite(t *testing.T) {
	_, _, _, svc, _, calls, req := secretFixture(t, true)
	state, err := svc.Request(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if state.Phase != handoff.PhaseHumanConnecting || state.PrivacyState != handoff.PrivacyPrivate || state.PrivacyRelease != handoff.PrivacyReleasePending || state.CaptureState != handoff.CapturePrivate {
		t.Fatalf("request state=%#v", state)
	}
	if _, err := svc.AttachLocalHuman(t.Context(), req.HandoffID, delegatedapp.HumanAttachSpec{}); err != nil {
		t.Fatal(err)
	}
	wantOrder := []string{"reserve", "fence_agent", "prepare_readiness", "arm_private", "prove_private", "mark_private_capture", "attach_human", "human_writable", "advance:human_owned"}
	assertCallsInOrder(t, *calls, wantOrder)
}

func TestSecretPrivacyArmFailureNeverAllowsHumanWriteEvenAfterPartialRequest(t *testing.T) {
	store, runtime, _, svc, _, calls, req := secretFixture(t, false)
	runtime.armErr = failure.New(failure.PrivateOutputBarrierFailed, map[string]string{"reason": "test_arm"}, nil)
	if _, err := svc.Request(t.Context(), req); !errors.Is(err, failure.PrivateOutputBarrierFailed) {
		t.Fatalf("request err=%v", err)
	}
	if store.state.Phase != handoff.PhaseHumanConnecting || store.state.AgentIngress != handoff.IngressFenced || store.state.HumanIngress != handoff.IngressFenced {
		t.Fatalf("partial state=%#v", store.state)
	}
	if _, err := svc.AttachLocalHuman(t.Context(), req.HandoffID, delegatedapp.HumanAttachSpec{}); !errors.Is(err, failure.PrivateOutputBarrierFailed) {
		t.Fatalf("attach after failed arm err=%v", err)
	}
	for _, call := range *calls {
		if call == "human_writable" {
			t.Fatalf("privacy failure allowed human write: %v", *calls)
		}
	}
}

func TestSecretPrivateProofRequiresDurableCaptureOmissionMarker(t *testing.T) {
	store, runtime, fencer, _, _, _, req := secretFixture(t, false)
	wrapped := &failingPrivateCaptureStore{fakeStore: store, err: errors.New("capture marker unavailable")}
	svc := New(wrapped, runtime, fencer)
	svc.EnableH4()
	if _, err := svc.Request(t.Context(), req); !errors.Is(err, failure.PrivateOutputBarrierFailed) {
		t.Fatalf("capture marker failure err=%v", err)
	}
	if store.state.HumanIngress == handoff.IngressWritable {
		t.Fatalf("capture marker failure exposed human ingress: %#v", store.state)
	}
}

func TestSecretManualReadyTransfersAuthorityButNeverReleasesPrivateCapture(t *testing.T) {
	_, runtime, _, svc, _, _, req := secretFixture(t, false)
	if _, err := svc.Request(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	attached, err := svc.AttachLocalHuman(t.Context(), req.HandoffID, delegatedapp.HumanAttachSpec{})
	if err != nil {
		t.Fatal(err)
	}
	sig := handoff.ControlSignal{HandoffID: req.HandoffID, AuthorityEpoch: attached.State.AuthorityEpoch, ControlID: "secret-manual-ready", Kind: handoff.HumanControlReady}
	result, err := svc.HumanControl(t.Context(), sig)
	if err != nil {
		t.Fatal(err)
	}
	state := result.State
	if state.Phase != handoff.PhaseAgentOwned || state.TransferBoundary.Kind != handoff.BoundaryHumanAttested || state.PrivacyState != handoff.PrivacyPrivate || state.PrivacyRelease != handoff.PrivacyReleasePending || state.CaptureState != handoff.CapturePrivate {
		t.Fatalf("manual secret ready state=%#v", state)
	}
	if runtime.releases != 0 {
		t.Fatalf("manual ready released private capture: %d", runtime.releases)
	}
}

func assertCallsInOrder(t *testing.T, calls, want []string) {
	t.Helper()
	at := 0
	for _, call := range calls {
		if at < len(want) && call == want[at] {
			at++
		}
	}
	if at != len(want) {
		t.Fatalf("calls=%v missing ordered suffix from %v at %q", calls, want, want[at])
	}
}

func assertCallBefore(t *testing.T, calls []string, first, second string) {
	t.Helper()
	a, b := -1, -1
	for i, call := range calls {
		if call == first && a < 0 {
			a = i
		}
		if call == second && b < 0 {
			b = i
		}
	}
	if a < 0 || b < 0 || a >= b {
		t.Fatalf("calls=%v want %q before %q", calls, first, second)
	}
}

var _ Runtime = (*h4Runtime)(nil)
var _ = reflect.DeepEqual
var _ = shellcore.ShellFish
