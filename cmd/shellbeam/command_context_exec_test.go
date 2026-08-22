package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	contextadapter "github.com/maemreyo/shellbeam/internal/adapter/contextexec"
)

func TestContextExecHelperCommandsAreHiddenAndPresentationCarriesOnlyOpaqueLaunchID(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(nil, &out, &errOut); code != 2 {
		t.Fatalf("usage code=%d", code)
	}
	usage := errOut.String()
	for _, hidden := range []string{"__context_exec_helper", "__context_exec_fdexec"} {
		if strings.Contains(usage, hidden) {
			t.Fatalf("hidden command leaked: %s", usage)
		}
	}
	out.Reset()
	errOut.Reset()
	if code := run([]string{"__context_exec_helper"}, &out, &errOut); code == 0 || strings.Contains(errOut.String(), "unknown command") {
		t.Fatalf("missing helper handling code=%d err=%q", code, errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := run([]string{"__context_exec_helper", "launch_01", "/bin/echo", "secret"}, &out, &errOut); code == 0 {
		t.Fatal("helper presentation accepted child argv")
	}
}

func TestContextExecHelperUsesInheritedPrivateRuntimeLocator(t *testing.T) {
	runtimeDir, err := os.MkdirTemp("/tmp", "sb-cx-locator-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtimeDir) })
	if err := os.Chmod(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	launchID := "launch_runtime_locator_01"
	listener, _, err := contextadapter.ListenPrivate(runtimeDir, launchID)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	t.Setenv("SHELLBEAM_CONTEXT_EXEC_RUNTIME_DIR", runtimeDir)
	accepted := make(chan struct{}, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr == nil {
			accepted <- struct{}{}
			_ = conn.Close()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	_ = runContextExecHelper(ctx, []string{launchID})
	select {
	case <-accepted:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("context helper did not dial inherited private runtime locator")
	}
}
