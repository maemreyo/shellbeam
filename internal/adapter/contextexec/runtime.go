package contextexec

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/contextexec"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

type ChildSpec struct {
	Argv             []string
	HelperExecutable string
	CWD              string
	Env              []string
}

type ChildExit struct {
	Reaped bool
	Code   *int
	Signal string
}

type ChildProcess struct {
	ResolvedExecutable string
	Stdout             io.ReadCloser
	Stderr             io.ReadCloser
	Wait               func() (ChildExit, error)
	KillGroup          func() error
}

type PreparedExecution interface {
	ResolvedExecutable() string
	Start() (*ChildProcess, error)
	Close() error
}

type ChildLauncher interface {
	Qualified() bool
	Prepare(ChildSpec) (PreparedExecution, error)
}

type ExecutionProtocol interface {
	AuthorizePrepared(context.Context, PreparedFrame) (ExecuteFrame, error)
	SendSpawn(SpawnFrame) error
	SendOutput(OutputFrame) error
}

type Runtime struct {
	Launcher         ChildLauncher
	HelperExecutable string
	Environ          func() []string
	Getwd            func() (string, error)
}

func (r Runtime) Execute(ctx context.Context, request RequestFrame, protocol ExecutionProtocol) (TerminalFrame, error) {
	if err := request.Validate(); err != nil {
		return TerminalFrame{}, err
	}
	if protocol == nil {
		return TerminalFrame{}, contextRuntimeFailure(request, failure.ContextExecUnavailable, "execution_protocol_unavailable")
	}
	if r.Launcher == nil || !r.Launcher.Qualified() {
		return TerminalFrame{}, contextRuntimeFailure(request, failure.ContextExecUnavailable, "strong_executor_unavailable")
	}
	spec, err := r.childSpec(request)
	if err != nil {
		return TerminalFrame{}, err
	}
	prepared, execute, resolved, err := r.prepareAuthorizedExecution(ctx, request, protocol, spec)
	if err != nil {
		return TerminalFrame{}, err
	}
	child, err := startAuthorizedExecution(request, protocol, prepared, execute, resolved)
	if err != nil {
		return TerminalFrame{}, err
	}
	return collectChildTerminal(ctx, request, protocol, child)
}

func (r Runtime) prepareAuthorizedExecution(ctx context.Context, request RequestFrame, protocol ExecutionProtocol, spec ChildSpec) (PreparedExecution, ExecuteFrame, string, error) {
	prepared, err := r.Launcher.Prepare(spec)
	if err != nil {
		_, ackErr := protocol.AuthorizePrepared(ctx, PreparedFrame{ProtocolVersion: ProtocolVersion, Kind: KindPrepared, FailureCode: string(failure.ContextExecUnavailable)})
		if ackErr != nil {
			return nil, ExecuteFrame{}, "", contextRuntimeFailure(request, failure.ContextHelperLost, "prepare_failure_ack_lost")
		}
		return nil, ExecuteFrame{}, "", contextRuntimeFailure(request, failure.ContextExecUnavailable, "prepare_failed")
	}
	if prepared == nil {
		return nil, ExecuteFrame{}, "", contextRuntimeFailure(request, failure.ContextExecUnavailable, "prepared_executor_unavailable")
	}
	resolved := prepared.ResolvedExecutable()
	if resolved == "" || !filepath.IsAbs(resolved) {
		_ = prepared.Close()
		return nil, ExecuteFrame{}, "", contextRuntimeFailure(request, failure.ContextExecUnavailable, "prepared_executable_unproven")
	}
	execute, err := protocol.AuthorizePrepared(ctx, PreparedFrame{ProtocolVersion: ProtocolVersion, Kind: KindPrepared, ResolvedExecutable: resolved})
	if err != nil {
		_ = prepared.Close()
		return nil, ExecuteFrame{}, "", contextRuntimeFailure(request, failure.ContextHelperLost, "execute_ack_lost")
	}
	if err := execute.Validate(); err != nil || !execute.Authorized || !ExecutableMatches(execute.ResolvedExecutable, resolved) {
		_ = prepared.Close()
		return nil, ExecuteFrame{}, "", contextRuntimeFailure(request, failure.ContextExecUnavailable, "execute_not_authorized")
	}
	return prepared, execute, resolved, nil
}

