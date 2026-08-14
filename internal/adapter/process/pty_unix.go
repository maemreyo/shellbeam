//go:build linux || darwin

package process

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"syscall"

	"github.com/creack/pty"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

type ptyHandle struct {
	cmd         *exec.Cmd
	terminal    io.ReadWriteCloser
	sink        app.OutputSink
	writeMu     sync.Mutex
	wait        chan receipt.ExitEvidence
	captureDone chan struct{}
}

func startPTY(spec operation.ExecutionSpec, sink app.OutputSink) (app.ProcessHandle, receipt.SpawnEvidence, error) {
	spawn := receipt.SpawnEvidence{Attempted: true}
	cmd, code, err := commandFor(spec)
	if err != nil {
		spawn.ErrorCode = code
		return nil, spawn, err
	}
	cmd.Dir = spec.CWD
	terminal, err := pty.Start(cmd)
	if err != nil {
		spawn.ErrorCode = "spawn_failed"
		return nil, spawn, err
	}
	spawn.Succeeded = true
	h := &ptyHandle{cmd: cmd, terminal: terminal, sink: sink, wait: make(chan receipt.ExitEvidence, 1), captureDone: make(chan struct{})}
	go h.capture()
	go h.reap()
	return h, spawn, nil
}
func (h *ptyHandle) capture() {
	defer close(h.captureDone)
	b := make([]byte, 32*1024)
	for {
		n, err := h.terminal.Read(b)
		if n > 0 {
			if e := h.sink.Append(context.Background(), append([]byte(nil), b[:n]...)); e != nil {
				h.sink.CaptureFailed(e)
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, syscall.EIO) {
				h.sink.CaptureFailed(err)
			}
			return
		}
	}
}
func (h *ptyHandle) reap() {
	err := h.cmd.Wait()
	<-h.captureDone
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
func (h *ptyHandle) Wait(ctx context.Context) receipt.ExitEvidence {
	select {
	case e := <-h.wait:
		return e
	case <-ctx.Done():
		return receipt.ExitEvidence{}
	}
}
func (h *ptyHandle) Write(b []byte) error {
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	for len(b) > 0 {
		n, err := h.terminal.Write(b)
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
func (h *ptyHandle) CloseStdin() error { return fmt.Errorf("input_eof_unsupported") }
func (h *ptyHandle) Signal(name string) receipt.SignalEvidence {
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
func (h *ptyHandle) Close() error { return h.terminal.Close() }
