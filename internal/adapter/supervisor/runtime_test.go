package supervisor

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func TestRuntimeSpawnsOncePersistsIOAndFreezesTerminalBeforeDone(t *testing.T) {
	layout, capability := runtimePrivateState(t, "persistent-session-runtime", "generation-runtime")
	owner := newRuntimeFakeOwner()
	runtime, err := NewRuntime(RuntimeOptions{
		Layout: layout, Capability: capability, Owner: owner,
		Spec:           operation.ExecutionSpec{Mode: operation.ExecutionModeShell, Shell: "/bin/sh", Executable: "/bin/sh", Command: "cat", CWD: "/tmp"},
		MaxOutputBytes: 64, InputLimits: InputLimits{MaxRecords: 16, MaxMetadataBytes: 32 << 10, MaxQueuedBytes: 16}, MaxKillRecords: 8, TerminationGrace: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Start(context.Background()); err == nil || owner.StartCount() != 1 {
		t.Fatalf("second start err=%v starts=%d", err, owner.StartCount())
	}
	if err := owner.Emit([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	first, err := runtime.Write(0, []byte("abc"), false)
	if err != nil || first.NextOffset != 3 {
		t.Fatalf("first write=%#v err=%v", first, err)
	}
	duplicate, err := runtime.Write(0, []byte("abc"), false)
	if err != nil || !duplicate.Duplicate || owner.handle.WriteCount() != 1 {
		t.Fatalf("duplicate=%#v writes=%d err=%v", duplicate, owner.handle.WriteCount(), err)
	}
	if _, err := runtime.Write(3, nil, true); err != nil || owner.handle.CloseStdinCount() != 1 {
		t.Fatalf("eof closes=%d err=%v", owner.handle.CloseStdinCount(), err)
	}
	owner.handle.FinishCode(0)
	record, err := runtime.WaitTerminal(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if record.State != session.Completed || record.Outcome != session.Success || record.OutputBytes != 5 || !record.OutputComplete || record.InputAcceptedBytes != 3 || record.InputDeliveredBytes != 3 || !record.StdinClosed {
		t.Fatalf("terminal=%#v", record)
	}
	loaded, err := LoadTerminalRecord(layout, capability, record.SessionID, record.GenerationID)
	if err != nil || loaded.Integrity != record.Integrity {
		t.Fatalf("terminal was not frozen before done: loaded=%#v err=%v", loaded, err)
	}
}

func TestRuntimeKillReplayDoesNotResignal(t *testing.T) {
	layout, capability := runtimePrivateState(t, "persistent-session-kill-runtime", "generation-kill-runtime")
	owner := newRuntimeFakeOwner()
	runtime := mustRuntime(t, layout, capability, owner, operation.ExecutionSpec{Mode: operation.ExecutionModeShell, Shell: "/bin/sh", Executable: "/bin/sh", Command: "sleep 10", CWD: "/tmp"}, 64)
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	first, err := runtime.Signal("kill-1", "TERM")
	if err != nil || !first.Attempted || !first.Succeeded || owner.handle.SignalCount("TERM") != 1 {
		t.Fatalf("first=%#v signals=%d err=%v", first, owner.handle.SignalCount("TERM"), err)
	}
	replay, err := runtime.Signal("kill-1", "TERM")
	if err != nil || !replay.Attempted || owner.handle.SignalCount("TERM") != 1 {
		t.Fatalf("replay=%#v signals=%d err=%v", replay, owner.handle.SignalCount("TERM"), err)
	}
	if _, err := runtime.Signal("kill-1", "KILL"); err == nil {
		t.Fatal("changed signal under same kill id accepted")
	}
	owner.handle.FinishSignal("terminated")
	record, err := runtime.WaitTerminal(context.Background())
	if err != nil || record.State != session.Killed || record.Outcome != session.KilledOutcome {
		t.Fatalf("terminal=%#v err=%v", record, err)
	}
}

func TestRuntimeTimeoutFiresWithoutDaemonAndPersistsEscalation(t *testing.T) {
	layout, capability := runtimePrivateState(t, "persistent-session-timeout", "generation-timeout")
	owner := newRuntimeFakeOwner()
	spec := operation.ExecutionSpec{Mode: operation.ExecutionModeShell, Shell: "/bin/sh", Executable: "/bin/sh", Command: "sleep 10", CWD: "/tmp", TimeoutMS: 20}
	runtime := mustRuntime(t, layout, capability, owner, spec, 64)
	runtime.terminationGrace = 10 * time.Millisecond
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(2 * time.Second)
	for owner.handle.SignalCount("KILL") == 0 {
		select {
		case <-deadline:
			t.Fatalf("timeout signals=%v", owner.handle.Signals())
		default:
			time.Sleep(time.Millisecond)
		}
	}
	owner.handle.FinishSignal("killed")
	record, err := runtime.WaitTerminal(context.Background())
	if err != nil || record.State != session.TimedOut || record.Outcome != session.Timeout || !record.TimedOut || record.Signal.Requested != "KILL" {
		t.Fatalf("timeout terminal=%#v err=%v", record, err)
	}
	state, err := LoadTimeoutState(layout)
	if err != nil || !state.Fired || !state.Term.Attempted || !state.Kill.Attempted || state.Deadline.IsZero() {
		t.Fatalf("timeout state=%#v err=%v", state, err)
	}
}

func TestRuntimeOutputLimitFailureTerminatesAndFreezesIncomplete(t *testing.T) {
	layout, capability := runtimePrivateState(t, "persistent-session-output-fail", "generation-output-fail")
	owner := newRuntimeFakeOwner()
	runtime := mustRuntime(t, layout, capability, owner, operation.ExecutionSpec{Mode: operation.ExecutionModeShell, Shell: "/bin/sh", Executable: "/bin/sh", Command: "yes", CWD: "/tmp"}, 3)
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := owner.Emit([]byte("toolong")); err == nil {
		t.Fatal("output overflow did not fail sink")
	}
	if owner.handle.SignalCount("TERM") == 0 {
		t.Fatal("output capture failure did not terminate child")
	}
	owner.handle.FinishSignal("terminated")
	record, err := runtime.WaitTerminal(context.Background())
	if err != nil || record.State != session.Killed || record.Outcome != session.KilledOutcome || record.OutputComplete || record.FailureReason != "output_capture_failed" {
		t.Fatalf("terminal=%#v err=%v", record, err)
	}
}

func mustRuntime(t *testing.T, layout Layout, capability Capability, owner *runtimeFakeOwner, spec operation.ExecutionSpec, maxOutput int64) *Runtime {
	t.Helper()
	runtime, err := NewRuntime(RuntimeOptions{Layout: layout, Capability: capability, Owner: owner, Spec: spec, MaxOutputBytes: maxOutput, InputLimits: InputLimits{MaxRecords: 16, MaxMetadataBytes: 32 << 10, MaxQueuedBytes: 64}, MaxKillRecords: 8, TerminationGrace: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

func runtimePrivateState(t *testing.T, sessionID, generation string) (Layout, Capability) {
	t.Helper()
	capability, err := NewCapability()
	if err != nil {
		t.Fatal(err)
	}
	layout, err := PreparePrivateState(filepath.Join(t.TempDir(), "runtime"), sessionID, generation, capability)
	if err != nil {
		t.Fatal(err)
	}
	return layout, capability
}

type runtimeFakeOwner struct {
	mu     sync.Mutex
	starts int
	sink   app.OutputSink
	handle *runtimeFakeHandle
}

func newRuntimeFakeOwner() *runtimeFakeOwner {
	return &runtimeFakeOwner{handle: newRuntimeFakeHandle()}
}

func (o *runtimeFakeOwner) Start(_ context.Context, _ operation.ExecutionSpec, sink app.OutputSink) (app.ProcessHandle, receipt.SpawnEvidence, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.starts++
	o.sink = sink
	return o.handle, receipt.SpawnEvidence{Attempted: true, Succeeded: true}, nil
}

func (o *runtimeFakeOwner) StartCount() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.starts
}

func (o *runtimeFakeOwner) Emit(data []byte) error {
	o.mu.Lock()
	sink := o.sink
	o.mu.Unlock()
	if sink == nil {
		return fmt.Errorf("sink unavailable")
	}
	err := sink.Append(context.Background(), data)
	if err != nil {
		sink.CaptureFailed(err)
	}
	return err
}

type runtimeFakeHandle struct {
	mu         sync.Mutex
	writes     [][]byte
	closeStdin int
	signals    []string
	wait       chan receipt.ExitEvidence
}

func newRuntimeFakeHandle() *runtimeFakeHandle {
	return &runtimeFakeHandle{wait: make(chan receipt.ExitEvidence, 1)}
}

func (h *runtimeFakeHandle) Write(data []byte) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.writes = append(h.writes, append([]byte(nil), data...))
	return nil
}
func (h *runtimeFakeHandle) CloseStdin() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closeStdin++
	return nil
}
func (h *runtimeFakeHandle) Signal(name string) receipt.SignalEvidence {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.signals = append(h.signals, name)
	return receipt.SignalEvidence{Requested: name, Attempted: true, Succeeded: true}
}
func (h *runtimeFakeHandle) Wait(ctx context.Context) receipt.ExitEvidence {
	select {
	case value := <-h.wait:
		return value
	case <-ctx.Done():
		return receipt.ExitEvidence{}
	}
}
func (h *runtimeFakeHandle) Close() error { return nil }
func (h *runtimeFakeHandle) PID() int     { return 4242 }

func (h *runtimeFakeHandle) FinishCode(code int) {
	h.wait <- receipt.ExitEvidence{Reaped: true, Code: &code}
}
func (h *runtimeFakeHandle) FinishSignal(signal string) {
	h.wait <- receipt.ExitEvidence{Reaped: true, Signal: signal}
}
func (h *runtimeFakeHandle) WriteCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.writes)
}
func (h *runtimeFakeHandle) CloseStdinCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.closeStdin
}
func (h *runtimeFakeHandle) SignalCount(name string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	count := 0
	for _, signal := range h.signals {
		if signal == name {
			count++
		}
	}
	return count
}
func (h *runtimeFakeHandle) Signals() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.signals...)
}

