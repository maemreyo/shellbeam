package store

import (
	"context"
	"testing"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/observation"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	persistent "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func TestPersistentLifecycleEventsAreExactlyOncePerCommittedTransition(t *testing.T) {
	r := openRecoveryRepository(t, t.TempDir()+"/state")
	base := time.Date(2026, 8, 16, 3, 0, 0, 0, time.UTC)
	binding := persistentBinding("persistent-event-session", "persistent-event-op", "event-server", base)
	reservePersistentOperationWithMetadata(t, r, binding, base)
	if _, created, result := r.ReservePersistentBinding(context.Background(), binding); result.Err != nil || !created {
		t.Fatalf("reserve created=%v result=%#v", created, result)
	}

	live := binding
	live.Lifecycle = persistent.LifecycleLive
	live.UpdatedAt = base.Add(time.Second)
	if result := r.AdvancePersistentBinding(context.Background(), live); result.Err != nil {
		t.Fatal(result.Err)
	}
	if result := r.AdvancePersistentBinding(context.Background(), live); result.Err != nil {
		t.Fatal(result.Err)
	}

	terminal := live
	terminal.Lifecycle = persistent.LifecycleTerminal
	terminal.UpdatedAt = base.Add(2 * time.Second)
	if result := r.AdvancePersistentBinding(context.Background(), terminal); result.Err != nil {
		t.Fatal(result.Err)
	}
	if result := r.AdvancePersistentBinding(context.Background(), terminal); result.Err != nil {
		t.Fatal(result.Err)
	}

	assertPersistentEventKinds(t, r, []observation.EventKind{observation.EventPersistentSessionStarted, observation.EventPersistentSessionTerminal})
}

func TestPersistentReattachedAndLostEventsAreExactlyOnceAndSafe(t *testing.T) {
	r := openRecoveryRepository(t, t.TempDir()+"/state")
	base := time.Date(2026, 8, 16, 4, 0, 0, 0, time.UTC)
	binding := persistentBinding("persistent-reattach-event", "persistent-reattach-op", "reattach-server", base)
	reservePersistentOperationWithMetadata(t, r, binding, base)
	if _, created, result := r.ReservePersistentBinding(context.Background(), binding); result.Err != nil || !created {
		t.Fatalf("reserve=%v %#v", created, result)
	}
	live := binding
	live.Lifecycle = persistent.LifecycleLive
	live.UpdatedAt = base.Add(time.Second)
	if result := r.AdvancePersistentBinding(context.Background(), live); result.Err != nil {
		t.Fatal(result.Err)
	}

	snapshot := session.Snapshot{SchemaVersion: 1, OperationID: binding.OperationID, SessionID: binding.SessionID, DaemonIncarnation: "daemon-reattached", State: session.Running, OutputAvailable: true, UpdatedAt: base.Add(2 * time.Second)}
	if result := r.AdvancePersistentReattachedSession(context.Background(), snapshot); result.Err != nil {
		t.Fatal(result.Err)
	}
	if result := r.AdvancePersistentReattachedSession(context.Background(), snapshot); result.Err != nil {
		t.Fatal(result.Err)
	}

	lost := live
	lost.Lifecycle = persistent.LifecycleLost
	lost.UpdatedAt = base.Add(3 * time.Second)
	if result := r.AdvancePersistentBinding(context.Background(), lost); result.Err != nil {
		t.Fatal(result.Err)
	}
	if result := r.AdvancePersistentBinding(context.Background(), lost); result.Err != nil {
		t.Fatal(result.Err)
	}

	assertPersistentEventKinds(t, r, []observation.EventKind{observation.EventPersistentSessionStarted, observation.EventPersistentSessionReattached, observation.EventPersistentSessionLost})
}

func assertPersistentEventKinds(t *testing.T, r *Repository, want []observation.EventKind) {
	t.Helper()
	obligations, err := r.ListObservationObligations(context.Background(), 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]observation.EventKind, 0, len(want))
	for _, item := range obligations {
		switch item.Kind {
		case observation.EventPersistentSessionStarted, observation.EventPersistentSessionReattached, observation.EventPersistentSessionTerminal, observation.EventPersistentSessionLost:
			if item.State != observation.ObligationCommitted {
				t.Fatalf("persistent event not committed: %#v", item)
			}
			if item.Correlation.SessionID == "" || item.Correlation.OperationID == "" {
				t.Fatalf("missing safe correlation: %#v", item)
			}
			got = append(got, item.Kind)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("event kinds=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event kinds=%v want=%v", got, want)
		}
	}
}

