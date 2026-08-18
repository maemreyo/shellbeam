package daemon_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

type resourceTerminalOwner struct{ handle *resourceTerminalHandle }

func (o resourceTerminalOwner) Start(context.Context, operation.ExecutionSpec, app.OutputSink) (app.ProcessHandle, receipt.SpawnEvidence, error) {
	return o.handle, receipt.SpawnEvidence{Attempted: true, Succeeded: true}, nil
}

type resourceTerminalHandle struct {
	breach          operation.ResourceLimitKind
	cleanupReason   string
	exit            chan receipt.ExitEvidence
	releaseOnSignal bool
	once            sync.Once
}

func newResourceTerminalHandle(kind operation.ResourceLimitKind) *resourceTerminalHandle {
	return &resourceTerminalHandle{breach: kind, exit: make(chan receipt.ExitEvidence, 1)}
}
func (h *resourceTerminalHandle) Write([]byte) error { return nil }
func (h *resourceTerminalHandle) CloseStdin() error  { return nil }
func (h *resourceTerminalHandle) Close() error       { return nil }
func (h *resourceTerminalHandle) Wait(ctx context.Context) receipt.ExitEvidence {
	select {
	case exit := <-h.exit:
		return exit
	case <-ctx.Done():
		return receipt.ExitEvidence{}
	}
}
func (h *resourceTerminalHandle) Signal(signal string) receipt.SignalEvidence {
	if h.releaseOnSignal {
		h.once.Do(func() {
			h.exit <- receipt.ExitEvidence{Reaped: true, Signal: signalNameForTest(signal)}
		})
	}
	return receipt.SignalEvidence{Requested: signal, Attempted: true, Succeeded: true}
}
func (h *resourceTerminalHandle) ResourceLimitBreach() operation.ResourceLimitKind { return h.breach }
func (h *resourceTerminalHandle) ResourceCleanupIncomplete() string                { return h.cleanupReason }
func (h *resourceTerminalHandle) Reap(exit receipt.ExitEvidence) {
	h.once.Do(func() { h.exit <- exit })
}

func signalNameForTest(signal string) string {
	switch signal {
	case "KILL":
		return "killed"
	case "TERM":
		return "terminated"
	default:
		return signal
	}
}

func resourceTerminalCatalog() capability.Catalog {
	return capability.Baseline(capability.Limits{}).WithResourceEnforcement(capability.ResourceEnforcementSupport{
		Version: 1, Maturity: "experimental", Provider: "test", Scope: "owned_process_tree", Placement: "pre_exec_atomic",
		MemoryBytes: capability.EnforcementHard, Processes: capability.EnforcementHard,
		CPUTimeMS: capability.EnforcementUnsupported, PersistentSessions: capability.EnforcementUnsupported,
	})
}

