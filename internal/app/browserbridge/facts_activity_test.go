package browserbridge

import (
	"context"
	"testing"
	"time"

	observationapp "github.com/maemreyo/shellbeam/internal/app/observation"
	activitycore "github.com/maemreyo/shellbeam/internal/core/activity"
	protocol "github.com/maemreyo/shellbeam/internal/core/browserbridge"
	observationcore "github.com/maemreyo/shellbeam/internal/core/observation"
	persistent "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/session"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type sessionsCall struct {
	activityID string
	maxRecords int
}

type eventsCall struct {
	target      observationcore.Target
	afterCursor string
	maxEvents   int
}

type fakeReader struct {
	stubDaemonReader
	activity      *activitycore.Activity
	activityFound bool
	sessions      *persistent.InspectPage
	sessionsFound bool
	events        *observationapp.InspectResult
	eventsFound   bool
	err           error
	activityIDs   []string
	sessionsCalls []sessionsCall
	eventsCalls   []eventsCall
}

func (f *fakeReader) Activity(_ context.Context, activityID string) (*activitycore.Activity, bool, error) {
	f.activityIDs = append(f.activityIDs, activityID)
	if f.err != nil {
		return nil, false, f.err
	}
	return f.activity, f.activityFound, nil
}

func (f *fakeReader) Sessions(_ context.Context, activityID string, maxRecords int) (*persistent.InspectPage, bool, error) {
	f.sessionsCalls = append(f.sessionsCalls, sessionsCall{activityID: activityID, maxRecords: maxRecords})
	if f.err != nil {
		return nil, false, f.err
	}
	return f.sessions, f.sessionsFound, nil
}

func (f *fakeReader) Events(_ context.Context, target observationcore.Target, afterCursor string, maxEvents int) (*observationapp.InspectResult, bool, error) {
	f.eventsCalls = append(f.eventsCalls, eventsCall{target: target, afterCursor: afterCursor, maxEvents: maxEvents})
	if f.err != nil {
		return nil, false, f.err
	}
	return f.events, f.eventsFound, nil
}

func TestActivityFactsComposesActivityAndSessionsAndDerivesEveryID(t *testing.T) {
	observed := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	reader := &fakeReader{
		activity: &activitycore.Activity{
			ID:                  "chatgpt-wt-01",
			WorkspaceIDs:        []workspace.WorkspaceID{"ws-1"},
			Operations:          []activitycore.OperationRef{{OperationID: "op-1", SessionID: "s-1", ObservedAt: observed}},
			CompactedOperations: 12,
		},
		activityFound: true,
		sessions: &persistent.InspectPage{Sessions: []persistent.Summary{
			{SessionID: "s-start", State: string(session.Starting), OwnershipStatus: persistent.OwnershipCurrent},
			{SessionID: "s-run", State: string(session.Running), OwnershipStatus: persistent.OwnershipCurrent},
			{SessionID: "s-final", State: string(session.Finalizing), OwnershipStatus: persistent.OwnershipReattached},
			{SessionID: "s-done", State: string(session.Completed), OwnershipStatus: persistent.OwnershipTerminal},
			{SessionID: "s-failed", State: string(session.Failed), OwnershipStatus: persistent.OwnershipTerminal},
			{SessionID: "s-timeout", State: string(session.TimedOut), OwnershipStatus: persistent.OwnershipTerminal},
			{SessionID: "s-killed", State: string(session.Killed), OwnershipStatus: persistent.OwnershipTerminal},
			{SessionID: "s-abandoned", State: string(session.Abandoned), OwnershipStatus: persistent.OwnershipTerminal},
			{SessionID: "s-lost", State: string(session.Running), OwnershipStatus: persistent.OwnershipLost},
		}},
		sessionsFound: true,
	}
	resp := NewPlanner(reader).ActivityFacts(context.Background(), "chatgpt-wt-01")

	if resp.Status != protocol.StatusOK {
		t.Fatalf("status = %q", resp.Status)
	}
	if resp.Activity == nil || !resp.Activity.Found {
		t.Fatal("activity not reported found")
	}
	if got := resp.Activity.Sessions; got.Provisioning != 1 || got.Live != 2 || got.Terminal != 5 || got.Lost != 1 {
		t.Fatalf("session counts = %+v", got)
	}
	if resp.Coverage.CompactedOperations != 12 || resp.Coverage.HistoricalOperations != "partial" {
		t.Fatalf("coverage = %+v", resp.Coverage)
	}
	if len(reader.activityIDs) != 1 || reader.activityIDs[0] != "chatgpt-wt-01" {
		t.Fatalf("activity selectors = %v", reader.activityIDs)
	}
	if len(reader.sessionsCalls) != 1 || reader.sessionsCalls[0].activityID != "chatgpt-wt-01" || reader.sessionsCalls[0].maxRecords != 64 {
		t.Fatalf("sessions calls = %+v", reader.sessionsCalls)
	}
}

func TestActivityFactsReportsFactsUnavailableWhenActivityMissing(t *testing.T) {
	reader := &fakeReader{}
	resp := NewPlanner(reader).ActivityFacts(context.Background(), "chatgpt-wt-01")
	if resp.Status != protocol.StatusFactsUnavailable {
		t.Fatalf("status = %q, want facts_unavailable", resp.Status)
	}
	if resp.Activity != nil && resp.Activity.Found {
		t.Fatal("missing activity reported as found")
	}
}

func TestActivityFactsReportsDaemonUnreachableOnTransportError(t *testing.T) {
	reader := &fakeReader{err: context.DeadlineExceeded}
	resp := NewPlanner(reader).ActivityFacts(context.Background(), "chatgpt-wt-01")
	if resp.Status != protocol.StatusDaemonUnreachable {
		t.Fatalf("status = %q, want daemon_unreachable", resp.Status)
	}
}

func TestActivityEventsUsesOneActivityScopedReadAndPassesCursorThrough(t *testing.T) {
	reader := &fakeReader{events: &observationapp.InspectResult{}, eventsFound: true}
	resp := NewPlanner(reader).ActivityEvents(context.Background(), "chatgpt-wt-01", "cursor-7")
	if resp.Status != protocol.StatusOK {
		t.Fatalf("status = %q", resp.Status)
	}
	if len(reader.eventsCalls) != 1 {
		t.Fatalf("expected exactly one read, got %d", len(reader.eventsCalls))
	}
	call := reader.eventsCalls[0]
	if call.target.Kind != observationcore.TargetActivity || call.target.ActivityID != "chatgpt-wt-01" {
		t.Fatalf("target = %+v", call.target)
	}
	if call.target.OperationID != "" || call.target.SessionID != "" {
		t.Fatal("event target leaked a non-activity selector")
	}
	if call.afterCursor != "cursor-7" {
		t.Fatalf("after cursor = %q", call.afterCursor)
	}
	if call.maxEvents != protocol.MaxActivityEvents {
		t.Fatalf("max events = %d", call.maxEvents)
	}
}

func TestActivityEventsRejectsAMalformedCursorWithoutCrashing(t *testing.T) {
	reader := &fakeReader{}
	resp := NewPlanner(reader).ActivityEvents(context.Background(), "chatgpt-wt-01", "not-a-valid-cursor")
	if resp.Status != protocol.StatusFactsUnavailable {
		t.Fatalf("status = %q, want facts_unavailable", resp.Status)
	}
}
