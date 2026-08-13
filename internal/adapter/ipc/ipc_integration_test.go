//go:build linux || darwin

package ipc

import (
	"context"
	"errors"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"os"
	"strings"
	"testing"
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
