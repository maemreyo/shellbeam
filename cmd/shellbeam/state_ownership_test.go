//go:build linux || darwin

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	"github.com/maemreyo/shellbeam/internal/adapter/ownership"
)

// Daemon ownership is a guarantee between processes: a lease is a lock on an
// open file descriptor, and the kernel is what arbitrates it and what releases
// it when a holder dies. Booting these daemons inside the test process would
// exercise a different thing -- one process contending with itself -- and would
// put several full daemons' goroutines and heap into a package whose other
// tests measure latency against hard deadlines. Every daemon here is a real
// subprocess.

var (
	ownershipBinaryOnce sync.Once
	ownershipBinaryPath string
	ownershipBinaryErr  error
)

// ownershipBinary builds the daemon once for the whole package.
func ownershipBinary(t *testing.T) string {
	t.Helper()
	ownershipBinaryOnce.Do(func() {
		root, err := os.MkdirTemp("/tmp", "shellbeam-ownership-bin-")
		if err != nil {
			ownershipBinaryErr = err
			return
		}
		binary := filepath.Join(root, "shellbeam")
		if out, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
			ownershipBinaryErr = err
			t.Logf("build daemon: %s", out)
			return
		}
		ownershipBinaryPath = binary
	})
	if ownershipBinaryErr != nil {
		t.Fatalf("build daemon: %v", ownershipBinaryErr)
	}
	return ownershipBinaryPath
}

func ownershipDirs(t *testing.T, names ...string) (string, []string) {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "shellbeam-ownership-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	dirs := make([]string, 0, len(names))
	for _, name := range names {
		dirs = append(dirs, filepath.Join(root, name))
	}
	return filepath.Join(root, "state"), dirs
}

type daemonProcess struct {
	cmd     *exec.Cmd
	logPath string
	client  *ipcadapter.Client
	waited  bool
	err     error
}

func launchDaemon(t *testing.T, stateDir, runtimeDir string) *daemonProcess {
	t.Helper()
	return launchDaemonWithConfig(t, stateDir, runtimeDir, "")
}

func launchDaemonWithConfig(t *testing.T, stateDir, runtimeDir, configPath string) *daemonProcess {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "daemon.log")
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"daemon", "--state-dir", stateDir, "--runtime-dir", runtimeDir, "--shell", "/bin/sh"}
	if configPath != "" {
		args = append(args, "--config", configPath)
	}
	cmd := exec.Command(ownershipBinary(t), args...)
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		_ = log.Close()
		t.Fatal(err)
	}
	_ = log.Close()
	d := &daemonProcess{cmd: cmd, logPath: logPath, client: ipcadapter.NewClient(filepath.Join(runtimeDir, "daemon.sock"))}
	t.Cleanup(func() { d.stop(t) })
	return d
}

// serving waits until the daemon answers on its socket.
func (d *daemonProcess) serving(t *testing.T) bool {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if d.exited() {
			return false
		}
		response, err := d.client.CallV2(context.Background(), ipcadapter.RequestV2{
			IPVersion: 2, Kind: "request", RequestID: "ownership-ready", Action: "inspect.server",
		})
		if err == nil && response.OK {
			return true
		}
		time.Sleep(25 * time.Millisecond)
	}
	return false
}

func (d *daemonProcess) exited() bool {
	return d.cmd.ProcessState != nil && d.cmd.ProcessState.Exited()
}

// awaitExit waits for the daemon to exit on its own and reports what it said.
func (d *daemonProcess) awaitExit(t *testing.T, within time.Duration) (error, string) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- d.cmd.Wait() }()
	select {
	case err := <-done:
		d.waited, d.err = true, err
		return err, d.output(t)
	case <-time.After(within):
		_ = d.cmd.Process.Kill()
		d.waited = true
		<-done
		return nil, d.output(t)
	}
}

func (d *daemonProcess) output(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(d.logPath)
	if err != nil {
		return ""
	}
	return string(b)
}

func (d *daemonProcess) stop(t *testing.T) {
	t.Helper()
	if d.waited {
		return
	}
	_ = d.cmd.Process.Signal(syscall.SIGTERM)
	done := make(chan error, 1)
	go func() { done <- d.cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		_ = d.cmd.Process.Kill()
		<-done
		t.Error("daemon did not stop on SIGTERM")
	}
	d.waited = true
}

func requireHeld(t *testing.T, dir string, want bool) {
	t.Helper()
	held, err := ownership.Held(dir)
	if err != nil {
		t.Fatal(err)
	}
	if held != want {
		t.Fatalf("ownership of %s = %v, want %v", dir, held, want)
	}
}

