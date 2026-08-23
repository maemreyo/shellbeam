package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func retentionRepository(t *testing.T) *Repository {
	t.Helper()
	r, err := Open(filepath.Join(t.TempDir(), "state"), Limits{
		MaxSessions: 64, MaxSessionOutput: 1 << 20, MaxTotalState: 1 << 30, ControlReserve: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ReconcileAdmission(); err != nil {
		t.Fatal(err)
	}
	return r
}

// seedTerminal writes a finished session whose durable terminal record carries
// the given age.
func seedTerminal(t *testing.T, r *Repository, index int, age time.Duration) operation.Reservation {
	t.Helper()
	res := reservationN(index)
	reserveOK(t, r, res)
	terminateOK(t, r, res)
	if _, got := r.AppendOutput(context.Background(), res.SessionID, []byte("output\n")); got.Err != nil {
		t.Fatal(got.Err)
	}
	snapshot, err := r.LoadSession(context.Background(), res.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.UpdatedAt = time.Now().UTC().Add(-age)
	if got := r.AdvanceSession(context.Background(), snapshot); got.Err != nil {
		t.Fatal(got.Err)
	}
	return res
}

func sessionExists(t *testing.T, r *Repository, id operation.SessionID) bool {
	t.Helper()
	_, err := r.LoadSession(context.Background(), id)
	if err == nil {
		return true
	}
	if errors.Is(err, ErrNotFound) {
		return false
	}
	t.Fatal(err)
	return false
}

func sweep(t *testing.T, r *Repository, retention time.Duration) RetentionReport {
	t.Helper()
	report, err := r.CollectExpiredTerminals(context.Background(), RetentionPolicy{TerminalRetention: retention})
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func TestRetentionRemovesCollectedOperationFromActivityIndex(t *testing.T) {
	r := retentionRepository(t)
	if err := r.repairCommittedOperations(context.Background()); err != nil {
		t.Fatal(err)
	}
	res := reservationN(900)
	res.ActivityID = "activity-retention-index"
	reserveOK(t, r, res)
	terminateOK(t, r, res)
	snapshot, err := r.LoadSession(context.Background(), res.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.UpdatedAt = time.Now().UTC().Add(-48 * time.Hour)
	if got := r.AdvanceSession(context.Background(), snapshot); got.Err != nil {
		t.Fatal(got.Err)
	}

	r.activityOperationMu.RLock()
	_, indexedBefore := r.activityOperations[res.ActivityID][res.OperationID]
	r.activityOperationMu.RUnlock()
	if !indexedBefore {
		t.Fatal("new reservation was not indexed before retention")
	}
	if report := sweep(t, r, time.Hour); report.Collected != 1 {
		t.Fatalf("collected %d sessions, want 1", report.Collected)
	}
	r.activityOperationMu.RLock()
	_, indexedAfter := r.activityOperations[res.ActivityID][res.OperationID]
	r.activityOperationMu.RUnlock()
	if indexedAfter {
		t.Fatal("retention left a collected operation in the activity index")
	}
}

// TestCollectedSessionsAreNotResurrectedByStartupRepair is the hazard that
// forces the order in which a session is taken apart.
//
// Startup repairs a reservation whose session directory is missing by recreating
// that session as starting. If retention removed a session directory but left
// its operation record, the next daemon start would bring every collected
// session back to life as an active one, each holding a capacity slot -- the
// exact opposite of what collecting it was for.
func TestCollectedSessionsAreNotResurrectedByStartupRepair(t *testing.T) {
	r := retentionRepository(t)
	expired := seedTerminal(t, r, 0, 48*time.Hour)
	kept := seedTerminal(t, r, 1, time.Minute)

	if report := sweep(t, r, time.Hour); report.Collected != 1 {
		t.Fatalf("collected %d sessions, want 1", report.Collected)
	}

	// A restart must not bring it back.
	if err := r.AbandonUnresolved(context.Background(), "next-incarnation"); err != nil {
		t.Fatal(err)
	}
	if sessionExists(t, r, expired.SessionID) {
		t.Fatal("startup repair resurrected a collected session")
	}
	if !sessionExists(t, r, kept.SessionID) {
		t.Fatal("a session inside the retention window was collected")
	}
	active, _, err := r.admissionCounters()
	if err != nil {
		t.Fatal(err)
	}
	if active != 0 {
		t.Fatalf("admission counts %d active sessions after collection and restart", active)
	}
}

// TestRetentionCollectsOnlyExpiredTerminalSessions.
func TestRetentionCollectsOnlyExpiredTerminalSessions(t *testing.T) {
	r := retentionRepository(t)
	expired := seedTerminal(t, r, 0, 48*time.Hour)
	recent := seedTerminal(t, r, 1, time.Minute)
	// A session that never finished must never be collected, however old.
	running := reservationN(2)
	reserveOK(t, r, running)
	old, err := r.LoadSession(context.Background(), running.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	old.UpdatedAt = time.Now().UTC().Add(-30 * 24 * time.Hour)
	if got := r.AdvanceSession(context.Background(), old); got.Err != nil {
		t.Fatal(got.Err)
	}

	sweep(t, r, time.Hour)

	if sessionExists(t, r, expired.SessionID) {
		t.Fatal("an expired terminal session survived")
	}
	if !sessionExists(t, r, recent.SessionID) {
		t.Fatal("a session inside the window was collected")
	}
	if !sessionExists(t, r, running.SessionID) {
		t.Fatal("a session that never reached terminal was collected")
	}
}

// TestRetentionDisabledCollectsNothing: an operator who has not configured a
// window has not asked for deletion, so zero must not read as "keep nothing".
func TestRetentionDisabledCollectsNothing(t *testing.T) {
	r := retentionRepository(t)
	ancient := seedTerminal(t, r, 0, 365*24*time.Hour)
	for _, retention := range []time.Duration{0, -time.Hour} {
		report, err := r.CollectExpiredTerminals(context.Background(), RetentionPolicy{TerminalRetention: retention})
		if err != nil {
			t.Fatal(err)
		}
		if report.Collected != 0 {
			t.Fatalf("retention %s collected %d sessions", retention, report.Collected)
		}
	}
	if !sessionExists(t, r, ancient.SessionID) {
		t.Fatal("disabled retention deleted history")
	}
}

// TestRetentionCutoffIsDeterministic pins the boundary rather than leaving it to
// whichever side of "now" a test happens to land on.
func TestRetentionCutoffIsDeterministic(t *testing.T) {
	r := retentionRepository(t)
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	retention := time.Hour

	older := seedTerminal(t, r, 0, 0)
	exact := seedTerminal(t, r, 1, 0)
	newer := seedTerminal(t, r, 2, 0)
	for id, at := range map[operation.SessionID]time.Time{
		older.SessionID: now.Add(-retention).Add(-time.Nanosecond),
		exact.SessionID: now.Add(-retention),
		newer.SessionID: now.Add(-retention).Add(time.Nanosecond),
	} {
		snapshot, err := r.LoadSession(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		snapshot.UpdatedAt = at
		if got := r.AdvanceSession(context.Background(), snapshot); got.Err != nil {
			t.Fatal(got.Err)
		}
	}

	if _, err := r.CollectExpiredTerminals(context.Background(), RetentionPolicy{
		TerminalRetention: retention, Now: func() time.Time { return now },
	}); err != nil {
		t.Fatal(err)
	}
	// Strictly older than the cutoff goes; the cutoff instant itself stays.
	if sessionExists(t, r, older.SessionID) {
		t.Fatal("a session older than the cutoff survived")
	}
	if !sessionExists(t, r, exact.SessionID) {
		t.Fatal("a session exactly at the cutoff was collected")
	}
	if !sessionExists(t, r, newer.SessionID) {
		t.Fatal("a session newer than the cutoff was collected")
	}
}

// TestSweepingTwiceChangesNothingFurther.
func TestSweepingTwiceChangesNothingFurther(t *testing.T) {
	r := retentionRepository(t)
	for i := 0; i < 5; i++ {
		seedTerminal(t, r, i, 48*time.Hour)
	}
	first := sweep(t, r, time.Hour)
	if first.Collected != 5 {
		t.Fatalf("first sweep collected %d, want 5", first.Collected)
	}
	second := sweep(t, r, time.Hour)
	if second.Collected != 0 {
		t.Fatalf("second sweep collected %d, want nothing left to do", second.Collected)
	}
}

// TestInterruptedRemovalIsFinishedByTheNextSweep. A session withdrawn from view
// but not yet removed is what a crash mid-sweep leaves behind; it must not wedge
// anything, and it must not linger.
func TestInterruptedRemovalIsFinishedByTheNextSweep(t *testing.T) {
	r := retentionRepository(t)
	seedTerminal(t, r, 0, 48*time.Hour)
	// Stage a session by hand, exactly as an interrupted sweep would leave it.
	orphan := filepath.Join(r.root, "sessions", gcStagingPrefix+"01INTERRUPTED")
	if err := os.MkdirAll(orphan, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(orphan, "output.log"), []byte("half removed"), 0600); err != nil {
		t.Fatal(err)
	}

	// Reconciliation must tolerate it rather than treat it as unexpected state.
	if err := r.AbandonUnresolved(context.Background(), "after-crash"); err != nil {
		t.Fatalf("startup recovery tripped over an interrupted removal: %v", err)
	}
	sweep(t, r, time.Hour)
	if _, err := os.Stat(orphan); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("interrupted removal was left behind: %v", err)
	}
}

// TestCollectionIsAllOrNothingOnDisk. A caller must never see a session with
// some of its parts missing, so the whole session leaves view in one step --
// which is a claim about the directory's own contents, not about two
// independent calls made without anything synchronizing them.
//
// An earlier version of this test made the stronger claim instead: that two
// separate, unsynchronized calls -- LoadSession, then LoadReceipt -- would
// always agree about whether a session was gone. They cannot be made to agree
// in general, because collectSession does real work (sizing the directory,
// removing the operation record) before the single rename that actually
// changes visibility, which leaves a real window for a snapshot read to land
// before that rename and a receipt read moments later to land after it. That
// is not a torn directory; it is two clocks a caller happened to read at
// different instants, and it reproduced on every run once measured directly.
// The genuine on-disk guarantee is checked here by listing the directory's own
// contents in one call, which a rename can only ever answer wholly one way or
// the other. The caller-facing half of this -- a poll must not turn that
// window into a terminal state with no receipt behind it -- is what
// TestPollDoesNotReturnATerminalStateWithNoReceipt in the daemon package
// checks; the fix lives at the layer that serves callers, not in the store.
func TestCollectionIsAllOrNothingOnDisk(t *testing.T) {
	r := retentionRepository(t)
	expired := seedTerminal(t, r, 0, 48*time.Hour)
	sessionDir := filepath.Join(r.root, "sessions", string(expired.SessionID))

	stop := make(chan struct{})
	inconsistent := make(chan string, 4)
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			entries, err := os.ReadDir(sessionDir)
			if err != nil {
				continue // the whole directory is gone: consistent
			}
			hasMetadata, hasReceipt := false, false
			for _, entry := range entries {
				switch entry.Name() {
				case "metadata.json":
					hasMetadata = true
				case "receipt.json":
					hasReceipt = true
				}
			}
			if hasMetadata != hasReceipt {
				select {
				case inconsistent <- fmt.Sprintf("directory listing had metadata=%v receipt=%v", hasMetadata, hasReceipt):
				default:
				}
				return
			}
		}
	}()
	sweep(t, r, time.Hour)
	close(stop)
	select {
	case detail := <-inconsistent:
		t.Fatalf("a directory listing saw a partially collected session: %s", detail)
	default:
	}
}

// TestCollectionReturnsBytesWithoutUndercounting keeps the advisory total a
// conservative bound after deletion, which is what admission relies on.
func TestCollectionReturnsBytesWithoutUndercounting(t *testing.T) {
	r := retentionRepository(t)
	for i := 0; i < 6; i++ {
		seedTerminal(t, r, i, 48*time.Hour)
	}
	report := sweep(t, r, time.Hour)
	if report.Freed <= 0 {
		t.Fatalf("collecting %d sessions freed %d bytes", report.Collected, report.Freed)
	}
	exact, err := r.scanStateBytes()
	if err != nil {
		t.Fatal(err)
	}
	r.admissionMu.Lock()
	tracked := r.admission.StateBytes
	r.admissionMu.Unlock()
	if tracked < exact {
		t.Fatalf("after collection the tracked total %d undercounts the store's %d", tracked, exact)
	}
}

// TestPersistentSessionsAreNotCollected: their lifecycle continues past a
// terminal receipt, and other subsystems still hold records for them.
func TestPersistentSessionsAreNotCollected(t *testing.T) {
	r := retentionRepository(t)
	res := reservationN(0)
	res.SchemaVersion = 4
	res.Persistent = true
	res.RequestFingerprint = "request"
	res.ExecutionFingerprint = "execution"
	res.Fingerprint = ""
	res.ExecutionMode = operation.ExecutionModeShell
	res.Executable = "/bin/sh"
	if _, created, got := r.ReserveOperation(context.Background(), res); got.Err != nil || !created {
		t.Fatalf("reserve persistent = %#v", got)
	}
	snapshot, err := r.LoadSession(context.Background(), res.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.State, snapshot.Outcome = session.Completed, session.Success
	snapshot.UpdatedAt = time.Now().UTC().Add(-48 * time.Hour)
	if got := r.AdvanceSession(context.Background(), snapshot); got.Err != nil {
		t.Fatal(got.Err)
	}

	sweep(t, r, time.Hour)
	if !sessionExists(t, r, res.SessionID) {
		t.Fatal("a persistent session was collected by ordinary retention")
	}
}

// TestSweepStopsAtItsBound so a large backlog is worked through over several
// passes rather than one unbounded burst.
func TestSweepStopsAtItsBound(t *testing.T) {
	r := retentionRepository(t)
	for i := 0; i < 10; i++ {
		seedTerminal(t, r, i, 48*time.Hour)
	}
	report, err := r.CollectExpiredTerminals(context.Background(), RetentionPolicy{
		TerminalRetention: time.Hour, MaxDeletions: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Collected != 4 || !report.Remaining {
		t.Fatalf("bounded sweep = %#v, want 4 collected with more remaining", report)
	}
	total := report.Collected
	for i := 0; i < 5; i++ {
		next := sweep(t, r, time.Hour)
		total += next.Collected
	}
	if total != 10 {
		t.Fatalf("repeated bounded sweeps collected %d of 10", total)
	}
}
