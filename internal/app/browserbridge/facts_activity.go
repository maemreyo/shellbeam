package browserbridge

import (
	"context"

	ipc "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	activitycore "github.com/maemreyo/shellbeam/internal/core/activity"
	protocol "github.com/maemreyo/shellbeam/internal/core/browserbridge"
	observationcore "github.com/maemreyo/shellbeam/internal/core/observation"
	persistent "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func base(verb protocol.Verb, status protocol.Status) protocol.Response {
	return protocol.Response{ProtocolVersion: protocol.ProtocolVersion, SupportedVersions: protocol.SupportedVersions(), Verb: verb, Status: status}
}

func unreachable(verb protocol.Verb) protocol.Response {
	resp := base(verb, protocol.StatusDaemonUnreachable)
	resp.Reason = "daemon_unreachable"
	return resp
}

func unavailable(verb protocol.Verb, reason string) protocol.Response {
	resp := base(verb, protocol.StatusFactsUnavailable)
	resp.Reason = reason
	return resp
}

// ActivityFacts runs the activity_facts read plan: inspect.activity for the
// operation refs, workspace ids and compaction counter, then
// inspect.sessions scoped to the same activity, because an Activity record
// carries no session state of its own.
func (p *Planner) ActivityFacts(ctx context.Context, correlationID string) protocol.Response {
	act, resp, ok := p.activity(ctx, protocol.VerbActivityFacts, correlationID)
	if !ok {
		return resp
	}
	facts := protocol.ActivityFacts{Found: true, OperationsRetained: len(act.Operations)}
	for _, id := range act.WorkspaceIDs {
		facts.WorkspaceIDs = append(facts.WorkspaceIDs, string(id))
	}
	if n := len(act.Operations); n > 0 {
		at := act.Operations[n-1].ObservedAt
		facts.LatestOperationAt = &at
	}
	sessions, err := p.reader.Read(ctx, ipc.RequestV2{IPVersion: 2, Kind: "request", RequestID: "bb-sessions", Action: "inspect.sessions", ActivityID: correlationID, MaxRecords: 64})
	if err != nil {
		return unreachable(protocol.VerbActivityFacts)
	}
	if sessions.OK && sessions.Sessions != nil {
		for _, s := range sessions.Sessions.Sessions {
			switch persistent.Lifecycle(s.State) {
			case persistent.LifecycleProvisioning:
				facts.Sessions.Provisioning++
			case persistent.LifecycleLive:
				facts.Sessions.Live++
			case persistent.LifecycleTerminal:
				facts.Sessions.Terminal++
			case persistent.LifecycleLost:
				facts.Sessions.Lost++
			}
		}
		facts.SessionsTruncated = sessions.Sessions.Continuation != ""
	}
	out := base(protocol.VerbActivityFacts, protocol.StatusOK)
	out.Activity = &facts
	out.Coverage = coverageFor(act.CompactedOperations)
	return out
}

// ActivityEvents runs the activity_events read plan: one activity-scoped
// journal read with one cursor. The cursor is opaque to the host and is
// stored by the extension, because the host holds no state.
func (p *Planner) ActivityEvents(ctx context.Context, correlationID, cursor string) protocol.Response {
	req := ipc.RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "bb-events", Action: "inspect.events",
		Target:    &observationcore.Target{Kind: observationcore.TargetActivity, ActivityID: correlationID},
		MaxEvents: protocol.MaxActivityEvents,
	}
	if cursor != "" {
		req.AfterEventCursor = cursor
	}
	resp, err := p.reader.Read(ctx, req)
	if err != nil {
		return unreachable(protocol.VerbActivityEvents)
	}
	if !resp.OK || resp.Events == nil {
		return unavailable(protocol.VerbActivityEvents, "events_unavailable")
	}
	facts := protocol.EventFacts{Returned: len(resp.Events.Events), Cursor: resp.Events.NextEventCursor}
	seen := map[string]bool{}
	for _, event := range resp.Events.Events {
		kind := string(event.Kind)
		if !seen[kind] {
			seen[kind] = true
			facts.Kinds = append(facts.Kinds, kind)
		}
	}
	if n := len(resp.Events.Events); n > 0 {
		at := resp.Events.Events[n-1].RecordedAt
		facts.LatestAt = &at
	}
	out := base(protocol.VerbActivityEvents, protocol.StatusOK)
	out.Events = &facts
	out.Coverage.Truncated = resp.Events.Truncated
	if out.Coverage.Truncated {
		out.Coverage.TruncationReason = "more_events_available"
	}
	if resp.Events.CompactedBefore > 0 {
		out.Coverage.HistoricalOperations = "partial"
	}
	return out
}

func coverageFor(compacted int) protocol.Coverage {
	coverage := protocol.Coverage{CompactedOperations: compacted, HistoricalOperations: "complete"}
	if compacted > 0 {
		coverage.HistoricalOperations = "partial"
	}
	return coverage
}

func (p *Planner) activity(ctx context.Context, verb protocol.Verb, correlationID string) (activityRecord, protocol.Response, bool) {
	resp, err := p.reader.Read(ctx, ipc.RequestV2{IPVersion: 2, Kind: "request", RequestID: "bb-activity", Action: "inspect.activity", ActivityID: correlationID})
	if err != nil {
		return activityRecord{}, unreachable(verb), false
	}
	if !resp.OK || resp.Activity == nil {
		return activityRecord{}, unavailable(verb, "activity_not_found"), false
	}
	return activityRecord{Operations: resp.Activity.Operations, WorkspaceIDs: resp.Activity.WorkspaceIDs, CompactedOperations: resp.Activity.CompactedOperations}, protocol.Response{}, true
}

type activityRecord struct {
	Operations          []activitycore.OperationRef
	WorkspaceIDs        []workspace.WorkspaceID
	CompactedOperations int
}
