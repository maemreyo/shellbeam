//go:build linux || darwin

package integration_test

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	processadapter "github.com/maemreyo/shellbeam/internal/adapter/process"
	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
)

type capture struct {
	mu  sync.Mutex
	b   strings.Builder
	err error
}

func (c *capture) Append(_ context.Context, b []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, _ = c.b.Write(b)
	return c.err
}
func (c *capture) CaptureFailed(e error) { c.mu.Lock(); c.err = e; c.mu.Unlock() }

func TestRealRuntimeStartWritePoll(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	st, err := storeadapter.Open(root, storeadapter.Limits{MaxSessions: 2, MaxSessionOutput: 1 << 20, MaxTotalState: 1 << 24, ControlReserve: 1 << 10})
	if err != nil {
		t.Fatal(err)
	}
	svc := app.NewService(st, processadapter.Owner{}, app.Options{Incarnation: "native", Shell: "/bin/sh", MaxQueuedInputBytes: 1024, TerminationGrace: 50 * time.Millisecond})
	view, err := svc.Start(context.Background(), app.StartRequest{OperationID: "native1", Command: "read line; printf 'got:%s' \"$line\"", CWD: t.TempDir(), YieldMS: 5, MaxOutputBytes: 100})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Write(context.Background(), app.WriteRequest{SessionID: view.SessionID, InputOffset: 0, Chars: "hello\n"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		view, err = svc.Poll(context.Background(), app.PollRequest{SessionID: view.SessionID, Cursor: 0, YieldMS: 20, MaxOutputBytes: 100})
		if err != nil {
			t.Fatal(err)
		}
		if view.State.Terminal() {
			break
		}
	}
	if !view.State.Terminal() {
		t.Fatalf("session did not finish: %#v", view)
	}
	if view.Output != "got:hello" {
		t.Fatalf("view=%#v", view)
	}
	if view.Receipt == nil || view.Receipt.OutputBytes != int64(len("got:hello")) || view.Receipt.InputAcceptedBytes != 6 || view.Receipt.InputDeliveredBytes != 6 {
		t.Fatalf("receipt=%#v", view.Receipt)
	}
}
