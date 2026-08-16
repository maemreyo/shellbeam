//go:build linux || darwin

package supervisor

import (
	"context"
	"testing"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func TestTerminalFreezeWaitsForInFlightTimeoutKillEvidence(t *testing.T) {
	layout, capability := runtimePrivateState(t, "timeout-order", "generation-timeout-order")
	handle := newBlockingKillHandle()
	owner := blockingKillOwner{handle: handle}
	spec := operation.ExecutionSpec{Mode: operation.ExecutionModeShell, Shell: "/bin/sh", Executable: "/bin/sh", Command: "sleep 10", CWD: "/tmp", TimeoutMS: 10}
	runtime, err := NewRuntime(RuntimeOptions{
		Layout: layout, Capability: capability, Owner: owner, Spec: spec, MaxOutputBytes: 64,
		InputLimits:    InputLimits{MaxRecords: 16, MaxMetadataBytes: 8192, MaxQueuedBytes: 64},
		MaxKillRecords: 8, TerminationGrace: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-handle.killEntered:
	case <-time.After(time.Second):
		t.Fatal("timeout KILL did not begin")
	}

	beforeCtx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	_, beforeErr := runtime.WaitTerminal(beforeCtx)
	cancel()
	if beforeErr == nil {
		t.Fatal("terminal froze while timeout KILL evidence was still in flight")
	}

	close(handle.releaseKill)
	record, err := runtime.WaitTerminal(context.Background())
	if err != nil || record.State != session.TimedOut || record.Signal.Requested != "KILL" {
		t.Fatalf("terminal=%#v err=%v", record, err)
	}
	state, err := LoadTimeoutState(layout)
	if err != nil || !state.Kill.Attempted {
		t.Fatalf("timeout state=%#v err=%v", state, err)
	}
}

type blockingKillOwner struct{ handle *blockingKillHandle }

func (o blockingKillOwner) Start(context.Context, operation.ExecutionSpec, app.OutputSink) (app.ProcessHandle, receipt.SpawnEvidence, error) {
	return o.handle, receipt.SpawnEvidence{Attempted: true, Succeeded: true}, nil
}

type blockingKillHandle struct {
	wait        chan receipt.ExitEvidence
	killEntered chan struct{}
	releaseKill chan struct{}
}

func newBlockingKillHandle() *blockingKillHandle {
	return &blockingKillHandle{wait: make(chan receipt.ExitEvidence, 1), killEntered: make(chan struct{}), releaseKill: make(chan struct{})}
}

func (h *blockingKillHandle) Write([]byte) error { return nil }
func (h *blockingKillHandle) CloseStdin() error  { return nil }
func (h *blockingKillHandle) Close() error       { return nil }
func (h *blockingKillHandle) PID() int           { return 4242 }
func (h *blockingKillHandle) Wait(ctx context.Context) receipt.ExitEvidence {
	select {
	case exit := <-h.wait:
		return exit
	case <-ctx.Done():
		return receipt.ExitEvidence{}
	}
}
func (h *blockingKillHandle) Signal(name string) receipt.SignalEvidence {
	evidence := receipt.SignalEvidence{Requested: name, Attempted: true, Succeeded: true}
	if name == "KILL" {
		close(h.killEntered)
		h.wait <- receipt.ExitEvidence{Reaped: true, Signal: "killed"}
		<-h.releaseKill
	}
	return evidence
}
