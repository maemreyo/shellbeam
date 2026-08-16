package store

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/session"
)

// TestConcurrentTerminalWritesDoNotDoubleDecrement drives the one interleaving
// the choke point must survive: two writers taking the same session terminal at
// the same time. AdvanceSession runs outside terminalMu, so it really can race
// PublishTerminal's metadata repair for the same session.
//
// If the read-modify-write of the active counter is not atomic, both writers
// observe a non-terminal predecessor and each subtract one, silently freeing a
// capacity slot that no session released.
func TestConcurrentTerminalWritesDoNotDoubleDecrement(t *testing.T) {
	const sessions = 8
	for attempt := 0; attempt < 40; attempt++ {
		r := admissionRepository(t, sessions)
		for i := 0; i < sessions; i++ {
			reserveOK(t, r, reservationN(i))
		}
		if active := activeNow(t, r); active != sessions {
			t.Fatalf("active before race = %d", active)
		}

		// Take exactly one session terminal, from two writers at once.
		res := reservationN(0)
		snap, err := r.LoadSession(context.Background(), res.SessionID)
		if err != nil {
			t.Fatal(err)
		}
		snap.State = session.Completed
		snap.Outcome = session.Success

		// Widen the window between observing the predecessor state and applying
		// the delta, so the interleaving is exercised rather than left to luck.
		r.writer = atomicWriter{fail: func(point string) error {
			if point == "replace.write" {
				time.Sleep(5 * time.Millisecond)
			}
			return nil
		}}

		second := snap
		second.OutputBytes = 4096 // differs, so neither writer can early-return

		var wg sync.WaitGroup
		start := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_ = r.AdvanceSession(context.Background(), snap)
		}()
		go func() {
			defer wg.Done()
			<-start
			_ = r.AdvanceSession(context.Background(), second)
		}()
		close(start)
		wg.Wait()
		r.writer = atomicWriter{}

		want := activeByScan(t, r)
		if got := activeNow(t, r); got != want {
			t.Fatalf("attempt %d: index active = %d, state store active = %d "+
				"(one session went terminal, so exactly one slot may be released)",
				attempt, got, want)
		}
	}
}

// TestReconcileDoesNotClobberConcurrentTerminals covers the other direction:
// reconciliation scans without the index lock, so a session that goes terminal
// mid-scan must not be resurrected by the scan's stale view. A resurrected
// session never writes metadata again, so its capacity slot would be lost for
// the lifetime of the daemon.
func TestReconcileDoesNotClobberConcurrentTerminals(t *testing.T) {
	const sessions = 6
	for attempt := 0; attempt < 20; attempt++ {
		r := admissionRepository(t, sessions)
		for i := 0; i < sessions; i++ {
			reserveOK(t, r, reservationN(i))
		}

		var wg sync.WaitGroup
		start := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			if err := r.ReconcileAdmission(); err != nil {
				t.Error(err)
			}
		}()
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < sessions; i++ {
				terminateOK(t, r, reservationN(i))
			}
		}()
		close(start)
		wg.Wait()

		if got, want := activeNow(t, r), activeByScan(t, r); got != want {
			t.Fatalf("attempt %d: index active = %d, state store active = %d "+
				"(reconciliation dropped a transition that landed during its scan)",
				attempt, got, want)
		}
		if active := activeNow(t, r); active != 0 {
			t.Fatalf("attempt %d: active = %d after every session reached terminal", attempt, active)
		}
	}
}

// TestTrackedStateBytesNeverUndercount is what keeps the budget guard sound:
// admission may run on a tracked estimate only because that estimate can never
// be smaller than the store. Every durable write is counted at full size and
// no reclamation is, so the figure drifts high, never low.
func TestTrackedStateBytesNeverUndercount(t *testing.T) {
	r := admissionRepository(t, 4)
	payload := make([]byte, 2048)
	for i := 0; i < 12; i++ {
		res := reservationN(i)
		reserveOK(t, r, res)
		if _, got := r.AppendOutput(context.Background(), res.SessionID, payload); got.Err != nil {
			t.Fatal(got.Err)
		}
		terminateOK(t, r, res)
		exact, err := r.scanStateBytes()
		if err != nil {
			t.Fatal(err)
		}
		r.admissionMu.Lock()
		tracked := r.admission.StateBytes
		r.admissionMu.Unlock()
		if tracked < exact {
			t.Fatalf("after %d operations: tracked = %d, store holds %d "+
				"(an undercount would admit work the store cannot hold)", i+1, tracked, exact)
		}
	}
}

// TestAdmissionForcesExactBytesNearBudget covers the cost of that safety: the
// estimate runs high, so next to the budget it must be replaced with the exact
// figure rather than rejecting work the store could still hold.
func TestAdmissionForcesExactBytesNearBudget(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r, err := Open(root, Limits{
		MaxSessions: 4, MaxSessionOutput: 1 << 20, MaxTotalState: 24576, ControlReserve: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ReconcileAdmission(); err != nil {
		t.Fatal(err)
	}
	// Overstate the total the way repeated Replaces of the same record do.
	r.admissionMu.Lock()
	r.admission.StateBytes = 23000
	r.stateBytesScannedAt = r.now()
	r.admissionMu.Unlock()
	scansBefore := r.fullScans.Load()

	_, bytes, err := r.admissionCounters()
	if err != nil {
		t.Fatal(err)
	}
	if r.fullScans.Load() == scansBefore {
		t.Fatal("admission near the budget did not re-derive the exact total")
	}
	exact, err := r.scanStateBytes()
	if err != nil {
		t.Fatal(err)
	}
	if bytes != exact {
		t.Fatalf("admission used %d bytes near the budget, store holds %d", bytes, exact)
	}
}