var _ = operation.SessionID("persistent-event-session")

func TestAdvancePersistentReattachedSessionRejectsMismatchedOrNonLiveCanonicalStateWithoutEvent(t *testing.T) {
	newLive := func(t *testing.T, suffix string) (*Repository, persistent.Binding, session.Snapshot) {
		t.Helper()
		r := openRecoveryRepository(t, t.TempDir()+"/state")
		base := time.Date(2026, 8, 16, 5, 0, 0, 0, time.UTC)
		binding := persistentBinding("persistent-reattach-guard-"+suffix, "persistent-reattach-guard-op-"+suffix, "guard-"+suffix, base)
		reservePersistentOperationWithMetadata(t, r, binding, base)
		if _, created, result := r.ReservePersistentBinding(context.Background(), binding); result.Err != nil || !created {
			t.Fatalf("reserve=%v %#v", created, result)
		}
		live := binding
		live.Lifecycle = persistent.LifecycleLive
		live.UpdatedAt = base.Add(time.Second)
		if result := r.AdvancePersistentBinding(context.Background(), live); result.Err != nil {
			t.Fatal(result.Err)
		}
		snap, err := r.LoadSession(context.Background(), operation.SessionID(binding.SessionID))
		if err != nil {
			t.Fatal(err)
		}
		snap.DaemonIncarnation = "new-daemon"
		snap.State = session.Running
		snap.OutputAvailable = true
		snap.UpdatedAt = base.Add(2 * time.Second)
		return r, live, snap
	}

	t.Run("operation mismatch", func(t *testing.T) {
		r, _, snap := newLive(t, "op")
		snap.OperationID = "different-operation"
		if result := r.AdvancePersistentReattachedSession(context.Background(), snap); result.Err == nil {
			t.Fatal("operation mismatch accepted")
		}
		if got := countPersistentEventKind(t, r, observation.EventPersistentSessionReattached); got != 0 {
			t.Fatalf("reattached events=%d", got)
		}
	})

	t.Run("binding not live", func(t *testing.T) {
		r, live, snap := newLive(t, "lost")
		lost := live
		lost.Lifecycle = persistent.LifecycleLost
		lost.UpdatedAt = snap.UpdatedAt.Add(time.Second)
		if result := r.AdvancePersistentBinding(context.Background(), lost); result.Err != nil {
			t.Fatal(result.Err)
		}
		if result := r.AdvancePersistentReattachedSession(context.Background(), snap); result.Err == nil {
			t.Fatal("lost binding accepted for reattach")
		}
		if got := countPersistentEventKind(t, r, observation.EventPersistentSessionReattached); got != 0 {
			t.Fatalf("reattached events=%d", got)
		}
	})
}

func countPersistentEventKind(t *testing.T, r *Repository, kind observation.EventKind) int {
	t.Helper()
	items, err := r.ListObservationObligations(context.Background(), 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, item := range items {
		if item.Kind == kind && item.State == observation.ObligationCommitted {
			count++
		}
	}
	return count
}

func TestPersistentPreparedEventsRecoverFromCanonicalStateAfterRestart(t *testing.T) {
	t.Run("lifecycle transitions", testPersistentLifecyclePreparedRecovery)
	t.Run("reattached", testPersistentReattachedPreparedRecovery)
}

func testPersistentLifecyclePreparedRecovery(t *testing.T) {
	for _, tc := range []struct {
		name      string
		lifecycle persistent.Lifecycle
		kind      observation.EventKind
	}{
		{name: "started", lifecycle: persistent.LifecycleLive, kind: observation.EventPersistentSessionStarted},
		{name: "terminal", lifecycle: persistent.LifecycleTerminal, kind: observation.EventPersistentSessionTerminal},
		{name: "lost", lifecycle: persistent.LifecycleLost, kind: observation.EventPersistentSessionLost},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir() + "/state"
			r := openRecoveryRepository(t, root)
			base := time.Date(2026, 8, 16, 8, 0, 0, 0, time.UTC)
			binding := persistentBinding("persistent-crash-"+tc.name, "persistent-crash-op-"+tc.name, "crash-"+tc.name, base)
			reservePersistentOperationWithMetadata(t, r, binding, base)
			if _, created, result := r.ReservePersistentBinding(context.Background(), binding); result.Err != nil || !created {
				t.Fatalf("reserve=%v result=%#v", created, result)
			}
			want := binding
			want.Lifecycle = tc.lifecycle
			want.UpdatedAt = base.Add(time.Second)
			seq, prepared := r.preparePersistentLifecycleObservation(context.Background(), binding, want)
			if prepared.Err != nil || seq == 0 {
				t.Fatalf("prepare seq=%d result=%#v", seq, prepared)
			}
			if result := r.writer.Replace(r.persistentBindingPath(operation.SessionID(binding.SessionID)), want); result.Err != nil {
				t.Fatal(result.Err)
			}
			assertPreparedObservation(t, r, seq)
			reopened := openRecoveryRepository(t, root)
			if err := reopened.AbandonUnresolved(context.Background(), "daemon-after-crash"); err != nil {
				t.Fatal(err)
			}
			assertRecoveredPersistentObservation(t, reopened, seq, tc.kind)
		})
	}
}

