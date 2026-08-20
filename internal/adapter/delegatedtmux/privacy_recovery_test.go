//go:build darwin

package delegatedtmux

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
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
