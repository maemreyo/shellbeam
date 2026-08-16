//go:build linux || darwin

package supervisor

import (
	"context"
	"crypto/hmac"
	"fmt"
	"sync"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

type RuntimeOptions struct {
	Layout           Layout
	Capability       Capability
	Owner            app.ProcessOwner
	Spec             operation.ExecutionSpec
	MaxOutputBytes   int64
	InputLimits      InputLimits
	MaxKillRecords   int
	TerminationGrace time.Duration
}

type RuntimeStatus struct {
	SessionID          string
	GenerationID       string
	State              session.State
	Outcome            session.Outcome
	Change             uint64
	PID                int
	OutputBytes        int64
	OutputAcknowledged int64
	Input              InputSnapshot
	Spawn              receipt.SpawnEvidence
	Exit               receipt.ExitEvidence
	Signal             receipt.SignalEvidence
	Terminal           bool
	FailureReason      string
}

type Runtime struct {
	mu               sync.Mutex
	controlMu        sync.Mutex
	layout           Layout
	capability       Capability
	metadata         Metadata
	owner            app.ProcessOwner
	spec             operation.ExecutionSpec
	spool            *Spool
	input            *InputLedger
	kills            *KillLedger
	terminationGrace time.Duration

	started       bool
	handle        app.ProcessHandle
	spawn         receipt.SpawnEvidence
	exit          receipt.ExitEvidence
	signal        receipt.SignalEvidence
	state         session.State
	outcome       session.Outcome
	target        session.State
	captureErr    error
	inputErr      error
	failureReason string
	timeout       TimeoutState
	change        uint64
	changed       chan struct{}
	done          chan struct{}
	doneOnce      sync.Once
	terminal      TerminalRecord
	terminalErr   error
}

func NewRuntime(options RuntimeOptions) (*Runtime, error) {
	if options.Owner == nil || options.Spec.TTY || options.MaxOutputBytes < 1 || options.MaxKillRecords < 1 || options.TerminationGrace < 0 {
		return nil, fmt.Errorf("invalid supervisor runtime options")
	}
	if err := validateLayout(options.Layout); err != nil {
		return nil, err
	}
	metadata, err := LoadMetadata(options.Layout)
	if err != nil {
		return nil, err
	}
	storedCapability, err := LoadCapability(options.Layout)
	if err != nil || !hmac.Equal(storedCapability.secret[:], options.Capability.secret[:]) {
		return nil, fmt.Errorf("supervisor capability mismatch")
	}
	spool, err := OpenSpool(options.Layout, options.MaxOutputBytes)
	if err != nil {
		return nil, err
	}
	input, err := OpenInputLedger(options.Layout, options.InputLimits)
	if err != nil {
		_ = spool.Close()
		return nil, err
	}
	kills, err := OpenKillLedger(options.Layout, options.MaxKillRecords)
	if err != nil {
		_ = spool.Close()
		return nil, err
	}
	return &Runtime{
		layout: options.Layout, capability: options.Capability, metadata: metadata, owner: options.Owner, spec: options.Spec,
		spool: spool, input: input, kills: kills, terminationGrace: options.TerminationGrace,
		state: session.Starting, changed: make(chan struct{}), done: make(chan struct{}),
	}, nil
}

func (r *Runtime) Start(ctx context.Context) error {
	r.controlMu.Lock()
	defer r.controlMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return fmt.Errorf("supervisor runtime already started")
	}
	r.started = true
	r.mu.Unlock()

	if r.spec.TimeoutMS > 0 {
		state, err := freezeTimeoutState(r.layout, r.spec.TimeoutMS, r.terminationGrace, time.Now().UTC())
		if err != nil {
			return err
		}
		r.mu.Lock()
		r.timeout = state
		r.mu.Unlock()
	}

	handle, spawn, err := r.owner.Start(context.Background(), r.spec, runtimeSink{runtime: r})
	r.mu.Lock()
	r.spawn = spawn
	if err == nil {
		r.handle = handle
		r.state = session.Running
		r.notifyLocked()
	}
	captureFailed := r.captureErr != nil
	r.mu.Unlock()
	if err != nil {
		r.freezeSpawnFailure()
		return err
	}
	if captureFailed {
		r.terminateForFailure("output_capture_failed")
	}
	go r.waitLoop()
	if r.spec.TimeoutMS > 0 {
		go r.timeoutLoop()
	}
	return nil
}

