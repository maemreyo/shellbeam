package observation

import (
	"context"
	"errors"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/observation"
)

func TestInspectEventsFiltersWithoutRenumberingUnderlyingCursor(t *testing.T) {
	store := newMemoryJournalStore()
	store.state = core.ProjectionState{SchemaVersion: 1, MaterializedThroughSeq: 5}
	store.high = 5
	store.events = map[core.ChangeSeq]core.Event{
		1: testProjectedEvent(1, "other", "s-other"),
		3: testProjectedEvent(3, "op-1", "s-1"),
		5: testProjectedEvent(5, "other", "s-other"),
	}
	service := newTestService(t, store, nil)
	result, err := service.Inspect(context.Background(), InspectRequest{Target: core.Target{Kind: core.TargetOperation, OperationID: "op-1"}, MaxEvents: 10})
	if err != nil {
		t.Fatal(err)
	}
	if result.Continuity != core.ContinuityComplete || len(result.Events) != 1 || result.Events[0].ChangeSeq != 3 || result.Truncated {
		t.Fatalf("result=%#v", result)
	}
	seq, err := service.codec.Decode(result.NextEventCursor, core.Target{Kind: core.TargetOperation, OperationID: "op-1"})
	if err != nil || seq != 5 {
		t.Fatalf("cursor seq=%d err=%v", seq, err)
	}
}

func TestInspectEventsPaginatesAtUnderlyingMatchedSequence(t *testing.T) {
	store := newMemoryJournalStore()
	store.state = core.ProjectionState{SchemaVersion: 1, MaterializedThroughSeq: 5}
	store.high = 5
	store.events = map[core.ChangeSeq]core.Event{
		1: testProjectedEvent(1, "other", "s-other"),
		2: testProjectedEvent(2, "op-1", "s-1"),
		4: testProjectedEvent(4, "op-1", "s-1"),
	}
	service := newTestService(t, store, nil)
	target := core.Target{Kind: core.TargetOperation, OperationID: "op-1"}
	first, err := service.Inspect(context.Background(), InspectRequest{Target: target, MaxEvents: 1})
	if err != nil || len(first.Events) != 1 || first.Events[0].ChangeSeq != 2 || !first.Truncated {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	cut, err := service.codec.Decode(first.NextEventCursor, target)
	if err != nil || cut != 2 {
		t.Fatalf("first cut=%d err=%v", cut, err)
	}
	second, err := service.Inspect(context.Background(), InspectRequest{Target: target, AfterEventCursor: first.NextEventCursor, MaxEvents: 10})
	if err != nil || len(second.Events) != 1 || second.Events[0].ChangeSeq != 4 || second.Truncated {
		t.Fatalf("second=%#v err=%v", second, err)
	}
	cut, err = service.codec.Decode(second.NextEventCursor, target)
	if err != nil || cut != 5 {
		t.Fatalf("second cut=%d err=%v", cut, err)
	}
}

func TestInspectExpiredCursorReturnsSnapshotAndResumeCut(t *testing.T) {
	store := newMemoryJournalStore()
	store.state = core.ProjectionState{SchemaVersion: 1, MaterializedThroughSeq: 5, CompactedThroughSeq: 3}
	store.high = 5
	provider := &fakeSnapshotProvider{snapshot: core.Snapshot{SchemaVersion: 1, Target: core.Target{Kind: core.TargetOperation, OperationID: "op-1"}, CapturedThroughSeq: 5, Facts: []core.SnapshotFact{{Code: "operation_state", Value: "completed"}}}}
	service := newTestService(t, store, provider)
	target := provider.snapshot.Target
	old, err := service.codec.Encode(target, 2)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Inspect(context.Background(), InspectRequest{Target: target, AfterEventCursor: old, MaxEvents: 10})
	if err != nil || result.Continuity != core.ContinuitySnapshotRequired || result.Snapshot == nil || result.CompactedBefore != 3 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	cut, err := service.codec.Decode(result.NextEventCursor, target)
	if err != nil || cut != 5 {
		t.Fatalf("resume cut=%d err=%v", cut, err)
	}
	store.high = 6
	store.state.MaterializedThroughSeq = 6
	store.events[6] = testProjectedEvent(6, "op-1", "s-1")
	next, err := service.Inspect(context.Background(), InspectRequest{Target: target, AfterEventCursor: result.NextEventCursor, MaxEvents: 10})
	if err != nil || next.Continuity != core.ContinuityComplete || len(next.Events) != 1 || next.Events[0].ChangeSeq != 6 {
		t.Fatalf("next=%#v err=%v", next, err)
	}
}

func TestInspectPreparedGapNeverSnapshotSkipsUnresolvedTransition(t *testing.T) {
	store := newMemoryJournalStore()
	store.state = core.ProjectionState{SchemaVersion: 1, MaterializedThroughSeq: 2}
	store.high = 4
	materializer := &fixedMaterializer{result: MaterializeResult{State: store.state, HighWatermark: 4, PreparedGapAt: 3}}
	service := newTestServiceWithMaterializer(t, store, nil, materializer)
	_, err := service.Inspect(context.Background(), InspectRequest{Target: core.Target{Kind: core.TargetOperation, OperationID: "op-1"}, MaxEvents: 10})
	if !errors.Is(err, failure.EventContinuityUnavailable) {
		t.Fatalf("gap error=%v", err)
	}
}
