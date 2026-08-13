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
	for _, l := range live {
		l.mu.Lock()
		if l.state == session.Running {
			l.terminalTarget = session.Killed
			l.signal = l.handle.Signal("TERM")
			l.notify()
		}
		l.mu.Unlock()
	}
	timer := time.NewTimer(s.options.TerminationGrace)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
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