func (r *Runtime) Write(offset int64, chars []byte, eof bool) (InputAdmission, error) {
	r.controlMu.Lock()
	defer r.controlMu.Unlock()
	r.mu.Lock()
	if r.state != session.Running || r.handle == nil {
		r.mu.Unlock()
		return InputAdmission{}, fmt.Errorf("session_not_writable")
	}
	handle := r.handle
	r.mu.Unlock()

	var admission InputAdmission
	var err error
	if eof {
		if len(chars) != 0 {
			return InputAdmission{}, fmt.Errorf("input_conflict")
		}
		admission, err = r.input.AcceptEOF(offset)
	} else {
		admission, err = r.input.AcceptChars(offset, chars)
	}
	if err != nil || !admission.NeedsDelivery {
		return admission, err
	}
	if eof {
		err = handle.CloseStdin()
	} else {
		err = handle.Write(chars)
	}
	if err != nil {
		r.failInputDelivery()
		return admission, fmt.Errorf("input_delivery_failed")
	}
	if err := r.input.MarkDelivered(admission.Record); err != nil {
		r.failInputDelivery()
		return admission, err
	}
	r.mu.Lock()
	r.notifyLocked()
	r.mu.Unlock()
	return admission, nil
}

func (r *Runtime) Signal(killID, signalName string) (KillRecord, error) {
	r.controlMu.Lock()
	defer r.controlMu.Unlock()
	r.mu.Lock()
	terminal := r.state.Terminal()
	handle := r.handle
	r.mu.Unlock()
	attempt, send, err := r.kills.Admit(killID, signalName, terminal)
	if err != nil || !send {
		return attempt, err
	}
	if handle == nil {
		return attempt, fmt.Errorf("session_not_live")
	}
	r.mu.Lock()
	r.setTargetLocked(session.Killed)
	r.mu.Unlock()
	evidence := handle.Signal(signalName)
	attempt.Attempted = evidence.Attempted
	attempt.Succeeded = evidence.Succeeded
	if err := r.kills.Record(attempt); err != nil {
		r.terminateForFailure("kill_persistence_failed")
		return attempt, err
	}
	r.mu.Lock()
	r.signal = evidence
	r.notifyLocked()
	r.mu.Unlock()
	return attempt, nil
}

func (r *Runtime) Output(offset int64, maxBytes int) ([]byte, int64, error) {
	return r.spool.ReadRange(offset, maxBytes)
}

func (r *Runtime) AcknowledgeOutput(offset int64) error {
	return r.spool.Acknowledge(offset)
}

func (r *Runtime) Status() RuntimeStatus {
	r.mu.Lock()
	status := RuntimeStatus{
		SessionID: r.metadata.SessionID, GenerationID: r.metadata.GenerationID, State: r.state, Outcome: r.outcome,
		Change: r.change, Spawn: r.spawn, Exit: r.exit, Signal: r.signal, Terminal: r.state.Terminal(), FailureReason: r.failureReason,
	}
	if handle, ok := r.handle.(interface{ PID() int }); ok && !status.Terminal {
		status.PID = handle.PID()
	}
	r.mu.Unlock()
	status.OutputBytes = r.spool.Size()
	status.OutputAcknowledged = r.spool.Acknowledged()
	status.Input = r.input.Snapshot()
	return status
}

func (r *Runtime) WaitChange(ctx context.Context, after uint64) (RuntimeStatus, error) {
	for {
		r.mu.Lock()
		if r.change > after {
			r.mu.Unlock()
			return r.Status(), nil
		}
		changed := r.changed
		r.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return RuntimeStatus{}, ctx.Err()
		}
	}
}

