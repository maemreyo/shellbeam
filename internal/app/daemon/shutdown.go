package daemon

import (
	"context"
	"time"

	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func (s *Service) Shutdown(ctx context.Context) error {
	// Reconcilers are quiesced before the non-terminal collection below, because
	// a persistent reconciler deliberately outlives its session's terminal
	// transition and the collection cannot see it.
	if err := s.quiescePersistentReconcilers(ctx); err != nil {
		return err
	}
	live := s.nonTerminalSessions()
	if len(live) == 0 {
		return nil
	}
	direct := live[:0]
	for _, l := range live {
		l.mu.Lock()
		persistent := l.persistent
		delegatedMode := l.delegated
		handle := l.handle
		delegatedCancel, delegatedDone, delegatedRef := l.delegatedCancel, l.delegatedWaitDone, l.delegatedRef
		l.mu.Unlock()
		if delegatedMode {
			if err := s.shutdownDelegatedSession(ctx, l, delegatedCancel, delegatedDone, delegatedRef); err != nil {
				return err
			}
			continue
		}
		if persistent {
			// The reconciler is already stopped: quiescePersistentReconcilers
			// cancelled and awaited it above, for terminal and non-terminal
			// sessions alike.
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

func (s *Service) nonTerminalSessions() []*liveSession {
	s.mu.RLock()
	defer s.mu.RUnlock()
	live := make([]*liveSession, 0, len(s.live))
	for _, l := range s.live {
		l.mu.Lock()
		terminal := l.state.Terminal()
		l.mu.Unlock()
		if !terminal {
			live = append(live, l)
		}
	}
	return live
}

// quiescePersistentReconcilers cancels every persistent session's reconciler and
// waits for each to exit.
//
// It deliberately ignores session state. A reconciler is the lifecycle owner of
// its session and keeps retrying post-terminal convergence -- a binding update
// -- after the session itself has gone terminal, so a terminal session can still
// hold a live writer. Its context is rooted at context.Background(), so
// live.persistentCancel is the only thing that can stop it.
func (s *Service) quiescePersistentReconcilers(ctx context.Context) error {
	s.mu.RLock()
	cancels := make([]context.CancelFunc, 0, len(s.live))
	waits := make([]chan struct{}, 0, len(s.live))
	for _, l := range s.live {
		l.mu.Lock()
		persistent, cancel, done := l.persistent, l.persistentCancel, l.persistentReconcileDone
		l.mu.Unlock()
		if !persistent {
			continue
		}
		if cancel != nil {
			cancels = append(cancels, cancel)
		}
		if done != nil {
			waits = append(waits, done)
		}
	}
	s.mu.RUnlock()

	// Every reconciler is cancelled before any of them is awaited, so the cost is
	// the slowest reconciler rather than the sum of all of them. Waiting happens
	// without s.mu held, because a reconciler may need it to finish its write.
	for _, cancel := range cancels {
		cancel()
	}
	for _, done := range waits {
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
