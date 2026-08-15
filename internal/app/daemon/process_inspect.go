package daemon

import (
	"context"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	processcore "github.com/maemreyo/shellbeam/internal/core/process"
)

func (s *Service) ResolveProcessSession(ctx context.Context, sessionID string) (processcore.SessionResolution, error) {
	result := processcore.SessionResolution{SessionID: sessionID}
	if live := s.get(sessionID); live != nil {
		live.mu.Lock()
		defer live.mu.Unlock()
		result.Known = true
		result.State = string(live.state)
		if !live.state.Terminal() {
			if handle, ok := live.handle.(pidHandle); ok {
				if pid := handle.PID(); pid > 0 {
					result.Current = true
					result.PID = pid
				}
			}
		}
		return result, nil
	}
	if s.store == nil {
		return result, nil
	}
	snapshot, err := s.store.LoadSession(ctx, operation.SessionID(sessionID))
	if err != nil {
		return result, nil
	}
	result.Known = true
	result.State = string(snapshot.State)
	return result, nil
}