func startAuthorizedExecution(request RequestFrame, protocol ExecutionProtocol, prepared PreparedExecution, execute ExecuteFrame, resolved string) (*ChildProcess, error) {
	child, startErr := prepared.Start()
	closeErr := prepared.Close()
	if startErr != nil {
		spawn := SpawnFrame{
			ProtocolVersion:    ProtocolVersion,
			Kind:               KindSpawn,
			ChildOperationID:   execute.ChildOperationID,
			ChildSessionID:     execute.ChildSessionID,
			ResolvedExecutable: resolved,
			Spawn:              receipt.SpawnEvidence{Attempted: true, Succeeded: false, ErrorCode: string(failure.ContextExecUnavailable)},
		}
		if err := protocol.SendSpawn(spawn); err != nil {
			return nil, contextRuntimeFailure(request, failure.ContextHelperLost, "spawn_failure_report_lost")
		}
		return nil, contextRuntimeFailure(request, failure.ContextExecUnavailable, "spawn_failed")
	}
	if child == nil || child.Stdout == nil || child.Stderr == nil || child.Wait == nil || child.KillGroup == nil || child.ResolvedExecutable == "" || !filepath.IsAbs(child.ResolvedExecutable) || !ExecutableMatches(child.ResolvedExecutable, resolved) {
		killAndReapChild(child)
		return nil, contextRuntimeFailure(request, failure.ContextExecUnavailable, "executor_identity_unproven")
	}
	if closeErr != nil {
		killAndReapChild(child)
		return nil, contextRuntimeFailure(request, failure.ContextHelperLost, "prepared_close_failed")
	}
	spawn := SpawnFrame{ProtocolVersion: ProtocolVersion, Kind: KindSpawn, ChildOperationID: execute.ChildOperationID, ChildSessionID: execute.ChildSessionID, ResolvedExecutable: resolved, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}}
	if err := protocol.SendSpawn(spawn); err != nil {
		killAndReapChild(child)
		return nil, contextRuntimeFailure(request, failure.ContextHelperLost, "spawn_report_lost")
	}
	return child, nil
}

func collectChildTerminal(ctx context.Context, request RequestFrame, protocol ExecutionProtocol, child *ChildProcess) (TerminalFrame, error) {
	outputCh := make(chan struct {
		value CanonicalOutput
		err   error
	}, 1)
	go func() {
		value, err := CaptureOutput(child.Stdout, child.Stderr, request.Request.MaxOutputBytes, protocol.SendOutput)
		outputCh <- struct {
			value CanonicalOutput
			err   error
		}{value, err}
	}()
	waitCh := make(chan struct {
		exit ChildExit
		err  error
	}, 1)
	go func() {
		exit, err := child.Wait()
		waitCh <- struct {
			exit ChildExit
			err  error
		}{exit, err}
	}()
	waited, timedOut, signal := waitForChild(ctx, request.Request.TimeoutMS, child, waitCh)
	captured := <-outputCh
	if waited.err != nil {
		return TerminalFrame{}, contextRuntimeFailure(request, failure.ContextHelperLost, "wait_failed")
	}
	if captured.err != nil {
		return TerminalFrame{}, contextRuntimeFailure(request, failure.ContextHelperLost, "output_failed")
	}
	if !waited.exit.Reaped {
		return TerminalFrame{}, contextRuntimeFailure(request, failure.ContextHelperLost, "reap_unproven")
	}
	quality := core.EvidenceQualityComplete
	if !captured.value.Complete {
		quality = core.EvidenceQualityIncomplete
	}
	result := core.Result{
		SchemaVersion: core.SchemaVersion, ContextExecID: request.Request.ContextExecID, RequestFingerprint: request.Helper.RequestFingerprint, Lifecycle: core.LifecycleChildTerminal,
		Context: request.Context, Helper: &request.Helper, Executable: core.ExecutableIdentity{Requested: request.Request.Argv[0], ResolvedPath: child.ResolvedExecutable},
		Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}, Exit: receipt.ExitEvidence{Reaped: true, Code: waited.exit.Code, Signal: waited.exit.Signal}, Signal: signal, TimedOut: timedOut,
		Output:          core.OutputEvidence{StdoutBytes: captured.value.StdoutBytes, StderrBytes: captured.value.StderrBytes, OutputComplete: captured.value.Complete, Truncated: captured.value.Truncated, Attribution: core.OutputAttributionHelperOwnedChildPipes},
		EvidenceQuality: quality, EvidenceAuthority: "",
	}
	terminal := TerminalFrame{ProtocolVersion: ProtocolVersion, Kind: KindTerminal, Result: result}
	if err := terminal.Validate(); err != nil {
		return TerminalFrame{}, fmt.Errorf("invalid context child terminal result: %w", err)
	}
	return terminal, nil
}

