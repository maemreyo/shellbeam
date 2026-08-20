package shellintegration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/shellintegration"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	core "github.com/maemreyo/shellbeam/internal/core/shellintegration"
)

type recordingCommandPort struct {
	mu      sync.Mutex
	scripts []string
	err     error
}

func (p *recordingCommandPort) WriteShell(_ context.Context, script string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.scripts = append(p.scripts, script)
	return p.err
}
func (p *recordingCommandPort) snapshot() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.scripts...)
}

func task6WatchRequest(family core.ShellFamily) app.WatchRequest {
	return app.WatchRequest{
		HandoffID: "handoff-task6", AuthorityEpoch: delegated.AuthorityEpoch(4),
		Shell:       core.ShellIdentity{Family: family, RuntimeID: "runtime-task6"},
		Requirement: core.Requirement{Kind: core.RequirementEnvironmentExportedNonempty, Name: "CONTROL_PLANE_API_KEY"},
	}
}

func task6Deps(t *testing.T, port *recordingCommandPort) Dependencies {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "sb-hn-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	return Dependencies{Executable: "/opt/shellbeam/bin/shellbeam", RuntimeDir: root, Command: port}
}

func assertMinimalNotifierInvocation(t *testing.T, script string) {
	t.Helper()
	if !strings.Contains(script, "'/usr/bin/env' '-i'") || !strings.Contains(script, "__handoff_notify") {
		t.Fatalf("notifier is not environment-isolated: %s", script)
	}
	for _, forbidden := range []string{"CONTROL_PLANE_API_KEY=", "env |", "printenv", "config.fish", ".zshrc", ".bashrc"} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("script contains forbidden %q: %s", forbidden, script)
		}
	}
}

func TestNotifierRejectsTooLongUnixSocketPathBeforeListen(t *testing.T) {
	port := &recordingCommandPort{}
	root := filepath.Join("/tmp", strings.Repeat("x", 120))
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	_, _, err := newOneShotWatcher(task6WatchRequest(core.ShellFish), Dependencies{Executable: "/opt/shellbeam/bin/shellbeam", RuntimeDir: root, Command: port}, fishScripts)
	if err == nil || !strings.Contains(err.Error(), "socket path too long") {
		t.Fatalf("err=%v", err)
	}
}

func TestWatcherInstallCommandFailureRemovesPrivateSocket(t *testing.T) {
	port := &recordingCommandPort{err: errors.New("command failed")}
	deps := task6Deps(t, port)
	adapter, err := NewFishAdapter(deps)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Install(t.Context(), task6WatchRequest(core.ShellFish)); err == nil || !strings.Contains(err.Error(), "command failed") {
		t.Fatalf("err=%v", err)
	}
	assertNoNotifierSockets(t, deps.RuntimeDir)
}

func TestWatcherCancellationClosesTransportWithoutResidentHelper(t *testing.T) {
	port := &recordingCommandPort{}
	deps := task6Deps(t, port)
	adapter, err := NewFishAdapter(deps)
	if err != nil {
		t.Fatal(err)
	}
	watcher, err := adapter.Install(t.Context(), task6WatchRequest(core.ShellFish))
	if err != nil {
		t.Fatal(err)
	}
	concrete, ok := watcher.(*oneShotWatcher)
	if !ok {
		t.Fatalf("watcher=%T", watcher)
	}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { _, err := watcher.Wait(ctx); errCh <- err }()
	cancel()
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("wait err=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("watcher did not exit on cancellation")
	}
	if _, err := os.Stat(concrete.socketPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket still present after cancellation: %v", err)
	}
	if err := watcher.Close(); err != nil {
		t.Fatal(err)
	}
	assertNoNotifierSockets(t, deps.RuntimeDir)
}

func TestNotifierFailsClosedAfterWatcherTransportIsGone(t *testing.T) {
	port := &recordingCommandPort{}
	deps := task6Deps(t, port)
	watcher, _, err := newOneShotWatcher(task6WatchRequest(core.ShellFish), deps, fishScripts)
	if err != nil {
		t.Fatal(err)
	}
	socket := watcher.socketPath
	notification := watcher.expected
	if err := watcher.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := SendNotification(ctx, socket, notification); err == nil {
		t.Fatal("notification unexpectedly succeeded after watcher transport closed")
	}
	assertNoNotifierSockets(t, deps.RuntimeDir)
}

func assertNoNotifierSockets(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".sock") {
			t.Fatalf("notifier socket leaked: %s", entry.Name())
		}
	}
}
