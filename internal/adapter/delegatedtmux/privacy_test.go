//go:build darwin

package delegatedtmux

import (
	"context"
	"fmt"
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

func TestNativePrivacyHundredArmReleaseReconnectCyclesKeepPublicNeighborsVisible(t *testing.T) {
	p, _ := nativeProvider(t)
	refA, sinkA := createHumanNativeSession(t, p, "session_h4_cycle_private_a", nil)
	refB, sinkB := createHumanNativeSession(t, p, "session_h4_cycle_public_b", nil)
	refC, sinkC := createHumanNativeSession(t, p, "session_h4_cycle_public_c", nil)
	humanA := attachNativeHuman(t, p, refA, os.Environ())

	for i := 0; i < 100; i++ {
		spec := app.PrivacySpec{HandoffID: fmt.Sprintf("handoff-h4-cycle-%03d", i), AuthorityEpoch: core.AuthorityEpoch(i + 2)}
		handle, err := p.ArmPrivateObservation(t.Context(), refA, spec)
		if err != nil {
			t.Fatalf("cycle %d arm: %v", i, err)
		}
		if _, err := p.ProvePrivateObservation(t.Context(), refA, handle); err != nil {
			t.Fatalf("cycle %d prove: %v", i, err)
		}
		if err := p.Detach(t.Context(), refA); err != nil {
			t.Fatalf("cycle %d detach: %v", i, err)
		}
		if _, err := p.Reattach(t.Context(), refA, sinkA); err != nil {
			t.Fatalf("cycle %d private reattach: %v", i, err)
		}
		if _, err := p.ProvePrivateObservation(t.Context(), refA, handle); err != nil {
			t.Fatalf("cycle %d reprove after reconnect: %v", i, err)
		}
		if err := p.SetHumanWritable(t.Context(), refA, humanA.result.ClientRef, true); err != nil {
			t.Fatalf("cycle %d human writable: %v", i, err)
		}
		if _, err := humanA.master.Write([]byte(fmt.Sprintf("A_SECRET_CYCLE_%03d\n", i))); err != nil {
			t.Fatalf("cycle %d private write: %v", i, err)
		}
		if err := p.Write(t.Context(), refB, []byte(fmt.Sprintf("B_PUBLIC_CYCLE_%03d\n", i))); err != nil {
			t.Fatalf("cycle %d B write: %v", i, err)
		}
		if err := p.Write(t.Context(), refC, []byte(fmt.Sprintf("C_PUBLIC_CYCLE_%03d\n", i))); err != nil {
			t.Fatalf("cycle %d C write: %v", i, err)
		}
		if err := p.SetHumanWritable(t.Context(), refA, humanA.result.ClientRef, false); err != nil {
			t.Fatalf("cycle %d human fence: %v", i, err)
		}
		if err := p.ReleasePrivateObservation(t.Context(), refA, handle, nativePrivacyBoundary(spec, time.Now().UTC())); err != nil {
			t.Fatalf("cycle %d release: %v", i, err)
		}
		if strings.Contains(sinkA.String(), "A_SECRET_CYCLE_") {
			t.Fatalf("cycle %d replayed/leaked private history: %q", i, sinkA.String())
		}
		publicA := fmt.Sprintf("A_PUBLIC_CYCLE_%03d\n", i)
		if err := p.Write(t.Context(), refA, []byte(publicA)); err != nil {
			t.Fatalf("cycle %d A public write: %v", i, err)
		}
		waitContains(t, sinkA, strings.TrimSpace(publicA))
	}
	waitContains(t, sinkB, "B_PUBLIC_CYCLE_099")
	waitContains(t, sinkC, "C_PUBLIC_CYCLE_099")
	if got := strings.Count(sinkB.String(), "B_PUBLIC_CYCLE_"); got != 100 {
		t.Fatalf("public B suppressed/lost markers: got=%d", got)
	}
	if got := strings.Count(sinkC.String(), "C_PUBLIC_CYCLE_"); got != 100 {
		t.Fatalf("public C suppressed/lost markers: got=%d", got)
	}
	if strings.Contains(sinkA.String(), "A_SECRET_CYCLE_") {
		t.Fatalf("private A leaked after 100 cycles: %q", sinkA.String())
	}
}
