package daemon_test

import (
	"context"
	"errors"
	"testing"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
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
	svc := app.NewService(store, &fakeOwner{}, app.Options{Incarnation: "h4-daemon", Shell: "/bin/sh", MaxQueuedInputBytes: 100, DelegatedRuntime: runtime, Capabilities: h4DaemonCapabilities()})
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

func TestDaemonPrivacyProviderDoesNotBypassH4CapabilityPolicy(t *testing.T) {
	store := openDelegatedStartStore(t)
	runtime := &h4DaemonRuntime{h2LocalDaemonRuntime: &h2LocalDaemonRuntime{delegatedStartRuntime: newDelegatedStartRuntime()}}
	svc := app.NewService(store, &fakeOwner{}, app.Options{Incarnation: "h4-policy-daemon", Shell: "/bin/sh", MaxQueuedInputBytes: 100, DelegatedRuntime: runtime, Capabilities: capability.Baseline(capability.Limits{}).WithDelegatedInteractive(capability.DelegatedInteractiveSupport{ProviderID: "tmux_control_mode", ProviderVersion: 1, Platform: "darwin", MaxMutationRecords: 4096}).WithInteractiveHandoff(capability.InteractiveHandoffSupport{ManualStandard: true})})
	started, err := svc.Start(t.Context(), delegatedStartRequest())
	if err != nil {
		t.Fatal(err)
	}
	req := handoff.Request{HandoffID: "handoff-daemon-secret-policy", SessionID: started.SessionID, Reason: handoff.ReasonCredentialRequired, Privacy: handoff.PrivacySecret, Completion: handoff.Completion{Kind: handoff.CompletionManualReady}}
	if _, err := svc.RequestHandoff(t.Context(), req); !errors.Is(err, failure.FeatureUnavailable) {
		t.Fatalf("unadvertised H4 secret request err=%v", err)
	}
	if runtime.arm != 0 || runtime.prove != 0 || runtime.release != 0 {
		t.Fatalf("unadvertised H4 mutated privacy provider: arm=%d prove=%d release=%d", runtime.arm, runtime.prove, runtime.release)
	}
}

func h4DaemonCapabilities() capability.Catalog {
	catalog := capability.Baseline(capability.Limits{}).WithDelegatedInteractive(capability.DelegatedInteractiveSupport{
		ProviderID: "tmux_control_mode", ProviderVersion: 1, Platform: "darwin", MaxMutationRecords: 4096,
	})
	return catalog.WithInteractiveHandoff(capability.InteractiveHandoffSupport{
		ManualStandard: true,
		Secret:         true,
		Privacy: &capability.HandoffPrivacySupport{
			SecretPrivateInterval: true, PrivacyReleaseSeparate: true, ObserverTopologyQualified: true, HumanInputPersisted: false,
		},
		CaptureQualities: []receipt.CaptureQuality{receipt.CaptureComplete, receipt.CapturePartial, receipt.CaptureIncomplete},
	})
}