// TestSecondDaemonOnSharedStateDirectoryIsRefused is the regression test for
// why the state directory is leased at all.
//
// The admission index that replaced the per-Start filesystem rescan lives in
// each daemon's memory. Two daemons pointed at one state directory would
// therefore each admit up to the configured session limit against the same
// store -- a maximum of four live sessions becoming eight -- and no amount of
// care inside the store can detect that, because the other process simply is
// not visible to it. Separate runtime directories keep the endpoints distinct,
// so the socket cannot be what stops this; only the state lease can.
func TestSecondDaemonOnSharedStateDirectoryIsRefused(t *testing.T) {
	stateDir, runtimes := ownershipDirs(t, "run-a", "run-b")

	owner := launchDaemon(t, stateDir, runtimes[0])
	if !owner.serving(t) {
		t.Fatalf("first daemon never served: %s", owner.output(t))
	}
	requireHeld(t, stateDir, true)

	second := launchDaemon(t, stateDir, runtimes[1])
	err, output := second.awaitExit(t, 15*time.Second)
	if err == nil {
		t.Fatalf("second daemon on the shared state directory kept running; the state lease did not refuse it\n%s", output)
	}
	if !strings.Contains(output, ownership.ErrOwnerAlive.Error()) {
		t.Fatalf("second daemon exited with %v but did not report %q:\n%s", err, ownership.ErrOwnerAlive, output)
	}
	if _, statErr := os.Lstat(filepath.Join(runtimes[1], "daemon.sock")); statErr == nil {
		t.Fatal("refused daemon still published an endpoint")
	}
	// The refusal must not have disturbed the owner.
	if !owner.serving(t) {
		t.Fatal("owner stopped serving after refusing a second daemon")
	}
}

// TestSecondDaemonOnSeparateStateDirectoryIsAllowed keeps the guarantee from
// overreaching: unrelated daemons must still coexist.
func TestSecondDaemonOnSeparateStateDirectoryIsAllowed(t *testing.T) {
	firstState, firstRuntimes := ownershipDirs(t, "run-a")
	secondState, secondRuntimes := ownershipDirs(t, "run-b")

	first := launchDaemon(t, firstState, firstRuntimes[0])
	second := launchDaemon(t, secondState, secondRuntimes[0])
	if !first.serving(t) {
		t.Fatalf("first daemon never served: %s", first.output(t))
	}
	if !second.serving(t) {
		t.Fatalf("second daemon never served: %s", second.output(t))
	}
	requireHeld(t, firstState, true)
	requireHeld(t, secondState, true)
}

// TestDaemonAcceptsSharedStateAndRuntimeDirectory keeps a configuration that
// has always worked working. Nothing requires the two directories to differ,
// and the daemon leases both -- so it must share ownership with itself rather
// than report daemon_already_running about its own lock.
func TestDaemonAcceptsSharedStateAndRuntimeDirectory(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "shellbeam-shared-dir-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	shared := filepath.Join(root, "d")
	if err := os.MkdirAll(shared, 0700); err != nil {
		t.Fatal(err)
	}

	daemon := launchDaemon(t, shared, shared)
	if !daemon.serving(t) {
		t.Fatalf("daemon using one directory for both state and runtime never served: %s", daemon.output(t))
	}
	requireHeld(t, shared, true)
}

// TestStateOwnershipReleasedOnShutdown lets a replacement daemon take over
// after an orderly stop.
func TestStateOwnershipReleasedOnShutdown(t *testing.T) {
	stateDir, runtimes := ownershipDirs(t, "run-a")
	first := launchDaemon(t, stateDir, runtimes[0])
	if !first.serving(t) {
		t.Fatalf("daemon never served: %s", first.output(t))
	}
	first.stop(t)
	requireHeld(t, stateDir, false)

	successor := launchDaemon(t, stateDir, runtimes[0])
	if !successor.serving(t) {
		t.Fatalf("successor never served: %s", successor.output(t))
	}
}

// TestStateOwnershipReleasedWhenDaemonIsKilled is the ungraceful half: nothing
// runs in a SIGKILLed process, so only the kernel can be what frees the lock.
func TestStateOwnershipReleasedWhenDaemonIsKilled(t *testing.T) {
	stateDir, runtimes := ownershipDirs(t, "run-a")
	first := launchDaemon(t, stateDir, runtimes[0])
	if !first.serving(t) {
		t.Fatalf("daemon never served: %s", first.output(t))
	}
	requireHeld(t, stateDir, true)

	if err := first.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_, _ = first.awaitExit(t, 10*time.Second)
	requireHeld(t, stateDir, false)

	successor := launchDaemon(t, stateDir, runtimes[0])
	if !successor.serving(t) {
		t.Fatalf("successor could not take over from a killed daemon: %s", successor.output(t))
	}
}
