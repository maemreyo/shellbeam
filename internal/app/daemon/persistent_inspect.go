package daemon

import (
	"context"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	persistent "github.com/maemreyo/shellbeam/internal/core/persistentsession"
)

type persistentSessionInspectStore interface {
	ListSessionSummaries(context.Context, persistent.InspectRequest) (persistent.InspectPage, error)
}

func (s *Service) InspectSessions(ctx context.Context, request persistent.InspectRequest) (persistent.InspectPage, error) {
	store, ok := s.store.(persistentSessionInspectStore)
	if !ok {
		return persistent.InspectPage{}, failure.New(failure.FeatureUnavailable, map[string]string{"feature": "inspect.sessions"}, nil)
	}
	page, err := store.ListSessionSummaries(ctx, request)
	if err != nil {
		return persistent.InspectPage{}, failure.Normalize(err)
	}
	for i := range page.Sessions {
		live := s.get(page.Sessions[i].SessionID)
		if live == nil {
			continue
		}
		live.mu.Lock()
		page.Sessions[i].State = string(live.state)
		page.Sessions[i].Outcome = string(live.outcome)
		page.Sessions[i].OutputBytes = live.outputBytes
		page.Sessions[i].InputAcceptedBytes = live.accepted
		page.Sessions[i].InputDeliveredBytes = live.delivered
		if live.state.Terminal() {
			page.Sessions[i].OwnershipStatus = persistent.OwnershipTerminal
		} else if live.persistent && live.persistentReattached {
			page.Sessions[i].OwnershipStatus = persistent.OwnershipReattached
		} else {
			page.Sessions[i].OwnershipStatus = persistent.OwnershipCurrent
		}
		live.mu.Unlock()
	}
	return page, nil
}