func killAndReapChild(child *ChildProcess) {
	if child == nil {
		return
	}
	if child.KillGroup != nil {
		_ = child.KillGroup()
	}
	if child.Wait != nil {
		_, _ = child.Wait()
	}
}

func (r Runtime) childSpec(request RequestFrame) (ChildSpec, error) {
	getwd := r.Getwd
	if getwd == nil {
		getwd = os.Getwd
	}
	cwd, err := getwd()
	if err != nil {
		return ChildSpec{}, contextRuntimeFailure(request, failure.ContextExecBoundaryUnproven, "cwd_unavailable")
	}
	if !sameDirectoryIdentity(cwd, request.Context.CWDObserved) {
		return ChildSpec{}, contextRuntimeFailure(request, failure.ContextExecBoundaryUnproven, "cwd_drift")
	}
	environ := r.Environ
	if environ == nil {
		environ = os.Environ
	}
	return ChildSpec{
		Argv:             append([]string(nil), request.Request.Argv...),
		HelperExecutable: r.HelperExecutable,
		CWD:              request.Context.CWDObserved,
		Env:              SanitizeChildEnvironment(environ()),
	}, nil
}

func waitForChild(ctx context.Context, timeoutMS int64, child *ChildProcess, waitCh <-chan struct {
	exit ChildExit
	err  error
}) (struct {
	exit ChildExit
	err  error
}, bool, receipt.SignalEvidence) {
	if timeoutMS == 0 {
		select {
		case waited := <-waitCh:
			return waited, false, receipt.SignalEvidence{}
		case <-ctx.Done():
			err := child.KillGroup()
			waited := <-waitCh
			return waited, false, receipt.SignalEvidence{Requested: "SIGKILL", Attempted: true, Succeeded: err == nil}
		}
	}
	timer := time.NewTimer(time.Duration(timeoutMS) * time.Millisecond)
	defer timer.Stop()
	select {
	case waited := <-waitCh:
		return waited, false, receipt.SignalEvidence{}
	case <-timer.C:
		err := child.KillGroup()
		waited := <-waitCh
		return waited, true, receipt.SignalEvidence{Requested: "SIGKILL", Attempted: true, Succeeded: err == nil}
	case <-ctx.Done():
		err := child.KillGroup()
		waited := <-waitCh
		return waited, false, receipt.SignalEvidence{Requested: "SIGKILL", Attempted: true, Succeeded: err == nil}
	}
}

func contextRuntimeFailure(request RequestFrame, code failure.Code, reason string) error {
	return failure.New(code, map[string]string{"context_exec_id": request.Request.ContextExecID, "session_id": request.Request.SessionID, "reason": reason}, nil)
}
