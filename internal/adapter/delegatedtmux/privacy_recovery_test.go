//go:build darwin

package delegatedtmux

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	core "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func TestNativePrivateObserverRestartReattachesPrivateFromFirstByte(t *testing.T) {
	p, _ := nativeProvider(t)
	ref, firstSink := createHumanNativeSession(t, p, "session_h4_private_recovery", nil)
	human := attachNativeHuman(t, p, ref, os.Environ())
	spec := app.PrivacySpec{HandoffID: "handoff-h4-recovery", AuthorityEpoch: 2}
	handle, err := p.ArmPrivateObservation(t.Context(), ref, spec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.ProvePrivateObservation(t.Context(), ref, handle); err != nil {
		t.Fatal(err)
	}
	if err := p.SetHumanWritable(t.Context(), ref, human.result.ClientRef, true); err != nil {
		t.Fatal(err)
	}

	p.mu.Lock()
	old := p.controls[ref.Ref]
	delete(p.controls, ref.Ref)
	p.mu.Unlock()
	if old == nil || !old.privateObservation {
		t.Fatalf("armed observer is not private: %#v", old)
	}
	_ = old.close()
	if _, err := human.master.Write([]byte("H4_SECRET_DURING_OBSERVER_GAP\n")); err != nil {
		t.Fatal(err)
	}

	secondSink := &nativeSink{}
	if _, err := p.Reattach(t.Context(), ref, secondSink); err != nil {
		t.Fatalf("private reattach: %s", nativeFailure(err))
	}
	p.mu.Lock()
	current := p.controls[ref.Ref]
	p.mu.Unlock()
	if current == nil || !current.privateObservation {
		t.Fatalf("replacement observer attached public: %#v", current)
	}
	if _, err := human.master.Write([]byte("H4_SECRET_AFTER_PRIVATE_RECONNECT\n")); err != nil {
		t.Fatal(err)
	}
	ensureNativeSinkAbsentFor(t, secondSink, "H4_SECRET_", 150*time.Millisecond)
	if strings.Contains(firstSink.String(), "H4_SECRET_") {
		t.Fatalf("old observer leaked private marker: %q", firstSink.String())
	}

	if err := p.SetHumanWritable(t.Context(), ref, human.result.ClientRef, false); err != nil {
		t.Fatal(err)
	}
	if err := p.ReleasePrivateObservation(t.Context(), ref, handle, nativePrivacyBoundary(spec, time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	if err := p.Write(t.Context(), ref, []byte("H4_PUBLIC_RECOVERED\n")); err != nil {
		t.Fatal(err)
	}
	waitContains(t, secondSink, "H4_PUBLIC_RECOVERED")
	if strings.Contains(secondSink.String(), "H4_SECRET_") {
		t.Fatalf("private history replayed on release: %q", secondSink.String())
	}
}

func TestNativePrivateArmRetryReplaysHandleWithoutReplacingPrivateObserver(t *testing.T) {
	p, _ := nativeProvider(t)
	ref, _ := createHumanNativeSession(t, p, "session_h4_private_retry", nil)
	spec := app.PrivacySpec{HandoffID: "handoff-h4-retry", AuthorityEpoch: 7}
	first, err := p.ArmPrivateObservation(t.Context(), ref, spec)
	if err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	observer := p.controls[ref.Ref]
	p.mu.Unlock()
	second, err := p.ArmPrivateObservation(t.Context(), ref, spec)
	if err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	after := p.controls[ref.Ref]
	p.mu.Unlock()
	if first != second || observer == nil || observer != after || !after.privateObservation {
		t.Fatalf("retry changed privacy identity/observer: first=%#v second=%#v before=%p after=%p", first, second, observer, after)
	}
	if err := p.ReleasePrivateObservation(t.Context(), ref, first, nativePrivacyBoundary(spec, time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
}

var _ context.Context

func TestNativePrivateObserverOverlapKeepsOldAndNewPrivate(t *testing.T) {
	p, _ := nativeProvider(t)
	ref, oldSink := createHumanNativeSession(t, p, "session_h4_private_overlap", nil)
	human := attachNativeHuman(t, p, ref, os.Environ())
	spec := app.PrivacySpec{HandoffID: "handoff-h4-overlap", AuthorityEpoch: 3}
	handle, err := p.ArmPrivateObservation(t.Context(), ref, spec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.ProvePrivateObservation(t.Context(), ref, handle); err != nil {
		t.Fatal(err)
	}
	if err := p.SetHumanWritable(t.Context(), ref, human.result.ClientRef, true); err != nil {
		t.Fatal(err)
	}
	state, err := p.state.load(ref.Ref)
	if err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	old := p.controls[ref.Ref]
	p.mu.Unlock()
	if old == nil || !old.isPrivateObservation() {
		t.Fatal("old private observer missing")
	}

	newSink := &nativeSink{}
	replacement, err := p.startPrivateControl(t.Context(), state.SocketPath, state.TmuxSession)
	if err != nil {
		t.Fatal(err)
	}
	defer replacement.close()
	facts, err := p.queryFacts(t.Context(), replacement, state.TmuxSession)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.verifyFacts(t.Context(), replacement, state, facts); err != nil {
		t.Fatal(err)
	}
	if err := replacement.setTarget(state.PaneID, newSink); err != nil {
		t.Fatal(err)
	}
	if !replacement.isPrivateObservation() {
		t.Fatal("new observer was not private from attach")
	}

	if _, err := human.master.Write([]byte("H4_SECRET_DURING_PRIVATE_OVERLAP\n")); err != nil {
		t.Fatal(err)
	}
	ensureNativeSinkAbsentFor(t, oldSink, "H4_SECRET_DURING_PRIVATE_OVERLAP", 100*time.Millisecond)
	ensureNativeSinkAbsentFor(t, newSink, "H4_SECRET_DURING_PRIVATE_OVERLAP", 100*time.Millisecond)

	if err := p.SetHumanWritable(t.Context(), ref, human.result.ClientRef, false); err != nil {
		t.Fatal(err)
	}
	if err := p.ReleasePrivateObservation(t.Context(), ref, handle, nativePrivacyBoundary(spec, time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
}

func TestNativePrivateActiveEpochRebindKeepsSameObserverPrivate(t *testing.T) {
	p, _ := nativeProvider(t)
	ref, _ := createHumanNativeSession(t, p, "session_h4_private_epoch_rebind", nil)
	firstSpec := app.PrivacySpec{HandoffID: "handoff-h4-epoch-rebind", AuthorityEpoch: 2}
	first, err := p.ArmPrivateObservation(t.Context(), ref, firstSpec)
	if err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	observer := p.controls[ref.Ref]
	p.mu.Unlock()
	if observer == nil || !observer.isPrivateObservation() {
		t.Fatal("initial observer not private")
	}
	secondSpec := app.PrivacySpec{HandoffID: firstSpec.HandoffID, AuthorityEpoch: 4}
	second, err := p.ArmPrivateObservation(t.Context(), ref, secondSpec)
	if err != nil {
		t.Fatalf("monotonic active epoch rebind failed: %v", err)
	}
	if second == first {
		t.Fatalf("epoch rebind reused stale handle: %#v", second)
	}
	p.mu.Lock()
	after := p.controls[ref.Ref]
	p.mu.Unlock()
	if after != observer || !after.isPrivateObservation() {
		t.Fatal("epoch rebind replaced or publicized private observer")
	}
	if _, err := p.ProvePrivateObservation(t.Context(), ref, first); err == nil {
		t.Fatal("stale pre-rebind privacy handle remained current")
	}
	if _, err := p.ProvePrivateObservation(t.Context(), ref, second); err != nil {
		t.Fatalf("rebound privacy handle not provable: %v", err)
	}
	if _, err := p.ArmPrivateObservation(t.Context(), ref, app.PrivacySpec{HandoffID: firstSpec.HandoffID, AuthorityEpoch: 3}); err == nil {
		t.Fatal("stale epoch rebind accepted")
	}
	if err := p.ReleasePrivateObservation(t.Context(), ref, second, nativePrivacyBoundary(secondSpec, time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
}

func TestNativePrivateObserverPreservesTerminalExitProof(t *testing.T) {
	p, _ := nativeProvider(t)
	ref := nativeRef(t, p, "session_h4_private_terminal")
	sink := &nativeSink{}
	spec := operation.ExecutionSpec{Mode: operation.ExecutionModeShell, Shell: "/bin/sh", Executable: "/bin/sh", Command: "IFS= read -r line; printf 'PRIVATE_DONE\\n'; exit 0", CWD: t.TempDir()}
	if _, err := p.Create(t.Context(), app.CreateRequest{ProviderRef: ref, SessionID: ref.SessionID, OperationID: "op_h4_private_terminal", Spec: spec, Output: sink}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close(context.Background(), ref) })
	privacy := app.PrivacySpec{HandoffID: "handoff-h4-private-terminal", AuthorityEpoch: 2}
	handle, err := p.ArmPrivateObservation(t.Context(), ref, privacy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.ProvePrivateObservation(t.Context(), ref, handle); err != nil {
		t.Fatal(err)
	}
	if err := p.Write(t.Context(), ref, []byte("finish\n")); err != nil {
		t.Fatal(err)
	}
	obs, err := p.Wait(t.Context(), ref)
	if err != nil {
		t.Fatalf("private terminal wait: %s", nativeFailure(err))
	}
	if !obs.Terminal || obs.ExitCode == nil || *obs.ExitCode != 0 || obs.Owner != core.OwnerNone {
		t.Fatalf("terminal=%#v", obs)
	}
	if strings.Contains(sink.String(), "PRIVATE_DONE") {
		t.Fatalf("private terminal output leaked: %q", sink.String())
	}
}

func TestNativeWaitStartedBeforePrivateSwapUsesReplacementObserverForTerminalProof(t *testing.T) {
	p, _ := nativeProvider(t)
	ref := nativeRef(t, p, "session_h4_wait_before_private_swap")
	sink := &nativeSink{}
	spec := operation.ExecutionSpec{Mode: operation.ExecutionModeShell, Shell: "/bin/sh", Executable: "/bin/sh", Command: "printf BEFORE; IFS= read -r line; exit 0", CWD: t.TempDir()}
	if _, err := p.Create(t.Context(), app.CreateRequest{ProviderRef: ref, SessionID: ref.SessionID, OperationID: "op_h4_wait_before_private_swap", Spec: spec, Output: sink}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close(context.Background(), ref) })

	type waitResult struct {
		obs app.Observation
		err error
	}
	waitContains(t, sink, "BEFORE")
	result := make(chan waitResult, 1)
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
	defer cancel()
	go func() {
		obs, err := p.Wait(ctx, ref)
		result <- waitResult{obs: obs, err: err}
	}()
	// Give Wait enough time to register against the original public observer.
	time.Sleep(50 * time.Millisecond)
	privacy := app.PrivacySpec{HandoffID: "handoff-h4-wait-before-swap", AuthorityEpoch: 2}
	if _, err := p.ArmPrivateObservation(t.Context(), ref, privacy); err != nil {
		t.Fatal(err)
	}
	if err := p.Write(t.Context(), ref, []byte("finish\n")); err != nil {
		t.Fatal(err)
	}
	got := <-result
	if got.err != nil {
		t.Fatalf("wait across private swap: %s", nativeFailure(got.err))
	}
	if !got.obs.Terminal || got.obs.ExitCode == nil || *got.obs.ExitCode != 0 {
		t.Fatalf("terminal=%#v", got.obs)
	}
	if got.obs.OutputBytes < int64(len("BEFORE")) {
		t.Fatalf("observer swap lost cumulative public output bytes: %#v", got.obs)
	}
}

func TestNativeReleasedPrivacyAllowsOnlyNewerDistinctHandoffToRearm(t *testing.T) {
	p, _ := nativeProvider(t)
	ref, _ := createHumanNativeSession(t, p, "session_h4_private_rearm", nil)
	firstSpec := app.PrivacySpec{HandoffID: "handoff-h4-rearm-one", AuthorityEpoch: 2}
	first, err := p.ArmPrivateObservation(t.Context(), ref, firstSpec)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.ReleasePrivateObservation(t.Context(), ref, first, nativePrivacyBoundary(firstSpec, time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	if _, err := p.ArmPrivateObservation(t.Context(), ref, firstSpec); err == nil {
		t.Fatal("released handoff was resurrected by replayed arm")
	}
	if _, err := p.ArmPrivateObservation(t.Context(), ref, app.PrivacySpec{HandoffID: "handoff-h4-rearm-stale", AuthorityEpoch: 2}); err == nil {
		t.Fatal("distinct handoff with non-newer epoch rearmed privacy")
	}
	secondSpec := app.PrivacySpec{HandoffID: "handoff-h4-rearm-two", AuthorityEpoch: 4}
	second, err := p.ArmPrivateObservation(t.Context(), ref, secondSpec)
	if err != nil {
		t.Fatalf("newer distinct handoff could not rearm privacy: %v", err)
	}
	if second == first {
		t.Fatalf("new handoff reused released handle: %#v", second)
	}
	if _, err := p.ProvePrivateObservation(t.Context(), ref, second); err != nil {
		t.Fatal(err)
	}
	if err := p.ReleasePrivateObservation(t.Context(), ref, second, nativePrivacyBoundary(secondSpec, time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
}
