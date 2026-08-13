//go:build linux || darwin

package ipc

import (
	"context"
	"errors"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeActions struct{}

func (fakeActions) Start(context.Context, app.StartRequest) (app.View, error) {
	return app.View{SessionID: "s"}, nil
}
func (fakeActions) Poll(context.Context, app.PollRequest) (app.View, error) {
	return app.View{SessionID: "s"}, nil
}
func (fakeActions) Write(context.Context, app.WriteRequest) (app.View, error) {
	return app.View{SessionID: "s"}, nil
}
func (fakeActions) Kill(context.Context, app.KillRequest) (app.View, error) {
	return app.View{SessionID: "s"}, nil
}

func TestServerClientUnixSocket(t *testing.T) {
	// t.TempDir() resolves under $TMPDIR, which on macOS is a long
	// per-user path (/var/folders/.../T/...) that overflows the
	// Darwin sun_path limit (104 bytes) once joined with the socket
	// name. Use /tmp directly to keep the socket path short on both
	// Linux and macOS.
	runtime, err := os.MkdirTemp("/tmp", "shellbeam-ipc-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(runtime) })
	srv, err := Listen(runtime, fakeActions{})
	if err != nil {
		if errors.Is(err, os.ErrPermission) || strings.Contains(err.Error(), "operation not permitted") {
			t.Skip("sandbox blocks Unix sockets")
		}
		t.Fatal(err)
	}
	defer srv.Close()
	go srv.Serve()
	client := NewClient(srv.SocketPath())
	got, err := client.Call(context.Background(), Request{IPVersion: 1, RequestID: "r", Payload: Action{Action: "poll", SessionID: "s"}})
	if err != nil {
		t.Fatal(err)
	}
	if !got.OK || got.View.SessionID != "s" {
		t.Fatalf("%#v", got)
	}
}

func TestListenFailsClosedOnInconclusiveSocketProbe(t *testing.T) {
	runtime, err := os.MkdirTemp("/tmp", "shellbeam-ipc-probe-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtime) })
	socket := filepath.Join(runtime, "daemon.sock")
	owner, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	before, err := os.Lstat(socket)
	if err != nil {
		t.Fatal(err)
	}

	_, err = listen(runtime, fakeActions{}, func(string, time.Duration) (net.Conn, error) {
		return nil, context.DeadlineExceeded
	})
	if err == nil || err.Error() != "daemon_already_running" {
		t.Fatalf("inconclusive probe error = %v", err)
	}
	after, statErr := os.Lstat(socket)
	if statErr != nil {
		t.Fatalf("live owner socket was removed: %v", statErr)
	}
	if !os.SameFile(before, after) {
		t.Fatal("live owner socket pathname was replaced")
	}
}

func TestListenReclaimsRefusedStaleSocket(t *testing.T) {
	runtime, err := os.MkdirTemp("/tmp", "shellbeam-ipc-stale-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtime) })
	socket := filepath.Join(runtime, "daemon.sock")
	stale, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	if unixListener, ok := stale.(*net.UnixListener); ok {
		unixListener.SetUnlinkOnClose(false)
	}
	staleInfo, err := os.Lstat(socket)
	if err != nil {
		t.Fatal(err)
	}
	if err := stale.Close(); err != nil {
		t.Fatal(err)
	}

	srv, err := Listen(runtime, fakeActions{})
	if err != nil {
		t.Fatalf("reclaim stale socket: %v", err)
	}
	defer srv.Close()
	current, err := os.Lstat(socket)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(staleInfo, current) {
		t.Fatal("stale socket inode was not replaced")
	}
}

func TestServerCloseDoesNotUnlinkReplacementSocket(t *testing.T) {
	runtime, err := os.MkdirTemp("/tmp", "shellbeam-ipc-close-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtime) })
	srv, err := Listen(runtime, fakeActions{})
	if err != nil {
		t.Fatal(err)
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- srv.Serve() }()

	socket := srv.SocketPath()
	if err := os.Remove(socket); err != nil {
		t.Fatal(err)
	}
	replacement, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer replacement.Close()
	replacementInfo, err := os.Lstat(socket)
	if err != nil {
		t.Fatal(err)
	}

	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-serveDone:
		if err != nil {
			t.Fatalf("serve shutdown: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not stop")
	}
	current, err := os.Lstat(socket)
	if err != nil {
		t.Fatalf("replacement socket was unlinked: %v", err)
	}
	if !os.SameFile(replacementInfo, current) {
		t.Fatal("replacement socket changed during old server close")
	}
}
