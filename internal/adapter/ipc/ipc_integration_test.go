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
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
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
	if current.Mode()&os.ModeSocket == 0 {
		t.Fatalf("reclaimed pathname is not a socket: %v", current.Mode())
	}
	// Do not compare inode identity here: Linux may immediately reuse the
	// stale inode after unlink. Behavioral reachability proves the new
	// listener owns the reclaimed pathname without relying on inode uniqueness.
	conn, err := net.DialTimeout("unix", socket, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("reclaimed socket is not reachable: %v", err)
	}
	_ = conn.Close()
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

func TestListenWaitsForStartupLock(t *testing.T) {
	runtime, err := os.MkdirTemp("/tmp", "shellbeam-ipc-start-lock-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtime) })
	lockPath := filepath.Join(runtime, startupLockName)
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX); err != nil {
		t.Fatal(err)
	}

	result := make(chan error, 1)
	var srv *Server
	go func() {
		var listenErr error
		srv, listenErr = Listen(runtime, fakeActions{})
		result <- listenErr
	}()
	select {
	case err := <-result:
		if srv != nil {
			_ = srv.Close()
		}
		t.Fatalf("Listen returned while startup lock held: %v", err)
	case <-time.After(75 * time.Millisecond):
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Listen after startup unlock: %v", err)
		}
		defer srv.Close()
	case <-time.After(2 * time.Second):
		t.Fatal("Listen did not resume after startup unlock")
	}
}

func TestConcurrentListenOnStaleSocketHasSingleWinner(t *testing.T) {
	for round := 0; round < 500; round++ {
		runtime, err := os.MkdirTemp("/tmp", "shellbeam-ipc-stale-race-")
		if err != nil {
			t.Fatal(err)
		}
		socket := filepath.Join(runtime, "daemon.sock")
		stale, err := net.Listen("unix", socket)
		if err != nil {
			_ = os.RemoveAll(runtime)
			t.Fatal(err)
		}
		if unixListener, ok := stale.(*net.UnixListener); ok {
			unixListener.SetUnlinkOnClose(false)
		}
		if err := stale.Close(); err != nil {
			_ = os.RemoveAll(runtime)
			t.Fatal(err)
		}

		start := make(chan struct{})
		type result struct {
			srv *Server
			err error
		}
		results := make(chan result, 2)
		var ready sync.WaitGroup
		ready.Add(2)
		for i := 0; i < 2; i++ {
			go func() {
				ready.Done()
				<-start
				srv, err := Listen(runtime, fakeActions{})
				results <- result{srv: srv, err: err}
			}()
		}
		ready.Wait()
		close(start)

		var successes, alreadyRunning int
		var servers []*Server
		for i := 0; i < 2; i++ {
			got := <-results
			if got.srv != nil {
				servers = append(servers, got.srv)
			}
			switch {
			case got.err == nil:
				successes++
			case got.err.Error() == "daemon_already_running":
				alreadyRunning++
			default:
				for _, srv := range servers {
					_ = srv.Close()
				}
				_ = os.RemoveAll(runtime)
				t.Fatalf("round %d unexpected Listen error: %v", round, got.err)
			}
		}
		for _, srv := range servers {
			_ = srv.Close()
		}
		_ = os.RemoveAll(runtime)
		if successes != 1 || alreadyRunning != 1 {
			t.Fatalf("round %d: successes=%d daemon_already_running=%d", round, successes, alreadyRunning)
		}
	}
}

func TestServerCloseWaitsForStartupLock(t *testing.T) {
	runtime, err := os.MkdirTemp("/tmp", "shellbeam-ipc-close-lock-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtime) })
	srv, err := Listen(runtime, fakeActions{})
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve()

	lock, err := os.OpenFile(filepath.Join(runtime, startupLockName), os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	closed := make(chan error, 1)
	go func() { closed <- srv.Close() }()
	select {
	case err := <-closed:
		t.Fatalf("Server.Close returned while socket transition lock held: %v", err)
	case <-time.After(75 * time.Millisecond):
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("Server.Close after startup unlock: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Server.Close did not resume after startup unlock")
	}
}