func testPersistentReattachedPreparedRecovery(t *testing.T) {
	root := t.TempDir() + "/state"
	r := openRecoveryRepository(t, root)
	base := time.Date(2026, 8, 16, 9, 0, 0, 0, time.UTC)
	binding := persistentBinding("persistent-crash-reattached", "persistent-crash-op-reattached", "crash-reattached", base)
	reservePersistentOperationWithMetadata(t, r, binding, base)
	if _, created, result := r.ReservePersistentBinding(context.Background(), binding); result.Err != nil || !created {
		t.Fatalf("reserve=%v result=%#v", created, result)
	}
	live := binding
	live.Lifecycle = persistent.LifecycleLive
	live.UpdatedAt = base.Add(time.Second)
	if result := r.AdvancePersistentBinding(context.Background(), live); result.Err != nil {
		t.Fatal(result.Err)
	}
	snapshot, err := r.LoadSession(context.Background(), operation.SessionID(binding.SessionID))
	if err != nil {
		t.Fatal(err)
	}
	snapshot.DaemonIncarnation = "daemon-after-reattach"
	snapshot.State, snapshot.OutputAvailable, snapshot.UpdatedAt = session.Running, true, base.Add(2*time.Second)
	request := observation.PrepareRequest{Kind: observation.EventPersistentSessionReattached, Correlation: correlationFromReservation(operation.Reservation{OperationID: operation.ID(binding.OperationID), SessionID: operation.SessionID(binding.SessionID)}), SubjectRef: "persistent:" + binding.SessionID + ":reattached:" + snapshot.DaemonIncarnation, Summary: "persistent session reattached"}
	seq, prepared := r.prepareExecutionObservation(context.Background(), request)
	if prepared.Err != nil || seq == 0 {
		t.Fatalf("prepare seq=%d result=%#v", seq, prepared)
	}
	if result := r.AdvanceSession(context.Background(), snapshot); result.Err != nil {
		t.Fatal(result.Err)
	}
	assertPreparedObservation(t, r, seq)
	reopened := openRecoveryRepository(t, root)
	if err := reopened.AbandonUnresolved(context.Background(), "daemon-next"); err != nil {
		t.Fatal(err)
	}
	assertRecoveredPersistentObservation(t, reopened, seq, observation.EventPersistentSessionReattached)
}

func assertPreparedObservation(t *testing.T, r *Repository, seq observation.ChangeSeq) {
	t.Helper()
	item, err := r.readObservation(seq)
	if err != nil || item.State != observation.ObligationPrepared {
		t.Fatalf("before restart item=%#v err=%v", item, err)
	}
}

func assertRecoveredPersistentObservation(t *testing.T, r *Repository, seq observation.ChangeSeq, kind observation.EventKind) {
	t.Helper()
	item, err := r.readObservation(seq)
	if err != nil || item.State != observation.ObligationCommitted || item.Kind != kind {
		t.Fatalf("after restart item=%#v err=%v", item, err)
	}
	if got := countPersistentEventKind(t, r, kind); got != 1 {
		t.Fatalf("%s events=%d", kind, got)
	}
}
