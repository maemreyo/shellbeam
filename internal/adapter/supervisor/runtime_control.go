//go:build linux || darwin

package supervisor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/session"
)

func (r *Runtime) timeoutLoop() {
	r.mu.Lock()
	deadline := r.timeout.Deadline
	r.mu.Unlock()
	delay := time.Until(deadline)
	if delay < 0 {
		delay = 0
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-r.done:
		return
	}

	r.controlMu.Lock()
	r.mu.Lock()
	if r.state != session.Running || r.handle == nil {
		r.mu.Unlock()
		r.controlMu.Unlock()
		return
	}
	r.setTargetLocked(session.TimedOut)
	handle := r.handle
	r.mu.Unlock()
	term := handle.Signal("TERM")
	r.mu.Lock()
	r.signal = term
	r.timeout.Fired = true
	r.timeout.Term = term
	timeoutState := r.timeout
	r.notifyLocked()
	r.mu.Unlock()
	_ = persistTimeoutState(r.layout, timeoutState)
	r.controlMu.Unlock()

	grace := time.NewTimer(r.terminationGrace)
	defer grace.Stop()
	select {
	case <-grace.C:
	case <-r.done:
		return
	}

	r.controlMu.Lock()
	defer r.controlMu.Unlock()
	r.mu.Lock()
	if r.state != session.Running && r.state != session.Finalizing || r.handle == nil {
		r.mu.Unlock()
		return
	}
	handle = r.handle
	r.mu.Unlock()
	kill := handle.Signal("KILL")
	r.mu.Lock()
	r.signal = kill
	r.timeout.Kill = kill
	timeoutState = r.timeout
	r.notifyLocked()
	r.mu.Unlock()
	_ = persistTimeoutState(r.layout, timeoutState)
}

func (r *Runtime) failInputDelivery() {
	r.mu.Lock()
	if r.inputErr != nil {
		r.mu.Unlock()
		return
	}
	r.inputErr = fmt.Errorf("input_delivery_failed")
	r.setTargetLocked(session.Killed)
	handle := r.handle
	r.mu.Unlock()
	if handle != nil {
		evidence := handle.Signal("TERM")
		r.mu.Lock()
		r.signal = evidence
		r.notifyLocked()
		r.mu.Unlock()
	}
}

func (r *Runtime) captureFailure(err error) {
	if err == nil {
		return
	}
	r.mu.Lock()
	if r.captureErr != nil {
		r.mu.Unlock()
		return
	}
	r.captureErr = fmt.Errorf("output_capture_failed")
	r.setTargetLocked(session.Killed)
	handle := r.handle
	r.mu.Unlock()
	if handle != nil {
		evidence := handle.Signal("TERM")
		r.mu.Lock()
		r.signal = evidence
		r.notifyLocked()
		r.mu.Unlock()
	}
}

func (r *Runtime) terminateForFailure(reason string) {
	if reason == "output_capture_failed" {
		r.captureFailure(errors.New(reason))
		return
	}
	r.mu.Lock()
	r.setTargetLocked(session.Killed)
	handle := r.handle
	r.mu.Unlock()
	if handle != nil {
		evidence := handle.Signal("TERM")
		r.mu.Lock()
		r.signal = evidence
		r.notifyLocked()
		r.mu.Unlock()
	}
}

func (r *Runtime) setTargetLocked(target session.State) {
	if r.target == "" {
		r.target = target
	}
}

func (r *Runtime) notifyLocked() {
	r.change++
	close(r.changed)
	r.changed = make(chan struct{})
}

type runtimeSink struct{ runtime *Runtime }

func (s runtimeSink) Append(_ context.Context, data []byte) error {
	_, err := s.runtime.spool.AppendRange(data)
	if err != nil {
		s.runtime.captureFailure(err)
	}
	return err
}

func (s runtimeSink) CaptureFailed(err error) { s.runtime.captureFailure(err) }

func (r *Runtime) Shutdown(ctx context.Context) error {
	r.controlMu.Lock()
	r.mu.Lock()
	if r.state.Terminal() {
		r.mu.Unlock()
		r.controlMu.Unlock()
		_, err := r.WaitTerminal(ctx)
		return err
	}
	if r.handle == nil {
		r.mu.Unlock()
		r.controlMu.Unlock()
		return fmt.Errorf("session_not_live")
	}
	r.setTargetLocked(session.Killed)
	handle := r.handle
	r.mu.Unlock()
	term := handle.Signal("TERM")
	r.mu.Lock()
	r.signal = term
	r.notifyLocked()
	r.mu.Unlock()
	r.controlMu.Unlock()

	grace := time.NewTimer(r.terminationGrace)
	defer grace.Stop()
	select {
	case <-r.done:
		_, err := r.WaitTerminal(ctx)
		return err
	case <-grace.C:
	case <-ctx.Done():
		return ctx.Err()
	}

	r.controlMu.Lock()
	r.mu.Lock()
	if !r.state.Terminal() && r.handle != nil {
		handle = r.handle
		r.mu.Unlock()
		kill := handle.Signal("KILL")
		r.mu.Lock()
		r.signal = kill
		r.notifyLocked()
	}
	r.mu.Unlock()
	r.controlMu.Unlock()
	_, err := r.WaitTerminal(ctx)
	return err
}
