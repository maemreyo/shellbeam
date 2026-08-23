//go:build darwin

package delegatedtmux

import (
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestDarwinProcessExitWatcherReportsKernelExitEvent(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "sleep .08; exit 7")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	watcher, err := newProcessExitWatcher(cmd.Process.Pid)
	if err != nil {
		_ = cmd.Process.Kill()
		t.Fatal(err)
	}
	defer watcher.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if err := watcher.Wait(ctx); err != nil {
		_ = cmd.Process.Kill()
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("expected child exit 7")
	}
}

func TestDarwinProcessExitWatcherCancelsWithoutPolling(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "sleep 10")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	watcher, err := newProcessExitWatcher(cmd.Process.Pid)
	if err != nil {
		_ = cmd.Process.Kill()
		t.Fatal(err)
	}
	defer watcher.Close()
	defer cmd.Wait()
	defer cmd.Process.Kill()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := watcher.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel err=%v", err)
	}
}

func TestDarwinProcessWaitRetriesOnlyInterrupts(t *testing.T) {
	if !retryProcessWaitError(unix.EINTR) {
		t.Fatal("EINTR must be retried")
	}
	if retryProcessWaitError(unix.EBADF) || retryProcessWaitError(context.Canceled) {
		t.Fatal("non-EINTR wait errors must remain fail-closed")
	}
}
