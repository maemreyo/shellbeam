//go:build darwin

package delegatedtmux

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	core "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

type nativeSink struct {
	mu   sync.Mutex
	data []byte
}

func (s *nativeSink) Append(b []byte) error {
	s.mu.Lock()
	s.data = append(s.data, b...)
	s.mu.Unlock()
	return nil
}
func (s *nativeSink) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return string(append([]byte(nil), s.data...))
}

type defaultSocketSnapshot struct {
	path   string
	exists bool
	mode   os.FileMode
	size   int64
	mtime  int64
}

func snapshotDefaultTmuxSocket(t *testing.T) defaultSocketSnapshot {
	t.Helper()
	base := os.Getenv("TMUX_TMPDIR")
	if base == "" {
		base = "/tmp"
	}
	s := defaultSocketSnapshot{path: filepath.Join(base, fmt.Sprintf("tmux-%d", os.Getuid()), "default")}
	info, err := os.Stat(s.path)
	if os.IsNotExist(err) {
		return s
	}
	if err != nil {
		t.Fatal(err)
	}
	s.exists = true
	s.mode = info.Mode()
	s.size = info.Size()
	s.mtime = info.ModTime().UnixNano()
	return s
}
func assertDefaultTmuxSocketUnchanged(t *testing.T, before defaultSocketSnapshot) {
	t.Helper()
	info, err := os.Stat(before.path)
	if !before.exists {
		if err == nil {
			t.Fatalf("default tmux socket appeared: %s", before.path)
		}
		if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode() != before.mode || info.Size() != before.size || info.ModTime().UnixNano() != before.mtime {
		t.Fatal("default tmux socket changed")
	}
}

func nativeProvider(t *testing.T) (*Provider, string) {
	t.Helper()
	tmux := os.Getenv("SHELLBEAM_H0_TMUX")
	if tmux == "" {
		t.Skip("SHELLBEAM_H0_TMUX not set")
	}
	root := filepath.Join(t.TempDir(), "state")
	cfg := DarwinQualifiedConfig(root, tmux)
	cfg.RuntimeBase = "/tmp"
	p, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Probe(t.Context()); err != nil {
		t.Fatal(err)
	}
	return p, root
}
func nativeRef(t *testing.T, p *Provider, sessionID string) core.ProviderRef {
	t.Helper()
	ref, err := p.ProviderRefForSession(sessionID, time.Date(2026, 8, 18, 18, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return ref
}
func waitContains(t *testing.T, sink *nativeSink, want string) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(sink.String(), want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("output missing %q: %q", want, sink.String())
}
func waitTerminal(t *testing.T, p *Provider, ref core.ProviderRef) app.Observation {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		obs, err := p.Inspect(t.Context(), ref)
		if err != nil {
			t.Fatal(err)
		}
		if obs.Terminal {
			return obs
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("provider did not reach terminal")
	return app.Observation{}
}

func TestNativeDelegatedCreateCapturesFirstByteWritesAndExitWithoutDefaultSocket(t *testing.T) {
	before := snapshotDefaultTmuxSocket(t)
	p, _ := nativeProvider(t)
	ref := nativeRef(t, p, "session_native_01")
	sink := &nativeSink{}
	spec := operation.ExecutionSpec{Mode: operation.ExecutionModeShell, Shell: "/bin/sh", Executable: "/bin/sh", Command: "printf FIRST; IFS= read -r line; printf ':SECOND:%s\\n' \"$line\"; exit 7", CWD: t.TempDir()}
	result, err := p.Create(t.Context(), app.CreateRequest{ProviderRef: ref, SessionID: ref.SessionID, OperationID: "op_native_01", Spec: spec, Output: sink})
	if err != nil {
		t.Fatalf("create: %s", nativeFailure(err))
	}
	t.Cleanup(func() { _ = p.Close(context.Background(), ref) })
	if result.ProviderRef != ref || !result.Observation.ProviderCurrent || result.Observation.Owner != core.OwnerAgent {
		t.Fatalf("create=%#v", result)
	}
	waitContains(t, sink, "FIRST")
	if err := p.Write(t.Context(), ref, []byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	waitContains(t, sink, ":SECOND:hello")
	obs := waitTerminal(t, p, ref)
	if obs.ExitCode == nil || *obs.ExitCode != 7 || obs.Owner != core.OwnerNone {
		t.Fatalf("terminal=%#v", obs)
	}
	state, err := p.state.load(ref.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if state.SocketPath == "" || state.PaneID == "" || state.ProviderGeneration == "" || !state.StartReleased {
		t.Fatalf("state=%#v", state)
	}
	assertDefaultTmuxSocketUnchanged(t, before)
}

func TestNativeDelegatedReattachIsForwardOnlyAndGenerationBound(t *testing.T) {
	p, _ := nativeProvider(t)
	ref := nativeRef(t, p, "session_native_02")
	first := &nativeSink{}
	spec := operation.ExecutionSpec{Mode: operation.ExecutionModeShell, Shell: "/bin/sh", Executable: "/bin/sh", Command: "printf BEFORE; IFS= read -r line; printf 'AFTER:%s\\n' \"$line\"; sleep 1", CWD: t.TempDir()}
	if _, err := p.Create(t.Context(), app.CreateRequest{ProviderRef: ref, SessionID: ref.SessionID, OperationID: "op_native_02", Spec: spec, Output: first}); err != nil {
		t.Fatalf("create: %s", nativeFailure(err))
	}
	t.Cleanup(func() { _ = p.Close(context.Background(), ref) })
	waitContains(t, first, "BEFORE")
	p.mu.Lock()
	old := p.controls[ref.Ref]
	delete(p.controls, ref.Ref)
	p.mu.Unlock()
	if old != nil {
		_ = old.close()
	}
	second := &nativeSink{}
	obs, err := p.Reattach(t.Context(), ref, second)
	if err != nil {
		t.Fatal(err)
	}
	if !obs.ProviderCurrent || obs.ProviderGeneration == "" {
		t.Fatalf("reattach=%#v", obs)
	}
	if strings.Contains(second.String(), "BEFORE") {
		t.Fatalf("reattach replayed hidden history: %q", second.String())
	}
	if err := p.Write(t.Context(), ref, []byte("new\n")); err != nil {
		t.Fatal(err)
	}
	waitContains(t, second, "AFTER:new")

	state, err := p.state.load(ref.Ref)
	if err != nil {
		t.Fatal(err)
	}
	state.ProviderGeneration = "gen_forged"
	state.UpdatedAt = state.UpdatedAt.Add(time.Second)
	if err := p.state.save(state); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	current := p.controls[ref.Ref]
	delete(p.controls, ref.Ref)
	p.mu.Unlock()
	if current != nil {
		_ = current.close()
	}
	if _, err := p.Reattach(t.Context(), ref, &nativeSink{}); err == nil {
		t.Fatal("forged provider generation accepted")
	}
}

func TestNativeDelegatedSignalTargetsWorkloadProcessGroup(t *testing.T) {
	p, _ := nativeProvider(t)
	ref := nativeRef(t, p, "session_native_03")
	sink := &nativeSink{}
	spec := operation.ExecutionSpec{Mode: operation.ExecutionModeShell, Shell: "/bin/sh", Executable: "/bin/sh", Command: "trap 'printf GOT_INT; exit 0' INT; printf READY; while :; do sleep 1; done", CWD: t.TempDir()}
	if _, err := p.Create(t.Context(), app.CreateRequest{ProviderRef: ref, SessionID: ref.SessionID, OperationID: "op_native_03", Spec: spec, Output: sink}); err != nil {
		t.Fatalf("create: %s", nativeFailure(err))
	}
	t.Cleanup(func() { _ = p.Close(context.Background(), ref) })
	waitContains(t, sink, "READY")
	if err := p.Signal(t.Context(), ref, "INT"); err != nil {
		t.Fatal(err)
	}
	waitContains(t, sink, "GOT_INT")
	obs := waitTerminal(t, p, ref)
	if obs.ExitCode == nil || *obs.ExitCode != 0 {
		t.Fatalf("terminal=%#v", obs)
	}
}

func nativeFailure(err error) string {
	var typed *failure.Failure
	if errors.As(err, &typed) {
		return fmt.Sprintf("code=%s details=%v cause=%v", typed.Code, typed.Details, typed.Cause)
	}
	return fmt.Sprintf("%T %v", err, err)
}
