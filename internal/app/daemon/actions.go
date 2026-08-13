package daemon

import (
	"context"
	"fmt"
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
	v := View{OperationID: string(res.OperationID), SessionID: sid, Output: text, Cursor: cursor, NextCursor: next, Truncated: truncated}
	if l := s.get(sid); l != nil {
		l.mu.Lock()
		v.OperationID = l.operationID
		v.State = l.state
		v.Outcome = l.outcome
		v.NextInputOffset = l.input.NextOffset()
		v.RawOutputBytes = l.outputBytes
		l.mu.Unlock()
	} else if snap, e := s.store.LoadSession(ctx, operation.SessionID(sid)); e == nil {
		v.OperationID = snap.OperationID
		v.State = snap.State
		v.Outcome = snap.Outcome
		v.RawOutputBytes = snap.OutputBytes
	}
	if v.State.Terminal() {
		if rec, e := s.store.LoadReceipt(ctx, operation.SessionID(sid)); e == nil {
			v.Receipt = &rec
			v.RawOutputBytes = rec.OutputBytes
		}
	}
	if v.RawOutputBytes < next {
		v.RawOutputBytes = next
	}
	return v, nil
}
func (s *Service) Write(_ context.Context, req WriteRequest) (View, error) {
	l := s.get(req.SessionID)
	if l == nil {
		return View{}, failure.New(failure.InvalidInput, map[string]string{"reason": "session_not_live"}, fmt.Errorf("session_not_live"))
	}
	l.mu.Lock()
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
func (s *Service) Kill(_ context.Context, req KillRequest) (View, error) {
	l := s.get(req.SessionID)
	if l == nil {
		return View{}, failure.New(failure.InvalidInput, map[string]string{"reason": "session_not_live"}, fmt.Errorf("session_not_live"))
	}
	l.mu.Lock()
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
