//go:build darwin

package delegatedtmux

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
)

func TestNativeWritableHumanControlIsOOBAndReadOnlyDetachRemainsReachable(t *testing.T) {
	p, _ := nativeProvider(t)
	ref, sink := createHumanNativeSession(t, p, "session_human_control", nil)
	human := attachNativeHuman(t, p, ref, os.Environ())
	if err := p.SetHumanWritable(t.Context(), ref, human.result.ClientRef, true); err != nil {
		t.Fatal(err)
	}
	spec := app.HumanControlSpec{HandoffID: "handoff-human-control", AuthorityEpoch: 2}
	if err := p.ArmWritableHumanControl(t.Context(), ref, human.result.ClientRef, spec); err != nil {
		t.Fatal(err)
	}
	result := make(chan struct {
		kind handoff.HumanControlKind
		err  error
	}, 1)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	go func() {
		kind, err := p.WaitWritableHumanControl(ctx, ref, human.result.ClientRef, spec)
		result <- struct {
			kind handoff.HumanControlKind
			err  error
		}{kind, err}
	}()
	before := sink.String()
	if _, err := human.master.Write([]byte("\x1b[24~")); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-result:
		if got.err != nil || got.kind != handoff.HumanControlReady {
			t.Fatalf("control=%q err=%v", got.kind, got.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ready control not observed")
	}
	if strings.Contains(strings.TrimPrefix(sink.String(), before), "\x1b[24~") {
		t.Fatal("control sequence reached pane")
	}
	if _, err := p.FenceHumanIngress(t.Context(), ref, human.result.ClientRef, 2); err != nil {
		t.Fatal(err)
	}
	if err := p.PrepareReadOnlyLocalControl(t.Context(), ref, human.result.ClientRef); err != nil {
		t.Fatal(err)
	}
	if _, err := human.master.Write([]byte("\x1b[24~")); err != nil {
		t.Fatal(err)
	}
	waitHumanDone(t, human.result.Done)
	if _, err := p.InspectHumanClient(t.Context(), ref, human.result.ClientRef); !errors.Is(err, failure.HandoffClientLost) {
		t.Fatalf("detached human err=%v want handoff_client_lost", err)
	}
}
