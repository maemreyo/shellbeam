package browserbridge

import (
	"context"
	"testing"
	"time"

	ipc "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	observationapp "github.com/maemreyo/shellbeam/internal/app/observation"
	activitycore "github.com/maemreyo/shellbeam/internal/core/activity"
	protocol "github.com/maemreyo/shellbeam/internal/core/browserbridge"
	observationcore "github.com/maemreyo/shellbeam/internal/core/observation"
	persistent "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type fakeReader struct {
	seen     []ipc.RequestV2
	byAction map[string]ipc.ResponseV2
	err      error
}

func (f *fakeReader) Read(_ context.Context, req ipc.RequestV2) (ipc.ResponseV2, error) {
	f.seen = append(f.seen, req)
	if f.err != nil {
		return ipc.ResponseV2{}, f.err
	}
	return f.byAction[req.Action], nil
}

func TestActivityFactsComposesActivityAndSessionsAndDerivesEveryID(t *testing.T) {
	observed := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	reader := &fakeReader{byAction: map[string]ipc.ResponseV2{
		"inspect.activity": {OK: true, Activity: &activitycore.Activity{
			ID:                  "chatgpt-wt-01",
			WorkspaceIDs:        []workspace.WorkspaceID{"ws-1"},
			Operations:          []activitycore.OperationRef{{OperationID: "op-1", SessionID: "s-1", ObservedAt: observed}},
			CompactedOperations: 12,
		}},
		"inspect.sessions": {OK: true, Sessions: &persistent.InspectPage{Sessions: []persistent.Summary{
			{SessionID: "s-1", State: string(persistent.LifecycleLive)},
			{SessionID: "s-2", State: string(persistent.LifecycleTerminal)},
		}}},
	}}
	resp := NewPlanner(reader).ActivityFacts(context.Background(), "chatgpt-wt-01")

	if resp.Status != protocol.StatusOK {
		t.Fatalf("status = %q", resp.Status)
	}
	if resp.Activity == nil || !resp.Activity.Found {
		t.Fatal("activity not reported found")
	}
	if resp.Activity.Sessions.Live != 1 || resp.Activity.Sessions.Terminal != 1 {
		t.Fatalf("session counts = %+v", resp.Activity.Sessions)
	}
	if resp.Coverage.CompactedOperations != 12 || resp.Coverage.HistoricalOperations != "partial" {
		t.Fatalf("coverage = %+v", resp.Coverage)
	}
	if len(reader.seen) != 2 {
		t.Fatalf("expected two reads, got %d", len(reader.seen))
	}
	for _, req := range reader.seen {
		if req.Command != "" || len(req.Argv) != 0 || req.CWD != "" || req.SessionID != "" || req.OperationID != "" {
			t.Fatalf("read plan leaked an execution or caller-named selector: %+v", req)
		}
		if req.ActivityID != "chatgpt-wt-01" {
			t.Fatalf("read not scoped to the correlation id: %+v", req)
		}
	}
}

func TestActivityFactsReportsFactsUnavailableWhenActivityMissing(t *testing.T) {
	reader := &fakeReader{byAction: map[string]ipc.ResponseV2{
		"inspect.activity": {OK: false, Error: &ipc.Error{Code: "activity_not_found"}},
	}}
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
	reader := &fakeReader{byAction: map[string]ipc.ResponseV2{
		"inspect.events": {OK: true, Events: &observationapp.InspectResult{}},
	}}
	resp := NewPlanner(reader).ActivityEvents(context.Background(), "chatgpt-wt-01", "cursor-7")
	if resp.Status != protocol.StatusOK {
		t.Fatalf("status = %q", resp.Status)
	}
	if len(reader.seen) != 1 {
		t.Fatalf("expected exactly one read, got %d", len(reader.seen))
	}
	req := reader.seen[0]
	if req.Action != "inspect.events" {
		t.Fatalf("action = %q", req.Action)
	}
	if req.Target == nil || req.Target.Kind != observationcore.TargetActivity || req.Target.ActivityID != "chatgpt-wt-01" {
		t.Fatalf("target = %+v", req.Target)
	}
	if req.Target.OperationID != "" || req.Target.SessionID != "" {
		t.Fatal("event target leaked a non-activity selector")
	}
	if req.AfterEventCursor != "cursor-7" {
		t.Fatalf("after_event_cursor = %q", req.AfterEventCursor)
	}
	if req.MaxEvents != protocol.MaxActivityEvents {
		t.Fatalf("max_events = %d", req.MaxEvents)
	}
}

func TestActivityEventsRejectsAMalformedCursorWithoutCrashing(t *testing.T) {
	reader := &fakeReader{byAction: map[string]ipc.ResponseV2{
		"inspect.events": {OK: false, Error: &ipc.Error{Code: "invalid_input"}},
	}}
	resp := NewPlanner(reader).ActivityEvents(context.Background(), "chatgpt-wt-01", "not-a-valid-cursor")
	if resp.Status != protocol.StatusFactsUnavailable {
		t.Fatalf("status = %q, want facts_unavailable", resp.Status)
	}
}
