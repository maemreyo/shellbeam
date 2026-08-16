package daemon

import (
	"context"
	"github.com/maemreyo/shellbeam/internal/core/session"
	"time"
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
		handle := l.handle
		l.mu.Unlock()
		if persistent {
			if handle != nil {
				_ = handle.Close()
			}
			s.endManagedShell(l)
			s.remove(l.sessionID)
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
