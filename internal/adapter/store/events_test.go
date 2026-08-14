package store

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/observation"
)

func TestEventProjectionStorePersistsIdempotentlyAndReopens(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openObservationRepository(t, root)
	key, err := r.EventCursorKey(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(key.Secret) != 32 || key.StateRootEpoch == "" || key.Generation == "" {
		t.Fatalf("cursor key=%#v", key)
	}
	event := testEvent(key.StateRootEpoch, 2, observation.EventProcessStarted, "session:s:started")
	if err := r.PutEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if err := r.PutEvent(context.Background(), event); err != nil {
		t.Fatalf("idempotent put: %v", err)
	}
	conflict := event
	conflict.Summary = "different"
	if err := r.PutEvent(context.Background(), conflict); err == nil {
		t.Fatal("conflicting event rewrite accepted")
	}
	state := observation.ProjectionState{SchemaVersion: 1, MaterializedThroughSeq: 2}
	if err := r.SaveEventProjectionState(context.Background(), state); err != nil {
		t.Fatal(err)
	}

	r = openObservationRepository(t, root)
	reopenedKey, err := r.EventCursorKey(context.Background())
	if err != nil || reopenedKey.StateRootEpoch != key.StateRootEpoch || reopenedKey.Generation != key.Generation || string(reopenedKey.Secret) != string(key.Secret) {
		t.Fatalf("reopened key=%#v err=%v", reopenedKey, err)
	}
	got, err := r.LoadEventProjectionState(context.Background())
	if err != nil || got.MaterializedThroughSeq != 2 {
		t.Fatalf("projection=%#v err=%v", got, err)
	}
	events, more, err := r.ListEvents(context.Background(), 0, 9, 10)
	if err != nil || more || len(events) != 1 || events[0].ChangeSeq != 2 {
		t.Fatalf("events=%#v more=%v err=%v", events, more, err)
	}
}

func TestEventProjectionRetentionOnlyDeletesEvents(t *testing.T) {
	r := openObservationRepository(t, filepath.Join(t.TempDir(), "state"))
	key, err := r.EventCursorKey(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for seq := 1; seq <= 5; seq++ {
		if _, result := r.PrepareObservation(context.Background(), observationRequest("subject:"+string(rune('a'+seq)))); result.Err != nil {
			t.Fatal(result.Err)
		}
		event := testEvent(key.StateRootEpoch, observation.ChangeSeq(seq), observation.EventOperationAdmitted, "operation:op-1")
		if err := r.PutEvent(context.Background(), event); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.SaveEventProjectionState(context.Background(), observation.ProjectionState{SchemaVersion: 1, MaterializedThroughSeq: 5}); err != nil {
		t.Fatal(err)
	}
	result, err := r.CompactEvents(context.Background(), EventRetentionPolicy{MaxEvents: 2, MaxBytes: 1 << 20, MaxAge: 24 * time.Hour})
	if err != nil || result.CompactedThroughSeq != 3 || result.RemainingEvents != 2 {
		t.Fatalf("compaction=%#v err=%v", result, err)
	}
	obligations, err := r.ListObservationObligations(context.Background(), 0, 10)
	if err != nil || len(obligations) != 5 {
		t.Fatalf("retention touched obligations=%d err=%v", len(obligations), err)
	}
	state, err := r.LoadEventProjectionState(context.Background())
	if err != nil || state.MaterializedThroughSeq != 5 || state.CompactedThroughSeq != 3 {
		t.Fatalf("state=%#v err=%v", state, err)
	}
}

func TestEventCursorKeyRejectsUnsafeOrCorruptFile(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openObservationRepository(t, root)
	if _, err := r.EventCursorKey(context.Background()); err != nil {
		t.Fatal(err)
	}
	path := r.eventCursorKeyPath()
	if err := os.WriteFile(path, []byte(`{"schema_version":1,"state_root_epoch":"bad","generation":"bad","secret":"`+strings.Repeat("A", 43)+`"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(root, r.limits); err == nil {
		t.Fatal("corrupt cursor key accepted")
	}
}

func testEvent(epoch string, seq observation.ChangeSeq, kind observation.EventKind, subject string) observation.Event {
	return observation.Event{SchemaVersion: 1, EventID: "evt_test_" + strings.Repeat("a", 8), StateRootEpoch: epoch, ChangeSeq: seq, Kind: kind, RecordedAt: time.Now().UTC(), Correlation: observation.Correlation{OperationID: "op-1", SessionID: "s"}, SubjectRef: subject, Summary: "summary"}
}

func TestEventCursorKeyRejectsSymlinkOnReopen(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openObservationRepository(t, root)
	keyPath := r.eventCursorKeyPath()
	if err := os.Remove(keyPath); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "cursor-key.json")
	if err := os.WriteFile(target, []byte(`{"schema_version":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, keyPath); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(root, r.limits); err == nil {
		t.Fatal("cursor key symlink accepted")
	}
}

func TestEventProjectionStateNeverRegresses(t *testing.T) {
	r := openObservationRepository(t, filepath.Join(t.TempDir(), "state"))
	if err := r.SaveEventProjectionState(context.Background(), observation.ProjectionState{SchemaVersion: 1, MaterializedThroughSeq: 8, CompactedThroughSeq: 3}); err != nil {
		t.Fatal(err)
	}
	for _, regressed := range []observation.ProjectionState{
		{SchemaVersion: 1, MaterializedThroughSeq: 7, CompactedThroughSeq: 3},
		{SchemaVersion: 1, MaterializedThroughSeq: 8, CompactedThroughSeq: 2},
	} {
		if err := r.SaveEventProjectionState(context.Background(), regressed); err == nil {
			t.Fatalf("projection regression accepted: %#v", regressed)
		}
	}
	got, err := r.LoadEventProjectionState(context.Background())
	if err != nil || got.MaterializedThroughSeq != 8 || got.CompactedThroughSeq != 3 {
		t.Fatalf("state=%#v err=%v", got, err)
	}
}

func TestEventRetentionEnforcesBytesAndAge(t *testing.T) {
	for _, tc := range []struct {
		name   string
		policy EventRetentionPolicy
		ageOld bool
	}{
		{name: "bytes", policy: EventRetentionPolicy{MaxEvents: 10, MaxBytes: 1, MaxAge: 24 * time.Hour}},
		{name: "age", policy: EventRetentionPolicy{MaxEvents: 10, MaxBytes: 1 << 20, MaxAge: time.Hour}, ageOld: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := openObservationRepository(t, filepath.Join(t.TempDir(), "state"))
			key, err := r.EventCursorKey(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			for seq := 1; seq <= 3; seq++ {
				if err := r.PutEvent(context.Background(), testEvent(key.StateRootEpoch, observation.ChangeSeq(seq), observation.EventOperationAdmitted, "operation:op-1")); err != nil {
					t.Fatal(err)
				}
			}
			if err := r.SaveEventProjectionState(context.Background(), observation.ProjectionState{SchemaVersion: 1, MaterializedThroughSeq: 3}); err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			if tc.ageOld {
				old := now.Add(-2 * time.Hour)
				if err := os.Chtimes(r.eventPath(1), old, old); err != nil {
					t.Fatal(err)
				}
			}
			policy := tc.policy
			policy.Now = now
			result, err := r.CompactEvents(context.Background(), policy)
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "bytes" && result.RemainingEvents != 0 {
				t.Fatalf("byte retention=%#v", result)
			}
			if tc.name == "age" && (result.RemainingEvents != 2 || result.CompactedThroughSeq != 1) {
				t.Fatalf("age retention=%#v", result)
			}
		})
	}
}

func TestEventRetentionCheckpointFailureDoesNotDeleteEvents(t *testing.T) {
	r := openObservationRepository(t, filepath.Join(t.TempDir(), "state"))
	key, err := r.EventCursorKey(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for seq := 1; seq <= 3; seq++ {
		if err := r.PutEvent(context.Background(), testEvent(key.StateRootEpoch, observation.ChangeSeq(seq), observation.EventOperationAdmitted, "operation:op-1")); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.SaveEventProjectionState(context.Background(), observation.ProjectionState{SchemaVersion: 1, MaterializedThroughSeq: 3}); err != nil {
		t.Fatal(err)
	}
	r.writer = failAtomicWriter("replace.rename")
	if _, err := r.CompactEvents(context.Background(), EventRetentionPolicy{MaxEvents: 1, MaxBytes: 1 << 20, MaxAge: 24 * time.Hour}); err == nil {
		t.Fatal("compaction checkpoint fault not surfaced")
	}
	r.writer = atomicWriter{}
	events, more, err := r.ListEvents(context.Background(), 0, 3, 10)
	if err != nil || more || len(events) != 3 {
		t.Fatalf("events deleted before checkpoint: len=%d more=%v err=%v", len(events), more, err)
	}
	state, err := r.LoadEventProjectionState(context.Background())
	if err != nil || state.CompactedThroughSeq != 0 {
		t.Fatalf("state=%#v err=%v", state, err)
	}
}

func TestEventListAndRetentionAreSerialized(t *testing.T) {
	r := openObservationRepository(t, filepath.Join(t.TempDir(), "state"))
	key, err := r.EventCursorKey(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for seq := 1; seq <= 40; seq++ {
		if err := r.PutEvent(context.Background(), testEvent(key.StateRootEpoch, observation.ChangeSeq(seq), observation.EventOperationAdmitted, "operation:op-1")); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.SaveEventProjectionState(context.Background(), observation.ProjectionState{SchemaVersion: 1, MaterializedThroughSeq: 40}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			if _, _, err := r.ListEvents(context.Background(), 0, 40, 100); err != nil {
				errs <- err
				return
			}
		}
		errs <- nil
	}()
	go func() {
		defer wg.Done()
		_, err := r.CompactEvents(context.Background(), EventRetentionPolicy{MaxEvents: 20, MaxBytes: 1 << 20, MaxAge: 24 * time.Hour})
		errs <- err
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}
