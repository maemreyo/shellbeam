package daemon_test

import (
	"context"
	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
	"path/filepath"
	"testing"
)

type pidOwner struct {
	pid  int
	wait chan struct{}
}

func (o *pidOwner) Start(context.Context, operation.ExecutionSpec, app.OutputSink) (app.ProcessHandle, receipt.SpawnEvidence, error) {
	return &pidTestHandle{pid: o.pid, wait: o.wait}, receipt.SpawnEvidence{Attempted: true, Succeeded: true}, nil
}

type pidTestHandle struct {
	pid  int
	wait chan struct{}
}

func (h *pidTestHandle) PID() int                           { return h.pid }
func (*pidTestHandle) Write([]byte) error                   { return nil }
func (*pidTestHandle) CloseStdin() error                    { return nil }
func (*pidTestHandle) Signal(string) receipt.SignalEvidence { return receipt.SignalEvidence{} }
func (h *pidTestHandle) Wait(context.Context) receipt.ExitEvidence {
	<-h.wait
	zero := 0
	return receipt.ExitEvidence{Reaped: true, Code: &zero}
}
func (*pidTestHandle) Close() error { return nil }

func TestResolveProcessSessionUsesOnlyCurrentLiveHandlePID(t *testing.T) {
	st, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1000, MaxTotalState: 1 << 20, ControlReserve: 100})
	if err != nil {
		t.Fatal(err)
	}
	wait := make(chan struct{})
	owner := &pidOwner{pid: 4242, wait: wait}
	svc := app.NewService(st, owner, app.Options{Incarnation: "d1", Shell: "/bin/sh", MaxQueuedInputBytes: 100})
	started, err := svc.Start(context.Background(), app.StartRequest{OperationID: "process-resolve", Command: "sleep", CWD: "/"})
	if err != nil {
		t.Fatal(err)
	}
	live, err := svc.ResolveProcessSession(context.Background(), started.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if !live.Known || !live.Current || live.PID != 4242 || session.State(live.State).Terminal() {
		t.Fatalf("live resolution=%#v", live)
	}

	restarted := app.NewService(st, nil, app.Options{Incarnation: "d2", Shell: "/bin/sh", MaxQueuedInputBytes: 100})
	old, err := restarted.ResolveProcessSession(context.Background(), started.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if !old.Known || old.Current || old.PID != 0 {
		t.Fatalf("restart resolution claimed current pid: %#v", old)
	}

	close(wait)
	terminal := waitForTerminal(t, svc, started.SessionID)
	if !terminal.State.Terminal() {
		t.Fatalf("terminal=%#v", terminal)
	}
	resolvedTerminal, err := svc.ResolveProcessSession(context.Background(), started.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if !resolvedTerminal.Known || resolvedTerminal.Current || resolvedTerminal.PID != 0 || !session.State(resolvedTerminal.State).Terminal() {
		t.Fatalf("terminal resolution=%#v", resolvedTerminal)
	}
}

func TestResolveProcessSessionDistinguishesUnknownSession(t *testing.T) {
	st, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1000, MaxTotalState: 1 << 20, ControlReserve: 100})
	if err != nil {
		t.Fatal(err)
	}
	svc := app.NewService(st, nil, app.Options{})
	got, err := svc.ResolveProcessSession(context.Background(), "missing-session")
	if err != nil {
		t.Fatal(err)
	}
	if got.Known || got.Current || got.PID != 0 {
		t.Fatalf("unknown session=%#v", got)
	}
}

func TestExistingFakeProcessHandleStillSatisfiesBaseInterface(t *testing.T) {
	var _ app.ProcessHandle = fakeHandle{}
}
