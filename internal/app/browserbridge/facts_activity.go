package browserbridge

import (
	"context"

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

// ActivityFacts composes the activity record with sessions scoped to the same
// activity. Every selector is derived from the correlation id or activity
// record; the typed port exposes no execution or caller-selected action field.
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
	sessions, found, err := p.reader.Sessions(ctx, correlationID, 64)
	if err != nil {
		return unreachable(protocol.VerbActivityFacts)
	}
	if found && sessions != nil {
		for _, s := range sessions.Sessions {
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
		facts.SessionsTruncated = sessions.Continuation != ""
	}
	out := base(protocol.VerbActivityFacts, protocol.StatusOK)
	out.Activity = &facts
	out.Coverage = coverageFor(act.CompactedOperations)
	return out
}

// ActivityEvents runs one activity-scoped journal read with one opaque cursor.
func (p *Planner) ActivityEvents(ctx context.Context, correlationID, cursor string) protocol.Response {
	target := observationcore.Target{Kind: observationcore.TargetActivity, ActivityID: correlationID}
	events, found, err := p.reader.Events(ctx, target, cursor, protocol.MaxActivityEvents)
	if err != nil {
		return unreachable(protocol.VerbActivityEvents)
	}
	if !found || events == nil {
		return unavailable(protocol.VerbActivityEvents, "events_unavailable")
	}
	facts := protocol.EventFacts{Returned: len(events.Events), Cursor: events.NextEventCursor}
	seen := map[string]bool{}
	for _, event := range events.Events {
		kind := string(event.Kind)
		if !seen[kind] {
			seen[kind] = true
			facts.Kinds = append(facts.Kinds, kind)
		}
	}
	if n := len(events.Events); n > 0 {
		at := events.Events[n-1].RecordedAt
		facts.LatestAt = &at
	}
	out := base(protocol.VerbActivityEvents, protocol.StatusOK)
	out.Events = &facts
	out.Coverage.Truncated = events.Truncated
	if out.Coverage.Truncated {
		out.Coverage.TruncationReason = "more_events_available"
	}
	if events.CompactedBefore > 0 {
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
	activity, found, err := p.reader.Activity(ctx, correlationID)
	if err != nil {
		return activityRecord{}, unreachable(verb), false
	}
	if !found || activity == nil {
		return activityRecord{}, unavailable(verb, "activity_not_found"), false
	}
	return activityRecord{
		Operations: activity.Operations, WorkspaceIDs: activity.WorkspaceIDs,
		CompactedOperations: activity.CompactedOperations,
	}, protocol.Response{}, true
}

type activityRecord struct {
	Operations          []activitycore.OperationRef
	WorkspaceIDs        []workspace.WorkspaceID
	CompactedOperations int
}
