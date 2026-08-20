package daemon_test

import (
	"context"
	"testing"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

type h4DaemonRuntime struct {
	*h2LocalDaemonRuntime
	arm, prove, release int
	handle              delegatedapp.PrivacyHandle
	spec                delegatedapp.PrivacySpec
}

func (r *h4DaemonRuntime) ArmPrivateObservation(_ context.Context, _ delegated.ProviderRef, spec delegatedapp.PrivacySpec) (delegatedapp.PrivacyHandle, error) {
	r.arm++
	r.spec = spec
	r.handle = delegatedapp.PrivacyHandle{OpaqueRef: "privacy_daemon_h4", Generation: "gen_test"}
	return r.handle, nil
}
func (r *h4DaemonRuntime) ProvePrivateObservation(context.Context, delegated.ProviderRef, delegatedapp.PrivacyHandle) (delegatedapp.PrivateObservationProof, error) {
	r.prove++
	return delegatedapp.PrivateObservationProof{Handle: r.handle, ProviderGeneration: "gen_test", PrivateFromFirstByte: true, ObservedAt: time.Now().UTC()}, nil
}
func (r *h4DaemonRuntime) ReleasePrivateObservation(context.Context, delegated.ProviderRef, delegatedapp.PrivacyHandle, delegatedapp.ForwardBoundary) error {
	r.release++
	return nil
}

func TestDaemonSecretManualHandoffUsesPrivateBarrierAndMarksCaptureTruth(t *testing.T) {
	store := openDelegatedStartStore(t)
	base := &h2LocalDaemonRuntime{delegatedStartRuntime: newDelegatedStartRuntime()}
	runtime := &h4DaemonRuntime{h2LocalDaemonRuntime: base}
	svc := app.NewService(store, &fakeOwner{}, app.Options{Incarnation: "h4-daemon", Shell: "/bin/sh", MaxQueuedInputBytes: 100, DelegatedRuntime: runtime})
	started, err := svc.Start(t.Context(), delegatedStartRequest())
	if err != nil {
		t.Fatal(err)
	}
	req := handoff.Request{HandoffID: "handoff-daemon-secret", SessionID: started.SessionID, Reason: handoff.ReasonCredentialRequired, Privacy: handoff.PrivacySecret, Completion: handoff.Completion{Kind: handoff.CompletionManualReady}}
	state, err := svc.RequestHandoff(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if state.PrivacyState != handoff.PrivacyPrivate || state.CaptureState != handoff.CapturePrivate || runtime.arm != 1 || runtime.prove != 1 {
		t.Fatalf("state=%#v privacy calls arm=%d prove=%d", state, runtime.arm, runtime.prove)
	}
	truth, err := store.LoadDelegatedCaptureTruth(t.Context(), operation.SessionID(started.SessionID))
	if err != nil {
		t.Fatal(err)
	}
	if truth.Quality != receipt.CapturePartial || truth.OutputComplete || len(truth.Reasons) != 1 || truth.Reasons[0] != receipt.CaptureReasonPrivateIntervalsOmitted {
		t.Fatalf("capture truth=%#v", truth)
	}
}
