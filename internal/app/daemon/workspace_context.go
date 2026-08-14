package daemon

import (
	"context"

	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

const (
	maxContextWorkspaces   = 64
	maxContextFingerprints = 256
)

func (s *Service) enrichWorkspaceContext(view *View, snapshot *workspace.FastSnapshot, hint *workspace.Hint) {
	if view == nil || snapshot == nil {
		return
	}
	s.contextMu.Lock()
	defer s.contextMu.Unlock()

	current := *snapshot
	if current.WorkspaceID != "" {
		if previous, ok := s.contextLast[current.WorkspaceID]; ok {
			for _, event := range workspace.ContextEvents(previous, current) {
				key := "event:" + event.TransitionFingerprint
				if s.markContextFingerprintLocked(key) {
					view.ContextEvents = append(view.ContextEvents, event)
				}
			}
		}
		s.rememberWorkspaceLocked(current)
	}
	for _, advisory := range workspace.EvaluateHint(current, hint) {
		key := "advisory:" + advisory.CauseFingerprint
		if s.markContextFingerprintLocked(key) {
			view.Advisories = append(view.Advisories, advisory)
		}
	}
}

func (s *Service) enrichReplayWorkspaceContext(ctx context.Context, view *View, storedCWD string, hint *workspace.Hint) {
	if view == nil || hint == nil || s.observer == nil || storedCWD == "" {
		return
	}
	snapshot := s.observer.ObserveCached(ctx, storedCWD)
	if snapshot.Quality == workspace.QualityUnavailable {
		return
	}
	s.enrichWorkspaceContext(view, &snapshot, hint)
}

func (s *Service) rememberWorkspaceLocked(snapshot workspace.FastSnapshot) {
	id := snapshot.WorkspaceID
	if _, exists := s.contextLast[id]; !exists {
		if len(s.contextLastOrder) >= maxContextWorkspaces {
			oldest := s.contextLastOrder[0]
			s.contextLastOrder = s.contextLastOrder[1:]
			delete(s.contextLast, oldest)
		}
		s.contextLastOrder = append(s.contextLastOrder, id)
	}
	s.contextLast[id] = snapshot
}

func (s *Service) markContextFingerprintLocked(key string) bool {
	if key == "event:" || key == "advisory:" {
		return false
	}
	if _, exists := s.contextSeen[key]; exists {
		return false
	}
	if len(s.contextSeenOrder) >= maxContextFingerprints {
		oldest := s.contextSeenOrder[0]
		s.contextSeenOrder = s.contextSeenOrder[1:]
		delete(s.contextSeen, oldest)
	}
	s.contextSeen[key] = struct{}{}
	s.contextSeenOrder = append(s.contextSeenOrder, key)
	return true
}