func resourceTerminalService(t *testing.T, handle *resourceTerminalHandle) *app.Service {
	t.Helper()
	st, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{
		MaxSessions: 8, MaxSessionOutput: 1 << 20, MaxTotalState: 1 << 28, ControlReserve: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	return app.NewService(st, resourceTerminalOwner{handle: handle}, app.Options{
		Incarnation: "resource-terminal", Shell: "/bin/sh", MaxQueuedInputBytes: 1024,
		DefaultTimeoutMS: 600000, MaxTimeoutMS: 86400000, TerminationGrace: 10 * time.Millisecond,
		Capabilities: resourceTerminalCatalog(),
	})
}

func TestResourceMemoryBreachMakesSuccessfulExitAFailedResourceOperation(t *testing.T) {
	handle := newResourceTerminalHandle(operation.ResourceLimitMemory)
	svc := resourceTerminalService(t, handle)
	limits := &operation.ResourceLimits{MemoryBytes: 64 << 20}
	started, err := svc.Start(context.Background(), app.StartRequest{
		ProtocolVersion: 2, OperationID: "resource-memory", Command: "true", CWD: "/tmp", ResourceLimits: limits, YieldMS: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	zero := 0
	handle.Reap(receipt.ExitEvidence{Reaped: true, Code: &zero})
	terminal := waitForTerminal(t, svc, started.SessionID)
	if terminal.State != session.Failed || terminal.Outcome != session.Failure || terminal.Receipt == nil {
		t.Fatalf("terminal=%#v", terminal)
	}
	if terminal.Receipt.FailureReason != "resource_limit_memory" {
		t.Fatalf("reason=%q", terminal.Receipt.FailureReason)
	}
	if terminal.Receipt.Exit.Code == nil || *terminal.Receipt.Exit.Code != 0 || terminal.Receipt.Exit.Signal != "" {
		t.Fatalf("literal exit evidence was rewritten: %#v", terminal.Receipt.Exit)
	}
	if terminal.Failure == nil || terminal.Failure.Stage != receipt.StageExecution || terminal.Failure.Class != receipt.ClassResource || terminal.Failure.Code != "resource_limit" || terminal.Failure.Details["resource_limit_kind"] != "memory" {
		t.Fatalf("derived failure=%#v", terminal.Failure)
	}
}

func TestResourceProcessBreachPreservesLiteralSignalEvidence(t *testing.T) {
	handle := newResourceTerminalHandle(operation.ResourceLimitProcesses)
	svc := resourceTerminalService(t, handle)
	limits := &operation.ResourceLimits{Processes: 3}
	started, err := svc.Start(context.Background(), app.StartRequest{
		ProtocolVersion: 2, OperationID: "resource-processes", Command: "true", CWD: "/tmp", ResourceLimits: limits, YieldMS: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	handle.Reap(receipt.ExitEvidence{Reaped: true, Signal: "killed"})
	terminal := waitForTerminal(t, svc, started.SessionID)
	if terminal.Receipt == nil || terminal.Receipt.FailureReason != "resource_limit_processes" {
		t.Fatalf("terminal=%#v", terminal)
	}
	if terminal.Receipt.Exit.Signal != "killed" || terminal.Receipt.Exit.Code != nil {
		t.Fatalf("literal signal evidence was rewritten: %#v", terminal.Receipt.Exit)
	}
	if terminal.Failure == nil || terminal.Failure.Class != receipt.ClassResource || terminal.Failure.Details["resource_limit_kind"] != "processes" {
		t.Fatalf("derived failure=%#v", terminal.Failure)
	}
}

func TestExplicitKillWinsOverResourceBreachAlreadyFrozenByHandle(t *testing.T) {
	handle := newResourceTerminalHandle(operation.ResourceLimitMemory)
	handle.releaseOnSignal = true
	svc := resourceTerminalService(t, handle)
	limits := &operation.ResourceLimits{MemoryBytes: 64 << 20}
	started, err := svc.Start(context.Background(), app.StartRequest{
		ProtocolVersion: 2, OperationID: "resource-explicit-kill", Command: "true", CWD: "/tmp", ResourceLimits: limits, YieldMS: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Kill(context.Background(), app.KillRequest{SessionID: started.SessionID, KillID: "kill-resource", Signal: "KILL"}); err != nil {
		t.Fatal(err)
	}
	terminal := waitForTerminal(t, svc, started.SessionID)
	if terminal.State != session.Killed || terminal.Outcome != session.KilledOutcome || terminal.Receipt == nil {
		t.Fatalf("terminal=%#v", terminal)
	}
	if terminal.Receipt.FailureReason == "resource_limit_memory" || terminal.Failure == nil || terminal.Failure.Code != "killed" {
		t.Fatalf("resource breach overrode explicit kill: receipt=%#v failure=%#v", terminal.Receipt, terminal.Failure)
	}
}

func TestTimeoutWinsOverResourceBreachAlreadyFrozenByHandle(t *testing.T) {
	handle := newResourceTerminalHandle(operation.ResourceLimitMemory)
	handle.releaseOnSignal = true
	svc := resourceTerminalService(t, handle)
	limits := &operation.ResourceLimits{MemoryBytes: 64 << 20}
	started, err := svc.Start(context.Background(), app.StartRequest{
		ProtocolVersion: 2, OperationID: "resource-timeout", Command: "true", CWD: "/tmp", ResourceLimits: limits,
		TimeoutMS: 20, YieldMS: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	terminal := waitForTerminal(t, svc, started.SessionID)
	if terminal.State != session.TimedOut || terminal.Outcome != session.Timeout || terminal.Receipt == nil {
		t.Fatalf("terminal=%#v", terminal)
	}
	if terminal.Receipt.FailureReason == "resource_limit_memory" || terminal.Failure == nil || terminal.Failure.Code != "timed_out" {
		t.Fatalf("resource breach overrode timeout: receipt=%#v failure=%#v", terminal.Receipt, terminal.Failure)
	}
}

func TestResourceCleanupIncompletePersistsSeparatelyFromSuccessfulChildOutcome(t *testing.T) {
	handle := newResourceTerminalHandle("")
	handle.cleanupReason = "cleanup_remove_failed"
	svc := resourceTerminalService(t, handle)
	limits := &operation.ResourceLimits{MemoryBytes: 64 << 20}
	started, err := svc.Start(context.Background(), app.StartRequest{
		ProtocolVersion: 2, OperationID: "resource-cleanup-incomplete", Command: "true", CWD: "/tmp", ResourceLimits: limits, YieldMS: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	zero := 0
	handle.Reap(receipt.ExitEvidence{Reaped: true, Code: &zero})
	terminal := waitForTerminal(t, svc, started.SessionID)
	if terminal.State != session.Completed || terminal.Outcome != session.Success || terminal.Failure != nil || terminal.Receipt == nil {
		t.Fatalf("cleanup metadata changed child/operation truth: %#v", terminal)
	}
	if terminal.Receipt.ResourceCleanup == nil || terminal.Receipt.ResourceCleanup.Status != "incomplete" || terminal.Receipt.ResourceCleanup.Reason != "cleanup_remove_failed" {
		t.Fatalf("cleanup metadata=%#v", terminal.Receipt.ResourceCleanup)
	}
}
