package shellintegration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
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
	root    string
}

func (p *recordingCommandPort) WriteShell(_ context.Context, script string) error {
	p.mu.Lock()
	p.scripts = append(p.scripts, script)
	err := p.err
	root := p.root
	p.mu.Unlock()
	if err == nil && root != "" && strings.Contains(script, "hook_installed") {
		go sendTestHookInstalledAck(root)
	}
	return err
}

func sendTestHookInstalledAck(root string) {
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		paths, _ := filepath.Glob(filepath.Join(root, ".hn_*.sock"))
		if len(paths) == 1 {
			base := filepath.Base(paths[0])
			hex := strings.TrimSuffix(strings.TrimPrefix(base, ".hn_"), ".sock")
			_ = SendNotification(context.Background(), paths[0], Notification{
				HandoffID: "handoff-task6", AuthorityEpoch: 4, EventID: "evt_" + hex,
				ShellRuntimeID: "runtime-task6", Event: NotificationHookInstalled,
			})
			return
		}
		time.Sleep(time.Millisecond)
	}
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
	port.root = root
	return Dependencies{Executable: "/opt/shellbeam/bin/shellbeam", RuntimeDir: root, Command: port}
}

func assertMinimalNotifierInvocation(t *testing.T, script string) {
	t.Helper()
	if !strings.Contains(script, "/usr/bin/env") || !strings.Contains(script, "-i") || !strings.Contains(script, "__handoff_notify") {
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

type delayedAckCommandPort struct {
	root    string
	mu      sync.Mutex
	scripts []string
}

func (p *delayedAckCommandPort) WriteShell(_ context.Context, script string) error {
	p.mu.Lock()
	p.scripts = append(p.scripts, script)
	p.mu.Unlock()
	go func() {
		time.Sleep(30 * time.Millisecond)
		paths, _ := filepath.Glob(filepath.Join(p.root, ".hn_*.sock"))
		if len(paths) != 1 {
			return
		}
		base := filepath.Base(paths[0])
		hex := strings.TrimSuffix(strings.TrimPrefix(base, ".hn_"), ".sock")
		_ = SendNotification(context.Background(), paths[0], Notification{
			HandoffID: "handoff-task6", AuthorityEpoch: 4, EventID: "evt_" + hex,
			ShellRuntimeID: "runtime-task6", Event: NotificationEvent("hook_installed"), Satisfied: false,
		})
	}()
	return nil
}

func (p *delayedAckCommandPort) snapshot() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.scripts...)
}

func TestWatcherInstallWaitsForShellExecutionAcknowledgement(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "sb-hn-ack-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	port := &delayedAckCommandPort{root: root}
	deps := Dependencies{Executable: "/opt/shellbeam/bin/shellbeam", RuntimeDir: root, Command: port}
	adapter, err := NewZshAdapter(deps)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	watcher, err := adapter.Install(t.Context(), task6WatchRequest(core.ShellZsh))
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.Close()
	if elapsed := time.Since(started); elapsed < 25*time.Millisecond {
		t.Fatalf("install returned before shell execution acknowledgement: %s", elapsed)
	}
	scripts := port.snapshot()
	if len(scripts) != 1 || !strings.Contains(scripts[0], "hook_installed") || !regexp.MustCompile(`(?m)^eval `).MatchString(scripts[0]) {
		t.Fatalf("install delivery lacks atomic execution ack: %q", scripts)
	}
}

func TestWatcherInstallAcknowledgementKeepsPromptTransportOpen(t *testing.T) {
	port := &recordingCommandPort{}
	deps := task6Deps(t, port)
	adapter, err := NewZshAdapter(deps)
	if err != nil {
		t.Fatal(err)
	}
	watcher, err := adapter.Install(t.Context(), task6WatchRequest(core.ShellZsh))
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.Close()
	concrete, ok := watcher.(*oneShotWatcher)
	if !ok {
		t.Fatalf("watcher=%T", watcher)
	}
	// Give the acknowledgement context-cancellation goroutine a chance to run.
	// A successful installation acknowledgement must not close the prompt listener.
	time.Sleep(5 * time.Millisecond)
	if _, err := os.Stat(concrete.socketPath); err != nil {
		t.Fatalf("prompt transport closed after install acknowledgement: %v", err)
	}

	waitCh := make(chan app.WatchEvent, 1)
	errCh := make(chan error, 1)
	go func() {
		event, err := watcher.Wait(t.Context())
		if err != nil {
			errCh <- err
			return
		}
		waitCh <- event
	}()
	notification := concrete.expected
	notification.Satisfied = true
	if err := SendNotification(t.Context(), concrete.socketPath, notification); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errCh:
		t.Fatal(err)
	case event := <-waitCh:
		if event.Result.State != core.RequirementSatisfied {
			t.Fatalf("event=%#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("prompt notification was not observed after install acknowledgement")
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

func TestWatcherNotificationSelfRemovalAvoidsRedundantCleanupInjection(t *testing.T) {
	port := &recordingCommandPort{}
	deps := task6Deps(t, port)
	watcher, _, err := newOneShotWatcher(task6WatchRequest(core.ShellFish), deps, fishScripts)
	if err != nil {
		t.Fatal(err)
	}
	notification := watcher.expected
	notification.Satisfied = true
	waitCh := make(chan error, 1)
	go func() {
		_, err := watcher.Wait(t.Context())
		waitCh <- err
	}()
	if err := SendNotification(t.Context(), watcher.socketPath, notification); err != nil {
		t.Fatal(err)
	}
	if err := <-waitCh; err != nil {
		t.Fatal(err)
	}
	before := len(port.snapshot())
	if before != 1 {
		t.Fatalf("scripts before close=%d want install only", before)
	}
	if err := watcher.Close(); err != nil {
		t.Fatal(err)
	}
	if after := len(port.snapshot()); after != before {
		t.Fatalf("successful notification injected redundant cleanup: before=%d after=%d scripts=%q", before, after, port.snapshot())
	}
}

func TestPosixWatcherDeliveryPreservesRealCommandBoundary(t *testing.T) {
	got := posixWatcherDelivery("install", "ack")
	want := "eval " + shellQuote("install\nack")
	if got != want {
		t.Fatalf("delivery=%q want=%q", got, want)
	}
}
