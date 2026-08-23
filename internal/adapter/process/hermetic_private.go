//go:build linux || darwin

package process

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"syscall"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	hermeticapp "github.com/maemreyo/shellbeam/internal/app/hermetic"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

// StartPrivateHermetic starts the already-qualified provider command exactly as
// prepared. It deliberately bypasses host PATH/environment rebinding. The
// returned reader is the provider-private fd 3 status channel; callers must not
// derive child exit/signal evidence from it.
func (o Owner) StartPrivateHermetic(_ context.Context, command hermeticapp.ProviderCommand, sink app.OutputSink) (app.ProcessHandle, receipt.SpawnEvidence, io.ReadCloser, error) {
	if err := command.ValidatePrivate(); err != nil || command.StatusFD != 3 {
		if err == nil {
			err = errors.New("hermetic_status_fd_required")
		}
		return nil, receipt.SpawnEvidence{ErrorCode: "invalid_execution_spec"}, nil, err
	}
	var domain resourceExecutionDomain
	if command.ResourceLimits != nil {
		spawn := receipt.SpawnEvidence{ErrorCode: "resource_enforcement_failed"}
		if o.resources == nil {
			return nil, spawn, nil, errors.New("resource_enforcement_unavailable")
		}
		var err error
		domain, err = o.resources.prepareExecution(*command.ResourceLimits)
		if err != nil {
			return nil, spawn, nil, err
		}
		if domain == nil {
			return nil, spawn, nil, errors.New("resource_enforcement_unavailable")
		}
	}
	return startPrivateHermeticNonTTY(command, sink, domain)
}

func startPrivateHermeticNonTTY(command hermeticapp.ProviderCommand, sink app.OutputSink, domain resourceExecutionDomain) (app.ProcessHandle, receipt.SpawnEvidence, io.ReadCloser, error) {
	spawn := receipt.SpawnEvidence{Attempted: true}
	cmd := exec.Command(command.Executable, command.Argv[1:]...)
	cmd.Args = append([]string(nil), command.Argv...)
	cmd.Dir = command.Dir
	cmd.Env = append([]string{}, command.Env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		spawn.ErrorCode = "stdin_pipe_failed"
		abortResourceDomain(domain)
		return nil, spawn, nil, err
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		_ = stdinR.Close()
		_ = stdinW.Close()
		spawn.ErrorCode = "output_pipe_failed"
		abortResourceDomain(domain)
		return nil, spawn, nil, err
	}
	statusR, statusW, err := os.Pipe()
	if err != nil {
		_ = stdinR.Close()
		_ = stdinW.Close()
		_ = outR.Close()
		_ = outW.Close()
		spawn.ErrorCode = "provider_status_pipe_failed"
		abortResourceDomain(domain)
		return nil, spawn, nil, err
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdinR, outW, outW
	cmd.ExtraFiles = []*os.File{statusW}

	var binding resourceSpawnBinding
	if domain != nil {
		binding, err = domain.bind(cmd)
		if err != nil {
			closePrivateHermeticSpawnFiles(stdinR, stdinW, outR, outW, statusR, statusW)
			spawn.ErrorCode = "resource_enforcement_failed"
			abortResourceDomain(domain)
			return nil, spawn, nil, err
		}
	}
	if err = cmd.Start(); binding != nil {
		_ = binding.Close()
	}
	if err != nil {
		closePrivateHermeticSpawnFiles(stdinR, stdinW, outR, outW, statusR, statusW)
		spawn.ErrorCode = startErrorCode(operation.ExecutionSpec{CWD: command.Dir, Executable: command.Executable}, err)
		abortResourceDomain(domain)
		return nil, spawn, nil, err
	}
	_ = stdinR.Close()
	_ = outW.Close()
	_ = statusW.Close()
	spawn.Succeeded = true
	if domain != nil {
		domain.startMonitoring()
	}
	h := &Handle{
		cmd: cmd, stdin: stdinW, output: outR, sink: sink,
		wait: make(chan receipt.ExitEvidence, 1), captureDone: make(chan struct{}), resourceDomain: domain,
	}
	h.writeMu.Lock()
	_ = h.endStdin()
	h.writeMu.Unlock()
	go h.capture()
	go h.reap()
	return h, spawn, statusR, nil
}

func closePrivateHermeticSpawnFiles(files ...*os.File) {
	for _, file := range files {
		if file != nil {
			_ = file.Close()
		}
	}
}
