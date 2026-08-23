package interactivehandoff

import (
	"context"
	"time"

	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
)

func (s *Service) Wait(ctx context.Context, req WaitRequest) (WaitResult, error) {
	yield := req.Yield
	if yield < 0 {
		yield = 0
	}
	if yield > MaxWait {
		yield = MaxWait
	}
	deadline := time.NewTimer(yield)
	defer deadline.Stop()
	for {
		changed := s.changeChannel()
		state, err := s.Inspect(ctx, req.HandoffID)
		if err != nil {
			return WaitResult{}, err
		}
		if handoffStable(state) || yield == 0 {
			return WaitResult{State: state}, nil
		}
		select {
		case <-ctx.Done():
			return WaitResult{}, ctx.Err()
		case <-changed:
			continue
		case <-deadline.C:
			state, err := s.Inspect(ctx, req.HandoffID)
			if err != nil {
				return WaitResult{}, err
			}
			return WaitResult{State: state, TimedOut: true}, nil
		}
	}
}

func handoffStable(state handoff.State) bool {
	switch handoff.ProjectStatus(state) {
	case handoff.StatusAgentOwned, handoff.StatusHumanOwned, handoff.StatusAborted, handoff.StatusReclaimBlocked:
		return true
	default:
		return false
	}
}