func (r *Runtime) WaitTerminal(ctx context.Context) (TerminalRecord, error) {
	select {
	case <-r.done:
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.terminal, r.terminalErr
	case <-ctx.Done():
		return TerminalRecord{}, ctx.Err()
	}
}

func (r *Runtime) Close() error {
	r.mu.Lock()
	handle := r.handle
	r.mu.Unlock()
	if handle != nil {
		_ = handle.Close()
	}
	return r.spool.Close()
}

func (r *Runtime) waitLoop() {
	r.mu.Lock()
	handle := r.handle
	r.mu.Unlock()
	exit := handle.Wait(context.Background())
	r.controlMu.Lock()
	defer r.controlMu.Unlock()
	r.mu.Lock()
	r.exit = exit
	r.state = session.Finalizing
	r.notifyLocked()
	target, captureErr, inputErr, spawn, signalEvidence := r.target, r.captureErr, r.inputErr, r.spawn, r.signal
	r.mu.Unlock()

	input := r.input.Snapshot()
	state, outcome, reason := terminalOutcome(target, captureErr, inputErr, exit, input)
	record := TerminalRecord{
		SchemaVersion: TerminalRecordSchemaVersion, ProtocolVersion: ProtocolVersion,
		SessionID: r.metadata.SessionID, GenerationID: r.metadata.GenerationID, State: state, Outcome: outcome,
		Spawn: spawn, Exit: exit, Signal: signalEvidence, TimedOut: state == session.TimedOut,
		OutputBytes: r.spool.Size(), OutputComplete: captureErr == nil,
		InputAcceptedBytes: input.AcceptedBytes, InputDeliveredBytes: input.DeliveredBytes, StdinClosed: input.EOFDelivered,
		FailureReason: reason,
	}
	r.freezeTerminal(record)
}

func terminalOutcome(target session.State, captureErr, inputErr error, exit receipt.ExitEvidence, input InputSnapshot) (session.State, session.Outcome, string) {
	if target == session.TimedOut {
		return session.TimedOut, session.Timeout, ""
	}
	if target == session.Killed {
		reason := ""
		if captureErr != nil {
			reason = "output_capture_failed"
		} else if inputErr != nil {
			reason = "input_delivery_failed"
		}
		return session.Killed, session.KilledOutcome, reason
	}
	if captureErr != nil {
		return session.Killed, session.KilledOutcome, "output_capture_failed"
	}
	if inputErr != nil {
		return session.Killed, session.KilledOutcome, "input_delivery_failed"
	}
	if exit.Code != nil && *exit.Code == 0 && input.AcceptedBytes == input.DeliveredBytes {
		return session.Completed, session.Success, ""
	}
	return session.Failed, session.Failure, ""
}

func (r *Runtime) freezeSpawnFailure() {
	record := TerminalRecord{
		SchemaVersion: TerminalRecordSchemaVersion, ProtocolVersion: ProtocolVersion,
		SessionID: r.metadata.SessionID, GenerationID: r.metadata.GenerationID,
		State: session.Failed, Outcome: session.Failure, Spawn: r.spawn, OutputBytes: r.spool.Size(), OutputComplete: true, FailureReason: "spawn_failed",
	}
	r.freezeTerminal(record)
}

func (r *Runtime) freezeTerminal(record TerminalRecord) {
	sealed, err := SealTerminalRecord(r.capability, record)
	if err == nil {
		err = WriteTerminalRecord(r.layout, sealed)
	}
	r.mu.Lock()
	if err == nil {
		r.terminal = sealed
	}
	r.terminalErr = err
	r.state = record.State
	r.outcome = record.Outcome
	r.failureReason = record.FailureReason
	r.notifyLocked()
	r.doneOnce.Do(func() { close(r.done) })
	r.mu.Unlock()
}
