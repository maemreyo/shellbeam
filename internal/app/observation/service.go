package observation

import (
	"context"
	"errors"
	"fmt"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/observation"
)

const (
	MaxInspectEvents = 256
	eventScanLimit   = 1024
)

type InspectRequest struct {
	Target           core.Target
	AfterEventCursor string
	MaxEvents        int
}

type InspectResult struct {
	Events          []core.Event    `json:"events,omitempty"`
	NextEventCursor string          `json:"next_event_cursor"`
	Continuity      core.Continuity `json:"continuity"`
	Snapshot        *core.Snapshot  `json:"snapshot,omitempty"`
	Truncated       bool            `json:"truncated"`
	CompactedBefore uint64          `json:"compacted_before,omitempty"`
}

type Service struct {
	store        JournalStore
	materializer MaterializerPort
	snapshots    SnapshotProvider
	codec        *CursorCodec
}

func NewService(store JournalStore, materializer MaterializerPort, snapshots SnapshotProvider, codec *CursorCodec) *Service {
	return &Service{store: store, materializer: materializer, snapshots: snapshots, codec: codec}
}

func (s *Service) Inspect(ctx context.Context, request InspectRequest) (InspectResult, error) {
	if err := request.Target.Validate(); err != nil || request.MaxEvents < 1 || request.MaxEvents > MaxInspectEvents {
		return InspectResult{}, failure.New(failure.InvalidInput, map[string]string{"field": "inspect.events"}, err)
	}
	materialized, err := s.materializer.Materialize(ctx)
	if err != nil {
		return InspectResult{}, failure.New(failure.EventContinuityUnavailable, map[string]string{"reason": "materialization"}, err)
	}
	if materialized.PreparedGapAt != 0 {
		return InspectResult{}, failure.New(failure.EventContinuityUnavailable, map[string]string{"reason": "prepared_gap"}, nil)
	}
	if materialized.State.MaterializedThroughSeq < materialized.HighWatermark {
		return InspectResult{}, failure.New(failure.EventContinuityUnavailable, map[string]string{"reason": "projection_gap"}, nil)
	}
	after, expired, err := s.decodeRequestCursor(request)
	if err != nil {
		return InspectResult{}, err
	}
	if expired || after < materialized.State.CompactedThroughSeq {
		return s.snapshotRecovery(ctx, request.Target, materialized.State)
	}
	if after > materialized.HighWatermark {
		return InspectResult{}, failure.New(failure.EventCursorInvalid, map[string]string{"reason": "future"}, nil)
	}
	return s.inspectDelta(ctx, request, after, materialized.State)
}

func (s *Service) decodeRequestCursor(request InspectRequest) (core.ChangeSeq, bool, error) {
	if request.AfterEventCursor == "" {
		return 0, false, nil
	}
	seq, err := s.codec.Decode(request.AfterEventCursor, request.Target)
	if errors.Is(err, failure.EventCursorExpired) {
		return 0, true, nil
	}
	return seq, false, err
}

func (s *Service) inspectDelta(ctx context.Context, request InspectRequest, after core.ChangeSeq, state core.ProjectionState) (InspectResult, error) {
	events, more, err := s.store.ListEvents(ctx, after, state.MaterializedThroughSeq, eventScanLimit)
	if err != nil {
		return InspectResult{}, failure.New(failure.EventContinuityUnavailable, map[string]string{"reason": "event_read"}, err)
	}
	cut := state.MaterializedThroughSeq
	truncated := more
	if more && len(events) > 0 {
		cut = events[len(events)-1].ChangeSeq
	}
	filtered := make([]core.Event, 0, min(request.MaxEvents, len(events)))
	for _, event := range events {
		if !eventMatchesTarget(event, request.Target) {
			continue
		}
		filtered = append(filtered, event)
		if len(filtered) == request.MaxEvents {
			cut = event.ChangeSeq
			truncated = cut < state.MaterializedThroughSeq
			break
		}
	}
	cursor, err := s.codec.Encode(request.Target, cut)
	if err != nil {
		return InspectResult{}, err
	}
	return InspectResult{Events: filtered, NextEventCursor: cursor, Continuity: core.ContinuityComplete, Truncated: truncated, CompactedBefore: uint64(state.CompactedThroughSeq)}, nil
}

func (s *Service) snapshotRecovery(ctx context.Context, target core.Target, state core.ProjectionState) (InspectResult, error) {
	if s.snapshots == nil {
		return InspectResult{}, failure.New(failure.EventContinuityUnavailable, map[string]string{"reason": "snapshot_unavailable"}, nil)
	}
	snapshot, err := s.snapshots.CaptureSnapshot(ctx, target)
	if err != nil || snapshot.Validate() != nil || snapshot.Target != target {
		return InspectResult{}, failure.New(failure.EventContinuityUnavailable, map[string]string{"reason": "snapshot_unavailable"}, err)
	}
	high, err := s.store.ObservationHighWatermark(ctx)
	if err != nil || snapshot.CapturedThroughSeq > high || snapshot.CapturedThroughSeq < state.CompactedThroughSeq {
		return InspectResult{}, failure.New(failure.EventContinuityUnavailable, map[string]string{"reason": "snapshot_cut"}, err)
	}
	cursor, err := s.codec.Encode(target, snapshot.CapturedThroughSeq)
	if err != nil {
		return InspectResult{}, err
	}
	return InspectResult{NextEventCursor: cursor, Continuity: core.ContinuitySnapshotRequired, Snapshot: &snapshot, CompactedBefore: uint64(state.CompactedThroughSeq)}, nil
}

func eventMatchesTarget(event core.Event, target core.Target) bool {
	switch target.Kind {
	case core.TargetOperation:
		return event.Correlation.OperationID == target.OperationID
	case core.TargetSession:
		return event.Correlation.SessionID == target.SessionID
	case core.TargetActivity:
		return event.Correlation.ActivityID == target.ActivityID
	case core.TargetWorkspace:
		return event.Correlation.WorkspaceID == target.WorkspaceID
	case core.TargetRepository:
		return event.Correlation.RepositoryID == target.RepositoryID
	default:
		return false
	}
}

func (r InspectResult) Validate() error {
	if r.Continuity != core.ContinuityComplete && r.Continuity != core.ContinuitySnapshotRequired && r.Continuity != core.ContinuityUnavailable {
		return fmt.Errorf("invalid observation continuity")
	}
	return nil
}
