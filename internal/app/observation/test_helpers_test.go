package observation

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/observation"
)

type memoryJournalStore struct {
	obligations       []core.ObservationObligation
	events            map[core.ChangeSeq]core.Event
	state             core.ProjectionState
	high              core.ChangeSeq
	putCalls          int
	eventConflicts    int
	failStateSaveOnce bool
}

func newMemoryJournalStore() *memoryJournalStore {
	return &memoryJournalStore{events: map[core.ChangeSeq]core.Event{}, state: core.ProjectionState{SchemaVersion: 1}, high: 0}
}
func (s *memoryJournalStore) ObservationHighWatermark(context.Context) (core.ChangeSeq, error) {
	if s.high != 0 {
		return s.high, nil
	}
	if len(s.obligations) == 0 {
		return 0, nil
	}
	return s.obligations[len(s.obligations)-1].ChangeSeq, nil
}
func (s *memoryJournalStore) ListObservationObligations(_ context.Context, after core.ChangeSeq, limit int) ([]core.ObservationObligation, error) {
	var out []core.ObservationObligation
	for _, ob := range s.obligations {
		if ob.ChangeSeq > after {
			out = append(out, ob)
		}
		if len(out) == limit {
			break
		}
	}
	return out, nil
}
func (s *memoryJournalStore) EventCursorKey(context.Context) (core.CursorKeyMaterial, error) {
	return testCursorKey("0"), nil
}
func (s *memoryJournalStore) PutEvent(_ context.Context, event core.Event) error {
	s.putCalls++
	if existing, ok := s.events[event.ChangeSeq]; ok {
		if !reflect.DeepEqual(existing, event) {
			s.eventConflicts++
			return fmt.Errorf("event conflict")
		}
		return nil
	}
	s.events[event.ChangeSeq] = event
	return nil
}
func (s *memoryJournalStore) LoadEventProjectionState(context.Context) (core.ProjectionState, error) {
	return s.state, nil
}
func (s *memoryJournalStore) SaveEventProjectionState(_ context.Context, state core.ProjectionState) error {
	if s.failStateSaveOnce {
		s.failStateSaveOnce = false
		return fmt.Errorf("checkpoint failed")
	}
	s.state = state
	return nil
}
func (s *memoryJournalStore) ListEvents(_ context.Context, after, through core.ChangeSeq, limit int) ([]core.Event, bool, error) {
	var seqs []int
	for seq := range s.events {
		if seq > after && seq <= through {
			seqs = append(seqs, int(seq))
		}
	}
	sort.Ints(seqs)
	more := len(seqs) > limit
	if more {
		seqs = seqs[:limit]
	}
	out := make([]core.Event, 0, len(seqs))
	for _, seq := range seqs {
		out = append(out, s.events[core.ChangeSeq(seq)])
	}
	return out, more, nil
}
func (s *memoryJournalStore) eventSeqs() []int {
	var out []int
	for seq := range s.events {
		out = append(out, int(seq))
	}
	sort.Ints(out)
	return out
}

type fakeSnapshotProvider struct {
	snapshot core.Snapshot
	err      error
}

func (p *fakeSnapshotProvider) CaptureSnapshot(context.Context, core.Target) (core.Snapshot, error) {
	return p.snapshot, p.err
}

type fixedMaterializer struct {
	result MaterializeResult
	err    error
}

func (m *fixedMaterializer) Materialize(context.Context) (MaterializeResult, error) {
	return m.result, m.err
}

func newTestService(t *testing.T, store *memoryJournalStore, provider SnapshotProvider) *Service {
	t.Helper()
	return newTestServiceWithMaterializer(t, store, provider, NewMaterializer(store))
}
func newTestServiceWithMaterializer(t *testing.T, store *memoryJournalStore, provider SnapshotProvider, materializer MaterializerPort) *Service {
	t.Helper()
	key, err := store.EventCursorKey(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	codec, err := NewCursorCodec(key)
	if err != nil {
		t.Fatal(err)
	}
	return NewService(store, materializer, provider, codec)
}

func testProjectedEvent(seq core.ChangeSeq, operationID, sessionID string) core.Event {
	return core.Event{SchemaVersion: 1, EventID: fmt.Sprintf("evt_%064x", seq), StateRootEpoch: testCursorKey("0").StateRootEpoch, ChangeSeq: seq, Kind: core.EventOutputAvailable, RecordedAt: time.Unix(int64(seq), 0).UTC(), Correlation: core.Correlation{OperationID: operationID, SessionID: sessionID}, SubjectRef: fmt.Sprintf("output:%s:0:%d", sessionID, seq), Summary: "output available"}
}
