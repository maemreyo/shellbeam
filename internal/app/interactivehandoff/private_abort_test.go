package interactivehandoff

import (
	"testing"

	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
)

func TestSecretAbortKeepsCapturePrivateCancelsWatcherAndResumeRepreparesBeforeWrite(t *testing.T) {
	_, runtime, _, svc, readiness, calls, req := secretFixture(t, true)
	readiness.recordClose = true
	if _, err := svc.Request(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AttachLocalHuman(t.Context(), req.HandoffID, delegatedapp.HumanAttachSpec{}); err != nil {
		t.Fatal(err)
	}
	firstWatcher := readiness.latest()
	aborted, err := svc.Abort(t.Context(), req.HandoffID)
	if err != nil {
		t.Fatal(err)
	}
	if aborted.Phase != handoff.PhaseAborted || aborted.DesiredOwner != delegated.OwnerNone || aborted.AgentIngress != handoff.IngressFenced || aborted.HumanIngress != handoff.IngressFenced || aborted.PrivacyRelease != handoff.PrivacyReleasePending || aborted.CaptureState != handoff.CapturePrivate || runtime.releases != 0 {
		t.Fatalf("aborted=%#v releases=%d", aborted, runtime.releases)
	}
	waitForWatcherClose(t, firstWatcher)
	assertCallsInOrder(t, *calls, []string{"fence_human", "prepare_readonly_control", "close_readiness", "advance:aborted"})

	runtime.human.Present = false // old local client disappeared while fenced
	*calls = nil
	resume := handoff.ControlSignal{HandoffID: req.HandoffID, AuthorityEpoch: aborted.AuthorityEpoch, ControlID: "secret-resume", Kind: handoff.HumanControlResume}
	resumed, err := svc.HumanControl(t.Context(), resume)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.State.Phase != handoff.PhaseHumanConnecting || resumed.State.PrivacyRelease != handoff.PrivacyReleasePending || resumed.State.CaptureState != handoff.CapturePrivate {
		t.Fatalf("resumed=%#v", resumed.State)
	}
	if readiness.count() != 2 {
		t.Fatalf("readiness prepare count=%d want 2", readiness.count())
	}
	if len(runtime.armEpochs) < 2 || runtime.armEpochs[len(runtime.armEpochs)-1] != resumed.State.AuthorityEpoch {
		t.Fatalf("privacy arm epochs=%v resumed_epoch=%d", runtime.armEpochs, resumed.State.AuthorityEpoch)
	}
	*calls = nil
	if _, err := svc.AttachLocalHuman(t.Context(), req.HandoffID, delegatedapp.HumanAttachSpec{}); err != nil {
		t.Fatal(err)
	}
	assertCallsInOrder(t, *calls, []string{"prove_private", "attach_human", "human_writable"})
}
