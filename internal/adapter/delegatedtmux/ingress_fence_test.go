//go:build darwin

package delegatedtmux

import (
	"os"
	"strings"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
)

func TestNativeHumanFenceRejectsNewIngressAfterProof(t *testing.T) {
	p, _ := nativeProvider(t)
	ref, sink := createHumanNativeSession(t, p, "session_human_fence", nil)
	human := attachNativeHuman(t, p, ref, os.Environ())
	if err := p.SetHumanWritable(t.Context(), ref, human.result.ClientRef, true); err != nil {
		t.Fatal(err)
	}
	if _, err := human.master.Write([]byte("H2_PRE_FENCE\n")); err != nil {
		t.Fatal(err)
	}
	waitContains(t, sink, "H2_PRE_FENCE")
	proof, err := p.FenceHumanIngress(t.Context(), ref, human.result.ClientRef, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !proof.Fenced || proof.AuthorityEpoch != 2 || proof.ProviderGeneration == "" || proof.ClientRef != human.result.ClientRef {
		t.Fatalf("proof=%#v", proof)
	}
	if _, err := human.master.Write([]byte("H2_POST_FENCE\n")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if strings.Contains(sink.String(), "H2_POST_FENCE") {
		t.Fatalf("post-fence bytes reached pane: %q", sink.String())
	}
	obs, err := p.InspectHumanClient(t.Context(), ref, human.result.ClientRef)
	if err != nil || !obs.ReadOnly || obs.ObservedOwner != core.OwnerNone {
		t.Fatalf("obs=%#v err=%v", obs, err)
	}
}
