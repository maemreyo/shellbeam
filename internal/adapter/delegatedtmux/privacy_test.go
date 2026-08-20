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
	shell "github.com/maemreyo/shellbeam/internal/core/shellintegration"
)

func TestNativePrivacyFirstHumanByteIsNeverModelVisibleAndReleaseIsForwardOnly(t *testing.T) {
	p, _ := nativeProvider(t)
	ref, sink := createHumanNativeSession(t, p, "session_h4_private_first", nil)
	human := attachNativeHuman(t, p, ref, os.Environ())
	spec := app.PrivacySpec{HandoffID: "handoff-h4-first", AuthorityEpoch: 2}

	handle, err := p.ArmPrivateObservation(t.Context(), ref, spec)
	if err != nil {
		t.Fatalf("arm private: %s", nativeFailure(err))
	}
	proof, err := p.ProvePrivateObservation(t.Context(), ref, handle)
	if err != nil {
		t.Fatalf("prove private: %s", nativeFailure(err))
	}
	if !proof.PrivateFromFirstByte || proof.ProviderGeneration == "" || proof.Handle != handle {
		t.Fatalf("private proof=%#v", proof)
	}
	if err := p.SetHumanWritable(t.Context(), ref, human.result.ClientRef, true); err != nil {
		t.Fatal(err)
	}
	if _, err := human.master.Write([]byte("H4_SECRET_FIRST_BYTE\n")); err != nil {
		t.Fatal(err)
	}
	ensureNativeSinkAbsentFor(t, sink, "H4_SECRET_FIRST_BYTE", 150*time.Millisecond)
	if err := p.SetHumanWritable(t.Context(), ref, human.result.ClientRef, false); err != nil {
		t.Fatal(err)
	}

	boundary := nativePrivacyBoundary(spec, time.Now().UTC())
	if err := p.ReleasePrivateObservation(t.Context(), ref, handle, boundary); err != nil {
		t.Fatalf("release private: %s", nativeFailure(err))
	}
	if err := p.Write(t.Context(), ref, []byte("H4_PUBLIC_AFTER_RELEASE\n")); err != nil {
		t.Fatal(err)
	}
	waitContains(t, sink, "H4_PUBLIC_AFTER_RELEASE")
	if strings.Contains(sink.String(), "H4_SECRET_FIRST_BYTE") {
		t.Fatalf("private history replayed after release: %q", sink.String())
	}
}

func TestNativePrivacyMultiSessionIsolationKeepsPublicBCVisible(t *testing.T) {
	p, _ := nativeProvider(t)
	refA, sinkA := createHumanNativeSession(t, p, "session_h4_private_a", nil)
	refB, sinkB := createHumanNativeSession(t, p, "session_h4_public_b", nil)
	refC, sinkC := createHumanNativeSession(t, p, "session_h4_public_c", nil)
	humanA := attachNativeHuman(t, p, refA, os.Environ())
	spec := app.PrivacySpec{HandoffID: "handoff-h4-multi", AuthorityEpoch: 2}
	handle, err := p.ArmPrivateObservation(t.Context(), refA, spec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.ProvePrivateObservation(t.Context(), refA, handle); err != nil {
		t.Fatal(err)
	}
	if err := p.SetHumanWritable(t.Context(), refA, humanA.result.ClientRef, true); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if _, err := humanA.master.Write([]byte("A_SECRET_PRIVATE\n")); err != nil {
			t.Fatal(err)
		}
		if err := p.Write(t.Context(), refB, []byte("B_PUBLIC_"+string(rune('a'+i))+"\n")); err != nil {
			t.Fatal(err)
		}
		if err := p.Write(t.Context(), refC, []byte("C_PUBLIC_"+string(rune('a'+i))+"\n")); err != nil {
			t.Fatal(err)
		}
	}
	waitContains(t, sinkB, "B_PUBLIC_h")
	waitContains(t, sinkC, "C_PUBLIC_h")
	ensureNativeSinkAbsentFor(t, sinkA, "A_SECRET_PRIVATE", 150*time.Millisecond)
	if err := p.SetHumanWritable(t.Context(), refA, humanA.result.ClientRef, false); err != nil {
		t.Fatal(err)
	}
	if err := p.ReleasePrivateObservation(t.Context(), refA, handle, nativePrivacyBoundary(spec, time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
}

func nativePrivacyBoundary(spec app.PrivacySpec, at time.Time) app.ForwardBoundary {
	return app.ForwardBoundary{Proof: shell.PrivacyReleaseProof{
		HandoffID: spec.HandoffID, AuthorityEpoch: spec.AuthorityEpoch,
		Shell:    shell.ShellIdentity{Family: shell.ShellBash, RuntimeID: "runtime-h4-test"},
		Boundary: "forward-boundary-h4", ForwardOnly: true, ObservedAt: at,
	}}
}

func ensureNativeSinkAbsentFor(t *testing.T, sink *nativeSink, marker string, duration time.Duration) {
	t.Helper()
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		if strings.Contains(sink.String(), marker) {
			t.Fatalf("private marker became model-visible: %q", sink.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

var _ core.ProviderRef
var _ context.Context
