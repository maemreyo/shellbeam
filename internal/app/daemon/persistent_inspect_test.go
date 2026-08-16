package daemon

import (
	"context"
	"testing"

	persistent "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

type sessionInspectStore struct {
	Store
	page  persistent.InspectPage
	calls int
}

func (s *sessionInspectStore) ListSessionSummaries(context.Context, persistent.InspectRequest) (persistent.InspectPage, error) {
	s.calls++
	return s.page, nil
}

func TestInspectSessionsOverlaysOnlyEstablishedLiveAttachmentCache(t *testing.T) {
	store := &sessionInspectStore{page: persistent.InspectPage{Sessions: []persistent.Summary{
		{SessionID: "persistent-inspect-session", OperationID: "persistent-inspect-op", Persistent: true, SessionName: "dev-server", State: string(session.Running), OwnershipStatus: persistent.OwnershipLost},
		{SessionID: "direct-inspect-session", OperationID: "direct-inspect-op", State: string(session.Running), OwnershipStatus: persistent.OwnershipLost},
	}}}
	svc := NewService(store, nil, Options{})
	svc.put(&liveSession{sessionID: "persistent-inspect-session", operationID: "persistent-inspect-op", persistent: true, persistentReattached: true, state: session.Running, outputBytes: 12, accepted: 7, delivered: 5})
	svc.put(&liveSession{sessionID: "direct-inspect-session", operationID: "direct-inspect-op", state: session.Running, outputBytes: 4, accepted: 3, delivered: 3})

	page, err := svc.InspectSessions(context.Background(), persistent.InspectRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if store.calls != 1 || len(page.Sessions) != 2 {
		t.Fatalf("calls=%d page=%#v", store.calls, page)
	}
	if got := page.Sessions[0]; got.OwnershipStatus != persistent.OwnershipReattached || got.OutputBytes != 12 || got.InputAcceptedBytes != 7 || got.InputDeliveredBytes != 5 {
		t.Fatalf("persistent overlay=%#v", got)
	}
	if got := page.Sessions[1]; got.OwnershipStatus != persistent.OwnershipCurrent || got.SessionName != "" || got.Persistent || got.OutputBytes != 4 {
		t.Fatalf("direct overlay=%#v", got)
	}
}
