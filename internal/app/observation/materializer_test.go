package observation

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/observation"
)

func TestMaterializerAdvancesAbortedAndStopsAtPreparedGap(t *testing.T) {
	store := newMemoryJournalStore()
	store.obligations = []core.ObservationObligation{
		testObligation(1, core.EventOperationAdmitted, core.ObligationCommitted),
		testObligation(2, core.EventProcessStarted, core.ObligationAborted),
		testObligation(3, core.EventOutputAvailable, core.ObligationCommitted),
		testObligation(4, core.EventProcessTerminal, core.ObligationPrepared),
		testObligation(5, core.EventStructuredChanged, core.ObligationCommitted),
	}
	materializer := NewMaterializer(store)
	result, err := materializer.Materialize(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.State.MaterializedThroughSeq != 3 || result.HighWatermark != 5 || result.PreparedGapAt != 4 {
		t.Fatalf("result=%#v", result)
	}
	if got := store.eventSeqs(); len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Fatalf("event seqs=%v", got)
	}
	firstWrites := store.putCalls
	result, err = materializer.Materialize(context.Background())
	if err != nil || result.PreparedGapAt != 4 || store.putCalls != firstWrites {
		t.Fatalf("repeat result=%#v puts=%d/%d err=%v", result, store.putCalls, firstWrites, err)
	}
	store.obligations[3].State = core.ObligationCommitted
	result, err = materializer.Materialize(context.Background())
	if err != nil || result.State.MaterializedThroughSeq != 5 || result.PreparedGapAt != 0 {
		t.Fatalf("resolved result=%#v err=%v", result, err)
	}
	if got := store.eventSeqs(); len(got) != 4 || got[2] != 4 || got[3] != 5 {
		t.Fatalf("resolved event seqs=%v", got)
	}
}

func TestMaterializerEventProjectionIsDeterministicAcrossCheckpointRetry(t *testing.T) {
	store := newMemoryJournalStore()
	store.obligations = []core.ObservationObligation{testObligation(1, core.EventOperationAdmitted, core.ObligationCommitted)}
	store.failStateSaveOnce = true
	materializer := NewMaterializer(store)
	if _, err := materializer.Materialize(context.Background()); err == nil {
		t.Fatal("checkpoint failure not surfaced")
	}
	first := store.events[1]
	if first.EventID == "" {
		t.Fatal("event was not projected before checkpoint failure")
	}
	if _, err := materializer.Materialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	second := store.events[1]
	if first != second || store.eventConflicts != 0 {
		t.Fatalf("event changed first=%#v second=%#v conflicts=%d", first, second, store.eventConflicts)
	}
}

func testObligation(seq core.ChangeSeq, kind core.EventKind, state core.ObligationState) core.ObservationObligation {
	ob := core.ObservationObligation{SchemaVersion: 1, ChangeSeq: seq, Kind: kind, State: state, PreparedAt: time.Unix(int64(seq), 0).UTC(), Correlation: core.Correlation{OperationID: "op-1", SessionID: "s-1"}, SubjectRef: "subject", Summary: "summary"}
	if state == core.ObligationAborted {
		ob.AbortReason = "canonical_missing"
	}
	return ob
}

func TestMaterializerSerializesConcurrentProjection(t *testing.T) {
	store := newConcurrentProbeStore()
	store.obligations = []core.ObservationObligation{
		testObligation(1, core.EventOperationAdmitted, core.ObligationCommitted),
		testObligation(2, core.EventProcessStarted, core.ObligationCommitted),
	}
	materializer := NewMaterializer(store)
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := materializer.Materialize(context.Background())
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := atomic.LoadInt32(&store.maxProjectionReads); got != 1 {
		t.Fatalf("concurrent materializer projection reads=%d", got)
	}
}

type concurrentProbeStore struct {
	*memoryJournalStore
	projectionReads    int32
	maxProjectionReads int32
}

func newConcurrentProbeStore() *concurrentProbeStore {
	return &concurrentProbeStore{memoryJournalStore: newMemoryJournalStore()}
}

func (s *concurrentProbeStore) LoadEventProjectionState(ctx context.Context) (core.ProjectionState, error) {
	current := atomic.AddInt32(&s.projectionReads, 1)
	for {
		max := atomic.LoadInt32(&s.maxProjectionReads)
		if current <= max || atomic.CompareAndSwapInt32(&s.maxProjectionReads, max, current) {
			break
		}
	}
	time.Sleep(time.Millisecond)
	state, err := s.memoryJournalStore.LoadEventProjectionState(ctx)
	atomic.AddInt32(&s.projectionReads, -1)
	return state, err
}
