package daemon

import (
	"context"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

type OutputSink interface {
	Append(context.Context, []byte) error
	CaptureFailed(error)
}
type ProcessOwner interface {
	Start(context.Context, operation.ExecutionSpec, OutputSink) (ProcessHandle, receipt.SpawnEvidence, error)
}

type ExecutionBinder interface {
	BindExecution(operation.ExecutionSpec) operation.ExecutionSpec
}

func bindExecution(owner ProcessOwner, spec operation.ExecutionSpec) operation.ExecutionSpec {
	if binder, ok := owner.(ExecutionBinder); ok {
		return binder.BindExecution(spec)
	}
	if spec.Mode == "" {
		spec.Mode = operation.ExecutionModeShell
	}
	if spec.Mode == operation.ExecutionModeShell {
		spec.Executable = spec.Shell
	} else if len(spec.Argv) > 0 {
		spec.Executable = spec.Argv[0]
	}
	return spec
}

type pidHandle interface {
	PID() int
}

type ProcessHandle interface {
	Write([]byte) error
	CloseStdin() error
	Signal(string) receipt.SignalEvidence
	Wait(context.Context) receipt.ExitEvidence
	Close() error
}

// resourceLimitHandle is optional. A handle that implements it must freeze the
// operation-local hard-limit result before Wait returns, so daemon terminal
// classification never races provider cleanup or later cgroup reuse.
type resourceLimitHandle interface {
	ResourceLimitBreach() operation.ResourceLimitKind
}

func resourceLimitBreachOf(handle ProcessHandle) operation.ResourceLimitKind {
	aware, ok := handle.(resourceLimitHandle)
	if !ok {
		return ""
	}
	switch kind := aware.ResourceLimitBreach(); kind {
	case operation.ResourceLimitMemory, operation.ResourceLimitProcesses:
		return kind
	default:
		return ""
	}
}
