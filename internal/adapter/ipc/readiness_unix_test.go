//go:build linux || darwin

package ipc

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPendingServerQueuesV2RequestUntilMarkedReady(t *testing.T) {
	runtime, err := os.MkdirTemp("/tmp", "shellbeam-ipc-pending-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtime) })

	srv, err := ListenPending(runtime, fakeActions{})
	if err != nil {
		if errors.Is(err, os.ErrPermission) || strings.Contains(err.Error(), "operation not permitted") {
			t.Skip("sandbox blocks Unix sockets")
		}
		t.Fatal(err)
	}
	defer srv.Close()
	serveDone := make(chan error, 1)
	go func() { serveDone <- srv.Serve() }()

	client := NewClient(srv.SocketPath())
	type callResult struct {
		response ResponseV2
		err      error
	}
	callDone := make(chan callResult, 1)
	go func() {
		response, err := client.CallV2(context.Background(), RequestV2{IPVersion: 2, Kind: "request", RequestID: "pending", Action: "inspect.server"})
		callDone <- callResult{response: response, err: err}
	}()

	select {
	case result := <-callDone:
		t.Fatalf("pending request dispatched before readiness: response=%#v err=%v", result.response, result.err)
	case <-time.After(50 * time.Millisecond):
	}

	srv.MarkReady()
	select {
	case result := <-callDone:
		if result.err != nil || !result.response.OK || result.response.Server == nil {
			t.Fatalf("ready response=%#v err=%v", result.response, result.err)
		}
	case <-time.After(time.Second):
		t.Fatal("request remained blocked after readiness")
	}
}

func TestPendingServerCloseReleasesWaitingRequest(t *testing.T) {
	runtime, err := os.MkdirTemp("/tmp", "shellbeam-ipc-pending-close-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtime) })

	srv, err := ListenPending(runtime, fakeActions{})
	if err != nil {
		if errors.Is(err, os.ErrPermission) || strings.Contains(err.Error(), "operation not permitted") {
			t.Skip("sandbox blocks Unix sockets")
		}
		t.Fatal(err)
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- srv.Serve() }()
	client := NewClient(srv.SocketPath())
	callDone := make(chan error, 1)
	go func() {
		_, err := client.CallV2(context.Background(), RequestV2{IPVersion: 2, Kind: "request", RequestID: "pending-close", Action: "inspect.server"})
		callDone <- err
	}()

	time.Sleep(30 * time.Millisecond)
	_ = srv.Close()
	select {
	case <-callDone:
	case <-time.After(time.Second):
		t.Fatal("pending request leaked after server close")
	}
	select {
	case <-serveDone:
	case <-time.After(time.Second):
		t.Fatal("Serve did not exit after close")
	}
}

func TestClaimSocketPublishesCanonicalPathOnlyAfterListenerIsReady(t *testing.T) {
	runtime, err := os.MkdirTemp("/tmp", "shellbeam-ipc-publish-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtime) })
	canonical := filepath.Join(runtime, "daemon.sock")
	listenerReady := make(chan string, 1)
	release := make(chan struct{})
	factory := func(path string) (net.Listener, error) {
		if path == canonical {
			t.Fatal("listener bound canonical pathname before publish")
		}
		ln, err := net.Listen("unix", path)
		if err != nil {
			return nil, err
		}
		listenerReady <- path
		<-release
		return ln, nil
	}
	type claimResult struct {
		ln   net.Listener
		info os.FileInfo
		err  error
	}
	result := make(chan claimResult, 1)
	go func() {
		ln, info, err := claimSocketWithListener(canonical, dialUnixSocket, factory)
		result <- claimResult{ln: ln, info: info, err: err}
	}()
	staged := <-listenerReady
	if _, err := os.Lstat(canonical); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("canonical socket published before listener factory returned: %v", err)
	}
	if info, err := os.Lstat(staged); err != nil || info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("staged listening socket missing: info=%v err=%v", info, err)
	}
	close(release)
	claimed := <-result
	if claimed.err != nil {
		t.Fatal(claimed.err)
	}
	defer claimed.ln.Close()
	current, err := os.Lstat(canonical)
	if err != nil || claimed.info == nil || !os.SameFile(claimed.info, current) {
		t.Fatalf("canonical publication mismatch info=%v current=%v err=%v", claimed.info, current, err)
	}
	if _, err := os.Lstat(staged); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staging pathname survived publication: %v", err)
	}
	conn, err := net.DialTimeout("unix", canonical, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("published canonical socket is not listening: %v", err)
	}
	_ = conn.Close()
}
