package daemon

import (
	"context"
	"fmt"
	persistentapp "github.com/maemreyo/shellbeam/internal/app/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
	"time"
)

func (s *Service) Poll(ctx context.Context, req PollRequest) (View, error) {
	res := operation.Reservation{SessionID: operation.SessionID(req.SessionID)}
	return s.waitView(ctx, res, req.SessionID, req.Cursor, req.YieldMS, req.MaxOutputBytes)
}
func (s *Service) waitView(ctx context.Context, res operation.Reservation, sid string, cursor int64, yieldMS int64, max int) (View, error) {
	if max <= 0 {
		max = 20000
	}
	if l := s.get(sid); l != nil && yieldMS > 0 {
		l.mu.Lock()
		ch := l.changed
		terminal := l.state.Terminal()
		l.mu.Unlock()
		if !terminal {
			timer := time.NewTimer(time.Duration(yieldMS) * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return View{}, ctx.Err()
			case <-ch:
				timer.Stop()
			case <-timer.C:
			}
		}
	}
	b, next, err := s.store.ReadOutput(ctx, operation.SessionID(sid), cursor, max+4)
	if err != nil {
		return View{}, failure.Normalize(err)
	}
	text, consumed, truncated := receipt.VisibleOutput(b, max)
	next = cursor + int64(consumed)
	v := View{OperationID: string(res.OperationID), ActivityID: res.ActivityID, WorkspaceID: res.WorkspaceID, SessionID: sid, Output: text, Cursor: cursor, NextCursor: next, Truncated: truncated}
	if l := s.get(sid); l != nil {
		l.mu.Lock()
		v.OperationID = l.operationID
		v.ActivityID = l.activityID
		v.WorkspaceID = l.reservation.WorkspaceID
		v.State = l.state
		v.Outcome = l.outcome
		if l.persistent {
			v.NextInputOffset = l.accepted
			v.EOFQueued = l.eof
		} else {
			v.NextInputOffset = l.input.NextOffset()
		}
		v.RawOutputBytes = l.outputBytes
		l.mu.Unlock()
	} else if snap, e := s.store.LoadSession(ctx, operation.SessionID(sid)); e != nil {
		// Neither live nor durable. Retention may have collected it, or it may
		// never have existed -- once the record is gone the daemon genuinely
		// cannot tell, and saying so is better than returning an empty view a
		// caller has to interpret as either.
		return View{}, failure.New(failure.SessionNotFound, map[string]string{
			"session_id": sid, "reason": "no durable session record",
		}, nil)
	} else {
		v.OperationID = snap.OperationID
		if reservation, loadErr := s.store.LoadOperation(ctx, operation.ID(snap.OperationID)); loadErr == nil {
			v.ActivityID = reservation.ActivityID
			v.WorkspaceID = reservation.WorkspaceID
		}
		v.State = snap.State
		v.Outcome = snap.Outcome
		v.RawOutputBytes = snap.OutputBytes
	}
	if v.State.Terminal() {
		if rec, e := s.store.LoadReceipt(ctx, operation.SessionID(sid)); e == nil {
			v.Receipt = &rec
			v.Failure = rec.Failure()
			v.RawOutputBytes = rec.OutputBytes
		}
	}
	if v.RawOutputBytes < next {
		v.RawOutputBytes = next
	}
	return v, nil
}
func (s *Service) Write(ctx context.Context, req WriteRequest) (View, error) {
	l := s.get(req.SessionID)
	if l == nil {
		return View{}, failure.New(failure.InvalidInput, map[string]string{"reason": "session_not_live"}, fmt.Errorf("session_not_live"))
	}
	l.mu.Lock()
	if l.persistent {
		state, handle := l.state, l.handle
		l.mu.Unlock()
		if state != session.Running {
			return View{}, failure.New(failure.InvalidInput, map[string]string{"reason": "session_not_writable"}, fmt.Errorf("session_not_writable"))
		}
		control, ok := handle.(persistentapp.ControlAttachment)
		if !ok {
			return View{}, failure.New(failure.SupervisorUnavailable, map[string]string{"session_id": req.SessionID, "reason": "ownership_proof"}, nil)
		}
		result, err := control.WriteInput(ctx, req.InputOffset, []byte(req.Chars), req.EOF)
		if err != nil {
			return View{}, failure.Normalize(err)
		}
		l.mu.Lock()
		l.accepted = result.NextOffset
		l.eof = l.eof || result.EOFDelivered
		l.notify()
		state = l.state
		l.mu.Unlock()
		return View{SessionID: req.SessionID, State: state, AcceptedInputBytes: result.AcceptedBytes, NextInputOffset: result.NextOffset, EOFQueued: result.EOFDelivered}, nil
	}
	defer l.mu.Unlock()
	if l.state != session.Running {
		return View{}, failure.New(failure.InvalidInput, map[string]string{"reason": "session_not_writable"}, fmt.Errorf("session_not_writable"))
	}
	var result session.InputResult
	var err error
	if req.EOF {
		result, err = l.input.AcceptEOF(req.InputOffset)
	} else {
		result, err = l.input.AcceptChars(req.InputOffset, []byte(req.Chars))
	}
	if err != nil {
		return View{}, failure.Normalize(err)
	}
	if !result.Duplicate {
		job := inputJob{data: []byte(req.Chars), eof: req.EOF}
		l.jobs <- job
		l.accepted += int64(result.AcceptedBytes)
		l.eof = l.eof || req.EOF
	}
	return View{SessionID: req.SessionID, State: l.state, AcceptedInputBytes: result.AcceptedBytes, NextInputOffset: result.NextOffset, EOFQueued: l.eof}, nil
}

func (s *Service) Kill(ctx context.Context, req KillRequest) (View, error) {
	l := s.get(req.SessionID)
	if l == nil {
		return View{}, failure.New(failure.InvalidInput, map[string]string{"reason": "session_not_live"}, fmt.Errorf("session_not_live"))
	}
	l.mu.Lock()
	if l.persistent {
		state, handle := l.state, l.handle
		l.mu.Unlock()
		control, ok := handle.(persistentapp.ControlAttachment)
		if !ok {
			return View{}, failure.New(failure.SupervisorUnavailable, map[string]string{"session_id": req.SessionID, "reason": "ownership_proof"}, nil)
		}
		attempt, err := control.SignalWithID(ctx, req.KillID, req.Signal)
		if err != nil {
			return View{}, failure.Normalize(err)
		}
		evidence := receipt.SignalEvidence{Requested: attempt.Signal, Attempted: attempt.Attempted, Succeeded: attempt.Succeeded}
		l.mu.Lock()
		l.signal = evidence
		l.notify()
		state = l.state
		l.mu.Unlock()
		return View{SessionID: req.SessionID, State: state, KillID: attempt.KillID, Signal: attempt.Signal, SignalAttempt: evidence}, nil
	}
	defer l.mu.Unlock()
	attempt, send, err := l.kills.Admit(req.KillID, req.Signal, l.state.Terminal())
	if err != nil {
		return View{}, failure.Normalize(err)
	}
	if send {
		l.terminalTarget = session.Killed
		attempt.Attempted = true
		e := l.handle.Signal(req.Signal)
		attempt.Succeeded = e.Succeeded
		l.signal = e
		l.kills.Record(attempt)
		l.notify()
	}
	evidence := receipt.SignalEvidence{Requested: attempt.Signal, Attempted: attempt.Attempted, Succeeded: attempt.Succeeded}
	return View{SessionID: req.SessionID, State: l.state, KillID: req.KillID, Signal: req.Signal, SignalAttempt: evidence}, nil
}
