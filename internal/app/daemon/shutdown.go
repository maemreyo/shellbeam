package daemon

import (
	"context"
	"time"

	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func (s *Service) Shutdown(ctx context.Context) error {
	s.mu.RLock()
	live := make([]*liveSession, 0, len(s.live))
	for _, l := range s.live {
		l.mu.Lock()
		terminal := l.state.Terminal()
		l.mu.Unlock()
		if !terminal {
			live = append(live, l)
		}
	}
	s.mu.RUnlock()
	if len(live) == 0 {
		return nil
	}
	direct := live[:0]
	for _, l := range live {
		l.mu.Lock()
		persistent := l.persistent
		delegated := l.delegated
		handle := l.handle
		delegatedCancel, delegatedDone, delegatedRef := l.delegatedCancel, l.delegatedWaitDone, l.delegatedRef
		l.mu.Unlock()
		if delegated {
			if err := s.shutdownDelegatedSession(ctx, l, delegatedCancel, delegatedDone, delegatedRef); err != nil {
				return err
			}
			continue
		}
		if persistent {
			if err := s.shutdownPersistentSession(ctx, l, handle); err != nil {
				return err
			}
			continue
		}
		direct = append(direct, l)
	}
	live = direct
	if len(live) == 0 {
		return nil
	}
	for _, l := range live {
		l.mu.Lock()
		if l.state == session.Running {
			l.terminalTarget = session.Killed
			l.signal = l.handle.Signal("TERM")
			l.notify()
		}
		l.mu.Unlock()
	}
	finished, err := waitForSessions(ctx, live, s.options.TerminationGrace)
	if err != nil || finished {
		return err
	}
	for _, l := range live {
		l.mu.Lock()
		if !l.state.Terminal() {
			l.signal = l.handle.Signal("KILL")
			l.notify()
		}
		done := l.done
		l.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
		}
	}
	return nil
}

func waitForSessions(ctx context.Context, live []*liveSession, grace time.Duration) (bool, error) {
	deadline := time.Now().Add(grace)
	for _, l := range live {
		l.mu.Lock()
		done := l.done
		l.mu.Unlock()
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false, nil
		}
		timer := time.NewTimer(remaining)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return false, ctx.Err()
		case <-done:
			if !timer.Stop() {
				<-timer.C
			}
		case <-timer.C:
			return false, nil
		}
	}
	return true, nil
}

func (s *Service) shutdownPersistentSession(ctx context.Context, live *liveSession, handle ProcessHandle) error {
	live.mu.Lock()
	cancel, reconcileDone := live.persistentCancel, live.persistentReconcileDone
	live.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if reconcileDone != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-reconcileDone:
		}
	}
	if handle != nil {
		_ = handle.Close()
	}
	s.endManagedShell(live)
	s.remove(live.sessionID)
	return nil
}

func (s *Service) shutdownDelegatedSession(ctx context.Context, live *liveSession, cancel context.CancelFunc, done <-chan struct{}, ref delegated.ProviderRef) error {
	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
		}
	}
	if err := s.detachDelegatedRuntime(ctx, ref); err != nil {
		return err
	}
	s.endManagedShell(live)
	s.remove(live.sessionID)
	return nil
}
