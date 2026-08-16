package store

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func admissionRepository(t *testing.T, maxSessions int) *Repository {
	t.Helper()
	r, err := Open(filepath.Join(t.TempDir(), "state"), Limits{
		MaxSessions: maxSessions, MaxSessionOutput: 1 << 20, MaxTotalState: 1 << 30, ControlReserve: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ReconcileAdmission(); err != nil {
		t.Fatal(err)
	}
	return r
}

func reservationN(i int) operation.Reservation {
	return operation.Reservation{
		SchemaVersion: 1, OperationID: operation.ID(fmt.Sprintf("op-%04d", i)),
		SessionID:   operation.SessionID(fmt.Sprintf("session-%04d", i)),
		Fingerprint: "fingerprint", Command: "true", CWD: "/",
		Shell: "/bin/sh", DaemonIncarnation: "daemon",
	}
}

func completedReceipt(res operation.Reservation) receipt.Receipt {
	code := 0
	return receipt.Receipt{
		SchemaVersion: 1, OperationID: string(res.OperationID), SessionID: string(res.SessionID),
		Fingerprint: res.Fingerprint, DaemonIncarnation: res.DaemonIncarnation,
		State: session.Completed, Outcome: session.Success, OutputComplete: true,
		Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true},
		Exit:  receipt.ExitEvidence{Reaped: true, Code: &code},
	}
}

func reserveOK(t *testing.T, r *Repository, res operation.Reservation) {
	t.Helper()
	if _, created, got := r.ReserveOperation(context.Background(), res); got.Err != nil || !created {
		t.Fatalf("reserve %s = %#v, created = %v", res.OperationID, got, created)
	}
}

func terminateOK(t *testing.T, r *Repository, res operation.Reservation) {
	t.Helper()
	if got := r.PublishTerminal(context.Background(), completedReceipt(res)); got.Err != nil {
		t.Fatalf("publish terminal %s = %#v", res.SessionID, got)
	}
}

func activeNow(t *testing.T, r *Repository) int {
	t.Helper()
	r.admissionMu.Lock()
	defer r.admissionMu.Unlock()
	return r.admission.ActiveSessions
}

// activeByScan recounts from the state store itself, bypassing the index.
func activeByScan(t *testing.T, r *Repository) int {
	t.Helper()
	active, _, err := r.usage()
	if err != nil {
		t.Fatal(err)
	}
	return active
}

// TestAdmissionNeverRescansHistory is the regression gate for the admission
// index: once the counters are reconciled, no admission may walk the state
// store again, however much history accumulates.
func TestAdmissionNeverRescansHistory(t *testing.T) {
	r := admissionRepository(t, 4)
	baseline := r.fullScans.Load()
	for i := 0; i < 200; i++ {
		res := reservationN(i)
		reserveOK(t, r, res)
		terminateOK(t, r, res)
	}
	if got := r.fullScans.Load(); got != baseline {
		t.Fatalf("admission performed %d full-tree scans over 200 operations", got-baseline)
	}
	if active := activeNow(t, r); active != 0 {
		t.Fatalf("active = %d after every session reached terminal", active)
	}
}

// TestAdmissionRejectionDoesNotRescan covers the capacity_exceeded path, which
// is reached before any reservation exists and so has no receipt to audit.
func TestAdmissionRejectionDoesNotRescan(t *testing.T) {
	r := admissionRepository(t, 2)
	reserveOK(t, r, reservationN(0))
	reserveOK(t, r, reservationN(1))
	baseline := r.fullScans.Load()
	for i := 2; i < 20; i++ {
		_, created, got := r.ReserveOperation(context.Background(), reservationN(i))
		if created || got.Err == nil || got.Err.Error() != "capacity_exceeded" {
			t.Fatalf("reserve at capacity = %#v, created = %v", got, created)
		}
	}
	if got := r.fullScans.Load(); got != baseline {
		t.Fatalf("rejection performed %d full-tree scans", got-baseline)
	}
}

// TestActiveCountMatchesStateStore keeps the index honest against the scan it
// replaced, across the full lifecycle.
func TestActiveCountMatchesStateStore(t *testing.T) {
	r := admissionRepository(t, 8)
	for i := 0; i < 6; i++ {
		reserveOK(t, r, reservationN(i))
		if got, want := activeNow(t, r), activeByScan(t, r); got != want {
			t.Fatalf("after reserve %d: index active = %d, scan active = %d", i, got, want)
		}
	}
	for i := 0; i < 6; i++ {
		terminateOK(t, r, reservationN(i))
		if got, want := activeNow(t, r), activeByScan(t, r); got != want {
			t.Fatalf("after terminal %d: index active = %d, scan active = %d", i, got, want)
		}
	}
	if active := activeNow(t, r); active != 0 {
		t.Fatalf("active = %d", active)
	}
}

// TestRepeatedTerminalWritesDoNotDoubleCount exercises the idempotence the
// choke point provides: replayed reservations and republished receipts both
// rewrite metadata.json, and neither may move the counter twice.
func TestRepeatedTerminalWritesDoNotDoubleCount(t *testing.T) {
	r := admissionRepository(t, 4)
	res := reservationN(0)
	reserveOK(t, r, res)
	if active := activeNow(t, r); active != 1 {
		t.Fatalf("active after reserve = %d", active)
	}
	// Replaying the same reservation rewrites session metadata.
	if _, created, got := r.ReserveOperation(context.Background(), res); got.Err != nil || created {
		t.Fatalf("replay = %#v, created = %v", got, created)
	}
	if active := activeNow(t, r); active != 1 {
		t.Fatalf("active after reservation replay = %d", active)
	}
	terminateOK(t, r, res)
	terminateOK(t, r, res)
	snap, err := r.LoadSession(context.Background(), res.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if got := r.AdvanceSession(context.Background(), snap); got.Err != nil {
		t.Fatal(got.Err)
	}
	if active := activeNow(t, r); active != 0 {
		t.Fatalf("active after repeated terminal writes = %d", active)
	}
	if scanned := activeByScan(t, r); scanned != 0 {
		t.Fatalf("scan active = %d", scanned)
	}
}

// TestAdmissionCountSurvivesRestart covers recovery: a daemon that dies with
// live sessions must not leak their capacity into the next incarnation.
func TestAdmissionCountSurvivesRestart(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	first, err := Open(root, Limits{MaxSessions: 4, MaxSessionOutput: 1 << 20, MaxTotalState: 1 << 30, ControlReserve: 1024})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		reserveOK(t, first, reservationN(i))
	}
	if active := activeNow(t, first); active != 3 {
		t.Fatalf("active before restart = %d", active)
	}

	// A new incarnation opens the same state store without any handover. The
	// persisted index is only a seed -- it may lag, because index writes are
	// coalesced -- so recovery, not the seed, is what must be exact.
	second, err := Open(root, Limits{MaxSessions: 4, MaxSessionOutput: 1 << 20, MaxTotalState: 1 << 30, ControlReserve: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if err := second.AbandonUnresolved(context.Background(), "restarted-daemon"); err != nil {
		t.Fatal(err)
	}
	if active := activeNow(t, second); active != 0 {
		t.Fatalf("active after recovery = %d, want the abandoned sessions to release capacity", active)
	}
	baseline := second.fullScans.Load()
	for i := 3; i < 7; i++ {
		reserveOK(t, second, reservationN(i))
	}
	if got := second.fullScans.Load(); got != baseline {
		t.Fatalf("post-recovery admission performed %d full-tree scans", got-baseline)
	}

	// Reconciliation publishes the index, so a later Open starts from it
	// instead of rescanning.
	if err := second.ReconcileAdmission(); err != nil {
		t.Fatal(err)
	}
	third, err := Open(root, Limits{MaxSessions: 4, MaxSessionOutput: 1 << 20, MaxTotalState: 1 << 30, ControlReserve: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if active := activeNow(t, third); active != 4 {
		t.Fatalf("seeded active = %d, want the 4 sessions reserved after recovery", active)
	}
	if !third.admissionReconciled() {
		t.Fatal("a published index must seed the next Open without a rescan")
	}
}

// TestReconciliationRepairsDriftedIndex proves the index is recoverable rather
// than authoritative: a corrupted count is corrected by reconciliation.
func TestReconciliationRepairsDriftedIndex(t *testing.T) {
	r := admissionRepository(t, 8)
	for i := 0; i < 3; i++ {
		reserveOK(t, r, reservationN(i))
	}
	r.admissionMu.Lock()
	r.admission.ActiveSessions = 99
	r.admissionMu.Unlock()
	if _, _, got := r.ReserveOperation(context.Background(), reservationN(9)); got.Err == nil {
		t.Fatal("drifted index did not gate admission")
	}
	if err := r.ReconcileAdmission(); err != nil {
		t.Fatal(err)
	}
	if active := activeNow(t, r); active != 3 {
		t.Fatalf("active after reconciliation = %d", active)
	}
	reserveOK(t, r, reservationN(9))
}

// TestConcurrentAdmissionRespectsSessionLimit asserts the limit still holds
// when admission is contended -- the property the serialized rescan used to
// provide.
func TestConcurrentAdmissionRespectsSessionLimit(t *testing.T) {
	const (
		limit    = 4
		claimers = 32
	)
	r := admissionRepository(t, limit)
	var wg sync.WaitGroup
	granted := make([]bool, claimers)
	start := make(chan struct{})
	for i := 0; i < claimers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, created, got := r.ReserveOperation(context.Background(), reservationN(i))
			granted[i] = created && got.Err == nil
		}(i)
	}
	close(start)
	wg.Wait()

	admitted := 0
	for _, ok := range granted {
		if ok {
			admitted++
		}
	}
	if admitted != limit {
		t.Fatalf("admitted %d concurrent sessions, limit = %d", admitted, limit)
	}
	if active := activeNow(t, r); active != limit {
		t.Fatalf("index active = %d after contended admission", active)
	}
	if scanned := activeByScan(t, r); scanned != limit {
		t.Fatalf("scan active = %d after contended admission", scanned)
	}
}

// TestStateBytesConvergeAfterRefresh covers the advisory half of the index:
// incremental accounting tracks session output, and the off-hot-path refresh
// re-derives everything else.
func TestStateBytesConvergeAfterRefresh(t *testing.T) {
	r := admissionRepository(t, 4)
	res := reservationN(0)
	reserveOK(t, r, res)
	payload := make([]byte, 4096)
	if _, got := r.AppendOutput(context.Background(), res.SessionID, payload); got.Err != nil {
		t.Fatal(got.Err)
	}
	r.admissionMu.Lock()
	tracked := r.admission.StateBytes
	r.admissionMu.Unlock()
	if tracked < int64(len(payload)) {
		t.Fatalf("state bytes = %d, want session output to be tracked incrementally", tracked)
	}

	// Force the refresh window open and let the asynchronous scan land.
	r.admissionMu.Lock()
	r.stateBytesScannedAt = r.now().Add(-2 * stateBytesRefreshInterval)
	r.admissionMu.Unlock()
	r.maybeRefreshStateBytes()
	r.awaitStateBytesRefresh()

	exact, err := r.scanStateBytes()
	if err != nil {
		t.Fatal(err)
	}
	r.admissionMu.Lock()
	refreshed := r.admission.StateBytes
	r.admissionMu.Unlock()
	if refreshed != exact {
		t.Fatalf("refreshed state bytes = %d, scan = %d", refreshed, exact)
	}
}

// TestCompactionReleasesTrackedBytes keeps reclamation symmetric with growth.
func TestCompactionReleasesTrackedBytes(t *testing.T) {
	r := admissionRepository(t, 4)
	res := reservationN(0)
	reserveOK(t, r, res)
	payload := make([]byte, 8192)
	if _, got := r.AppendOutput(context.Background(), res.SessionID, payload); got.Err != nil {
		t.Fatal(got.Err)
	}
	terminateOK(t, r, res)
	r.admissionMu.Lock()
	before := r.admission.StateBytes
	r.admissionMu.Unlock()
	if got := r.Compact(context.Background(), res.SessionID); got.Err != nil {
		t.Fatal(got.Err)
	}
	r.admissionMu.Lock()
	after := r.admission.StateBytes
	r.admissionMu.Unlock()
	if after >= before {
		t.Fatalf("compaction did not release tracked bytes: %d -> %d", before, after)
	}
	// Compaction also rewrites session metadata, so the net release is the
	// output log minus that write -- but the total must stay conservative.
	exact, err := r.scanStateBytes()
	if err != nil {
		t.Fatal(err)
	}
	if after < exact {
		t.Fatalf("tracked %d bytes after compaction, store holds %d", after, exact)
	}
}

// BenchmarkReserveOperation measures admission against a store that already
// holds terminal history. The per-operation cost must not track historySize.
func BenchmarkReserveOperation(b *testing.B) {
	for _, historySize := range []int{0, 250, 1000} {
		b.Run(fmt.Sprintf("history=%d", historySize), func(b *testing.B) {
			r, err := Open(filepath.Join(b.TempDir(), "state"), Limits{
				MaxSessions: 4, MaxSessionOutput: 1 << 20, MaxTotalState: 1 << 34, ControlReserve: 1024,
			})
			if err != nil {
				b.Fatal(err)
			}
			for i := 0; i < historySize; i++ {
				res := reservationN(i)
				if _, _, got := r.ReserveOperation(context.Background(), res); got.Err != nil {
					b.Fatal(got.Err)
				}
				if got := r.PublishTerminal(context.Background(), completedReceipt(res)); got.Err != nil {
					b.Fatal(got.Err)
				}
			}
			if err := r.ReconcileAdmission(); err != nil {
				b.Fatal(err)
			}
			scansBefore := r.fullScans.Load()
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				res := reservationN(historySize + i)
				if _, _, got := r.ReserveOperation(context.Background(), res); got.Err != nil {
					b.Fatal(got.Err)
				}
				if got := r.PublishTerminal(context.Background(), completedReceipt(res)); got.Err != nil {
					b.Fatal(got.Err)
				}
			}
			b.StopTimer()
			if got := r.fullScans.Load(); got != scansBefore {
				b.Fatalf("benchmark loop performed %d full-tree scans", got-scansBefore)
			}
		})
	}
}