func TestRuntimeShutdownTerminatesWithEscalationAndFreezesKilled(t *testing.T) {
	layout, capability := runtimePrivateState(t, "persistent-session-shutdown", "generation-shutdown")
	owner := newRuntimeFakeOwner()
	runtime := mustRuntime(t, layout, capability, owner, operation.ExecutionSpec{Mode: operation.ExecutionModeShell, Shell: "/bin/sh", Executable: "/bin/sh", Command: "sleep 10", CWD: "/tmp"}, 64)
	runtime.terminationGrace = 5 * time.Millisecond
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- runtime.Shutdown(context.Background()) }()
	deadline := time.After(time.Second)
	for owner.handle.SignalCount("KILL") == 0 {
		select {
		case <-deadline:
			t.Fatalf("shutdown signals=%v", owner.handle.Signals())
		default:
			time.Sleep(time.Millisecond)
		}
	}
	owner.handle.FinishSignal("killed")
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	record, err := runtime.WaitTerminal(context.Background())
	if err != nil || record.State != session.Killed || record.Outcome != session.KilledOutcome {
		t.Fatalf("terminal=%#v err=%v", record, err)
	}
	if owner.handle.SignalCount("TERM") != 1 || owner.handle.SignalCount("KILL") != 1 {
		t.Fatalf("shutdown signals=%v", owner.handle.Signals())
	}
}
