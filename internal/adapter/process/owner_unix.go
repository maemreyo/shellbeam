//go:build linux || darwin

// Package process owns live POSIX child capabilities for the current daemon incarnation.
package process

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

type Owner struct{}
type Handle struct {
	cmd         *exec.Cmd
	stdin       *os.File
	output      *os.File
	sink        app.OutputSink
	writeMu     sync.Mutex
	wait        chan receipt.ExitEvidence
	captureDone chan struct{}
	closeOnce   sync.Once
	// stdinClosed is guarded by writeMu. Both the policy close at spawn and an
	// explicit end-of-input go through the same primitive, so the write end is
	// closed exactly once however input ends.
	stdinClosed bool
}

// ErrStdinClosed reports input offered to a child whose standard input has
// already ended.
var ErrStdinClosed = errors.New("stdin_closed")

// endStdin closes the write end once, whoever asks.
func (h *Handle) endStdin() error {
	var err error
	h.closeOnce.Do(func() { err = h.stdin.Close() })
	h.stdinClosed = true
	return err
}

func (Owner) Start(_ context.Context, spec operation.ExecutionSpec, sink app.OutputSink) (app.ProcessHandle, receipt.SpawnEvidence, error) {
	if spec.TTY {
		return startPTY(spec, sink)
	}
	return startNonTTY(spec, sink, commandFor)
}

type FrozenOwner struct{}

func (FrozenOwner) Start(_ context.Context, spec operation.ExecutionSpec, sink app.OutputSink) (app.ProcessHandle, receipt.SpawnEvidence, error) {
	if spec.TTY {
		return nil, receipt.SpawnEvidence{Attempted: true, ErrorCode: "tty_unsupported"}, errors.New("tty_unsupported")
	}
	return startNonTTY(spec, sink, commandForFrozen)
}

func startNonTTY(spec operation.ExecutionSpec, sink app.OutputSink, build func(operation.ExecutionSpec) (*exec.Cmd, string, error)) (app.ProcessHandle, receipt.SpawnEvidence, error) {
	spawn := receipt.SpawnEvidence{Attempted: true}
	cmd, code, err := build(spec)
	if err != nil {
		spawn.ErrorCode = code
		return nil, spawn, err
	}
	cmd.Dir = spec.CWD
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		spawn.ErrorCode = "stdin_pipe_failed"
		return nil, spawn, err
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		_ = stdinR.Close()
		_ = stdinW.Close()
		spawn.ErrorCode = "output_pipe_failed"
		return nil, spawn, err
	}
	cmd.Stdin = stdinR
	cmd.Stdout = outW
	cmd.Stderr = outW
	if err = cmd.Start(); err != nil {
		_ = stdinR.Close()
		_ = stdinW.Close()
		_ = outR.Close()
		_ = outW.Close()
		spawn.ErrorCode = startErrorCode(spec, err)
		return nil, spawn, err
	}
	_ = stdinR.Close()
	_ = outW.Close()
	spawn.Succeeded = true
	h := &Handle{cmd: cmd, stdin: stdinW, output: outR, sink: sink, wait: make(chan receipt.ExitEvidence, 1), captureDone: make(chan struct{})}
	if spec.StdinMode == operation.StdinModeClosed {
		// Deliver EOF now, so a child that reads its input finishes instead of
		// waiting for a caller who was never going to write. Dropping the write
		// end here also means this session never holds it open at all.
		h.writeMu.Lock()
		_ = h.endStdin()
		h.writeMu.Unlock()
	}
	go h.capture()
	go h.reap()
	return h, spawn, nil
}

func (h *Handle) PID() int {
	if h == nil || h.cmd == nil || h.cmd.Process == nil {
		return 0
	}
	return h.cmd.Process.Pid
}

func (h *Handle) capture() {
	defer close(h.captureDone)
	b := make([]byte, 32*1024)
	for {
		n, err := h.output.Read(b)
		if n > 0 {
			if e := h.sink.Append(context.Background(), append([]byte(nil), b[:n]...)); e != nil {
				h.sink.CaptureFailed(e)
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				h.sink.CaptureFailed(err)
			}
			return
		}
	}
}
func (h *Handle) reap() {
	err := h.cmd.Wait()
	<-h.captureDone
	_ = h.output.Close()
	e := receipt.ExitEvidence{Reaped: true}
	if err == nil {
		zero := 0
		e.Code = &zero
	} else if x, ok := err.(*exec.ExitError); ok {
		status := x.Sys().(syscall.WaitStatus)
		if status.Signaled() {
			e.Signal = status.Signal().String()
		} else {
			code := status.ExitStatus()
			e.Code = &code
		}
	}
	h.wait <- e
	close(h.wait)
}
func (h *Handle) Wait(ctx context.Context) receipt.ExitEvidence {
	select {
	case e := <-h.wait:
		return e
	case <-ctx.Done():
		return receipt.ExitEvidence{}
	}
}
func (h *Handle) Write(b []byte) error {
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	if h.stdinClosed {
		return ErrStdinClosed
	}
	for len(b) > 0 {
		n, err := h.stdin.Write(b)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}
func (h *Handle) CloseStdin() error {
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	return h.endStdin()
}
func (h *Handle) Signal(name string) receipt.SignalEvidence {
	e := receipt.SignalEvidence{Requested: name, Attempted: true}
	sig := map[string]syscall.Signal{"INT": syscall.SIGINT, "TERM": syscall.SIGTERM, "KILL": syscall.SIGKILL}[name]
	if sig == 0 {
		return e
	}
	if err := syscall.Kill(-h.cmd.Process.Pid, sig); err == nil {
		e.Succeeded = true
	}
	return e
}
func (h *Handle) Close() error {
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	return h.endStdin()
}

// startErrorCode names why a child could not start.
//
// The operating system reports a missing working directory and a missing
// executable the same way, and reporting both as "spawn_failed" left an agent
// to find out which by running pwd and ls. Every spawn failure in the corpus
// that prompted this was a working directory that did not exist -- twice a path
// that had never been created, once a hyphen typed as an underscore -- and
// naming that turns three tool calls of guessing into one corrected cwd.
func startErrorCode(spec operation.ExecutionSpec, err error) string {
	if spec.CWD != "" {
		if info, statErr := os.Stat(spec.CWD); statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				return "cwd_not_found"
			}
			if errors.Is(statErr, os.ErrPermission) {
				return "permission_denied"
			}
		} else if !info.IsDir() {
			return "cwd_not_directory"
		}
	}
	switch {
	case errors.Is(err, os.ErrNotExist):
		return "executable_not_found"
	case errors.Is(err, os.ErrPermission):
		return "permission_denied"
	case errors.Is(err, syscall.EMFILE), errors.Is(err, syscall.ENFILE), errors.Is(err, syscall.EAGAIN), errors.Is(err, syscall.ENOMEM):
		return "resource_exhausted"
	}
	return "spawn_failed"
}
