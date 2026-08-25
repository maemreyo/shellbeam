package daemon

import (
	"context"
	"fmt"

	persistentapp "github.com/maemreyo/shellbeam/internal/app/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	persistent "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func (s *Service) replayPersistentKill(ctx context.Context, req KillRequest) (View, bool, error) {
	store, ok := s.store.(PersistentKillStore)
	if !ok {
		return View{}, false, nil
	}
	record, found, err := store.LookupPersistentKill(ctx, operation.SessionID(req.SessionID), req.KillID)
	if err != nil || !found {
		return View{}, false, err
	}
	if record.Signal != req.Signal {
		return View{}, true, failure.New(failure.OperationMetadataConflict, map[string]string{"field": "signal"}, fmt.Errorf("kill_conflict"))
	}
	if !record.Complete {
		return View{}, false, nil
	}
	state, outcome, terminalReceipt, ok := s.currentPersistentReplayState(ctx, req.SessionID)
	if !ok {
		return View{}, false, nil
	}
	view := persistentKillView(state, outcome, record)
	view.Receipt = terminalReceipt
	return view, true, nil
}

func (s *Service) killPersistent(ctx context.Context, live *liveSession, req KillRequest) (View, error) {
	store, ok := s.store.(PersistentKillStore)
	if !ok {
		return View{}, failure.New(failure.PersistenceUnavailable, nil, nil)
	}
	live.mu.Lock()
	state, outcome, handle := live.state, live.outcome, live.handle
	live.mu.Unlock()
	record, _, result := store.ReservePersistentKill(ctx, operation.SessionID(req.SessionID), req.KillID, req.Signal, state.Terminal())
	if result.Err != nil {
		return View{}, failure.Normalize(result.Err)
	}
	if record.Complete {
		return persistentKillView(state, outcome, record), nil
	}
	control, ok := handle.(persistentapp.ControlAttachment)
	if !ok {
		return View{}, failure.New(failure.SupervisorUnavailable, map[string]string{"session_id": req.SessionID, "reason": "ownership_proof"}, nil)
	}
	attempt, err := control.SignalWithID(ctx, req.KillID, req.Signal)
	if err != nil {
		return View{}, failure.Normalize(err)
	}
	record.Attempted, record.Succeeded, record.Needed, record.Complete = attempt.Attempted, attempt.Succeeded, attempt.Needed, true
	completed, stored := store.CompletePersistentKill(ctx, record)
	if stored.Err != nil {
		return View{}, failure.Normalize(stored.Err)
	}
	evidence := receipt.SignalEvidence{Requested: completed.Signal, Attempted: completed.Attempted, Succeeded: completed.Succeeded}
	live.mu.Lock()
	live.signal = evidence
	live.notify()
	state, outcome = live.state, live.outcome
	live.mu.Unlock()
	return persistentKillView(state, outcome, completed), nil
}

func (s *Service) currentPersistentReplayState(ctx context.Context, sessionID string) (session.State, session.Outcome, *receipt.Receipt, bool) {
	if live := s.get(sessionID); live != nil {
		live.mu.Lock()
		state, outcome := live.state, live.outcome
		live.mu.Unlock()
		if !state.Terminal() {
			return state, outcome, nil, true
		}
	}
	rec, err := s.store.LoadReceipt(ctx, operation.SessionID(sessionID))
	if err != nil {
		return "", "", nil, false
	}
	return rec.State, rec.Outcome, &rec, true
}

func persistentKillView(state session.State, outcome session.Outcome, record persistent.KillRecord) View {
	return View{
		SessionID: record.SessionID, State: state, Outcome: outcome, KillID: record.KillID, Signal: record.Signal,
		SignalAttempt: receipt.SignalEvidence{Requested: record.Signal, Attempted: record.Attempted, Succeeded: record.Succeeded},
	}
}
