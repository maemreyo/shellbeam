//go:build darwin

package delegatedtmux

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	app "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	core "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

type nativeHuman struct {
	master *os.File
	result app.HumanAttachResult
}

func attachNativeHuman(t *testing.T, p *Provider, ref core.ProviderRef, env []string) nativeHuman {
	t.Helper()
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	if err := pty.Setsize(slave, &pty.Winsize{Rows: 24, Cols: 100}); err != nil {
		t.Fatal(err)
	}
	result, err := p.AttachHuman(t.Context(), ref, app.HumanAttachSpec{Stdin: slave, Stdout: slave, Stderr: slave, Environment: env})
	_ = slave.Close()
	if err != nil {
		_ = master.Close()
		t.Fatalf("attach human: %s", nativeFailure(err))
	}
	t.Cleanup(func() { _ = master.Close() })
	return nativeHuman{master: master, result: result}
}

func createHumanNativeSession(t *testing.T, p *Provider, sessionID string, additions []operation.EnvironmentEntry) (core.ProviderRef, *nativeSink) {
	t.Helper()
	ref := nativeRef(t, p, sessionID)
	sink := &nativeSink{}
	command := `stty -echo; printf HUMAN_READY; while IFS= read -r line; do case "$line" in H2_ENV_A) printf 'H2_ENV_A:%s\\n' "$SHELLBEAM_H2_SENTINEL" ;; H2_ENV_B) printf 'H2_ENV_B:%s\\n' "$SHELLBEAM_H2_SENTINEL" ;; *) printf '%s\\n' "$line" ;; esac; done`
	spec := operation.ExecutionSpec{Mode: operation.ExecutionModeShell, Shell: "/bin/sh", Executable: "/bin/sh", Command: command, CWD: t.TempDir(), EnvironmentAdditions: additions}
	if _, err := p.Create(t.Context(), app.CreateRequest{ProviderRef: ref, SessionID: ref.SessionID, OperationID: "op_" + sessionID, Spec: spec, Output: sink}); err != nil {
		t.Fatalf("create: %s", nativeFailure(err))
	}
	t.Cleanup(func() { _ = p.Close(context.Background(), ref) })
	waitContains(t, sink, "HUMAN_READY")
	return ref, sink
}

func TestNativeHumanAttachStartsReadOnlyAndPreservesSessionEnvironment(t *testing.T) {
	p, _ := nativeProvider(t)
	ref, sink := createHumanNativeSession(t, p, "session_human_env", []operation.EnvironmentEntry{{Key: "SHELLBEAM_H2_SENTINEL", Value: "session_value"}})
	if err := p.Write(t.Context(), ref, []byte("H2_ENV_A\n")); err != nil {
		t.Fatal(err)
	}
	waitContains(t, sink, "H2_ENV_A:session_value")
	human := attachNativeHuman(t, p, ref, append(os.Environ(), "SHELLBEAM_H2_SENTINEL=attach_value"))
	obs, err := p.InspectHumanClient(t.Context(), ref, human.result.ClientRef)
	if err != nil {
		t.Fatal(err)
	}
	if !obs.Present || !obs.ReadOnly || obs.ObservedOwner != core.OwnerNone || obs.ProviderGeneration == "" {
		t.Fatalf("human=%#v", obs)
	}
	state, err := p.state.load(ref.Ref)
	if err != nil {
		t.Fatal(err)
	}
	privateClient, err := p.loadHumanClientState(state, human.result.ClientRef)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(p.humanClientPath(state, human.result.ClientRef))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("human client state mode=%#o", info.Mode().Perm())
	}
	if strings.Contains(human.result.ClientRef.Ref, privateClient.ClientName) || strings.Contains(human.result.ClientRef.Ref, privateClient.TTY) {
		t.Fatalf("opaque client ref leaked provider identity: ref=%q state=%#v", human.result.ClientRef.Ref, privateClient)
	}
	if err := p.Write(t.Context(), ref, []byte("H2_ENV_B\n")); err != nil {
		t.Fatal(err)
	}
	waitContains(t, sink, "H2_ENV_B:session_value")
}

func TestNativeHumanExactClientWritabilityIsolation(t *testing.T) {
	p, _ := nativeProvider(t)
	ref, _ := createHumanNativeSession(t, p, "session_human_isolation", nil)
	first := attachNativeHuman(t, p, ref, os.Environ())
	second := attachNativeHuman(t, p, ref, os.Environ())
	if first.result.ClientRef == second.result.ClientRef {
		t.Fatal("distinct human clients got same ref")
	}
	if err := p.SetHumanWritable(t.Context(), ref, first.result.ClientRef, true); err != nil {
		t.Fatal(err)
	}
	one, err := p.InspectHumanClient(t.Context(), ref, first.result.ClientRef)
	if err != nil {
		t.Fatal(err)
	}
	two, err := p.InspectHumanClient(t.Context(), ref, second.result.ClientRef)
	if err != nil {
		t.Fatal(err)
	}
	if one.ReadOnly || one.ObservedOwner != core.OwnerHuman || !two.ReadOnly {
		t.Fatalf("first=%#v second=%#v", one, two)
	}
	if err := p.SetHumanWritable(t.Context(), ref, first.result.ClientRef, false); err != nil {
		t.Fatal(err)
	}
}

func TestNativeHumanClientRejectsProviderGenerationMismatch(t *testing.T) {
	p, _ := nativeProvider(t)
	ref, _ := createHumanNativeSession(t, p, "session_human_generation", nil)
	human := attachNativeHuman(t, p, ref, os.Environ())
	state, err := p.state.load(ref.Ref)
	if err != nil {
		t.Fatal(err)
	}
	state.ProviderGeneration = "gen_forged_h2"
	state.UpdatedAt = state.UpdatedAt.Add(time.Second)
	if err := p.state.save(state); err != nil {
		t.Fatal(err)
	}
	if _, err := p.InspectHumanClient(t.Context(), ref, human.result.ClientRef); !errors.Is(err, failure.DelegatedProviderMismatch) {
		t.Fatalf("generation mismatch err=%v want delegated_provider_mismatch", err)
	}
}

func waitHumanDone(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("human attach exit: %v", err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("human attach did not exit")
	}
}
