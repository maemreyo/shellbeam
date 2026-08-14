//go:build linux || darwin

package ipc

import (
	"context"
	"errors"
	"os"
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
