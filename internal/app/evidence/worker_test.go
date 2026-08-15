package evidence

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/evidence"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

type workerDeriverFake struct {
	mu      sync.Mutex
	calls   []receipt.Receipt
	block   <-chan struct{}
	started chan<- struct{}
	err     error
}

func (d *workerDeriverFake) DeriveTerminal(ctx context.Context, rec receipt.Receipt) (core.Record, bool, error) {
	if d.started != nil {
		select {
		case d.started <- struct{}{}:
		default:
		}
	}
	if d.block != nil {
		select {
		case <-d.block:
		case <-ctx.Done():
			return core.Record{}, false, ctx.Err()
		}
	}
	d.mu.Lock()
	d.calls = append(d.calls, rec)
	d.mu.Unlock()
	if d.err != nil {
		return core.Record{}, false, d.err
	}
	return core.Record{OperationID: rec.OperationID}, true, nil
}
func (d *workerDeriverFake) count() int { d.mu.Lock(); defer d.mu.Unlock(); return len(d.calls) }

type workerRecoveryFake struct {
	mu           sync.Mutex
	candidates   []operation.ID
	reservations map[operation.ID]operation.Reservation
	sessions     map[operation.SessionID]session.Snapshot
	receipts     map[operation.SessionID]receipt.Receipt
	existing     map[operation.ID]core.Record
	cleared      []operation.ID
}

func (r *workerRecoveryFake) ListEvidenceCandidates(context.Context, int) ([]operation.ID, error) {
	return append([]operation.ID(nil), r.candidates...), nil
}
func (r *workerRecoveryFake) FindOperation(_ context.Context, id operation.ID) (operation.Reservation, bool, error) {
	value, ok := r.reservations[id]
	return value, ok, nil
}
func (r *workerRecoveryFake) LoadSession(_ context.Context, id operation.SessionID) (session.Snapshot, error) {
	return r.sessions[id], nil
}
func (r *workerRecoveryFake) LoadReceipt(_ context.Context, id operation.SessionID) (receipt.Receipt, error) {
	return r.receipts[id], nil
}
func (r *workerRecoveryFake) FindEvidenceByOperation(_ context.Context, id operation.ID) (core.Record, bool, error) {
	value, ok := r.existing[id]
	return value, ok, nil
}
func (r *workerRecoveryFake) ClearEvidenceCandidate(_ context.Context, id operation.ID) error {
	r.mu.Lock()
	r.cleared = append(r.cleared, id)
	r.mu.Unlock()
	return nil
}

func TestEvidenceWorkerQueueIsBoundedAndSuccessfulDerivationClearsCandidate(t *testing.T) {
	block := make(chan struct{})
	started := make(chan struct{}, 1)
	deriver := &workerDeriverFake{block: block, started: started}
	recovery := &workerRecoveryFake{}
	worker, err := NewWorker(deriver, recovery, WorkerOptions{MaxWorkers: 1, QueueDepth: 1, MaxDuration: time.Second, RecoveryLimit: 8})
	if err != nil {
		t.Fatal(err)
	}
	rec1 := workerTerminalReceipt("worker-op-1", "worker-session-1")
	rec2 := workerTerminalReceipt("worker-op-2", "worker-session-2")
	rec3 := workerTerminalReceipt("worker-op-3", "worker-session-3")
	if err := worker.ScheduleTerminal(context.Background(), rec1); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker did not enter derivation")
	}
	if err := worker.ScheduleTerminal(context.Background(), rec2); err != nil {
		t.Fatal(err)
	}
	if err := worker.ScheduleTerminal(context.Background(), rec3); !errors.Is(err, ErrWorkerQueueFull) {
		t.Fatalf("queue err=%v", err)
	}
	close(block)
	if err := worker.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if deriver.count() != 2 {
		t.Fatalf("derive calls=%d", deriver.count())
	}
	if len(recovery.cleared) != 2 {
		t.Fatalf("cleared=%#v", recovery.cleared)
	}
}

func TestEvidenceWorkerRecoverUsesCandidateIndexWithoutScanningRunningSessions(t *testing.T) {
	terminalID := operation.ID("recover-terminal")
	runningID := operation.ID("recover-running")
	existingID := operation.ID("recover-existing")
	terminalSID := operation.SessionID("recover-terminal-session")
	runningSID := operation.SessionID("recover-running-session")
	existingSID := operation.SessionID("recover-existing-session")
	recovery := &workerRecoveryFake{
		candidates: []operation.ID{terminalID, runningID, existingID},
		reservations: map[operation.ID]operation.Reservation{
			terminalID: {OperationID: terminalID, SessionID: terminalSID, Evidence: &core.Contract{VerificationKind: core.VerificationTest}},
			runningID:  {OperationID: runningID, SessionID: runningSID, Evidence: &core.Contract{VerificationKind: core.VerificationTest}},
			existingID: {OperationID: existingID, SessionID: existingSID},
		},
		sessions: map[operation.SessionID]session.Snapshot{
			terminalSID: {OperationID: string(terminalID), SessionID: string(terminalSID), State: session.Completed, Outcome: session.Success},
			runningSID:  {OperationID: string(runningID), SessionID: string(runningSID), State: session.Running},
		},
		receipts: map[operation.SessionID]receipt.Receipt{terminalSID: workerTerminalReceipt(string(terminalID), string(terminalSID))},
		existing: map[operation.ID]core.Record{existingID: {OperationID: string(existingID)}},
	}
	deriver := &workerDeriverFake{}
	worker, err := NewWorker(deriver, recovery, WorkerOptions{MaxWorkers: 1, QueueDepth: 4, MaxDuration: time.Second, RecoveryLimit: 8})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := worker.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if deriver.count() != 1 {
		t.Fatalf("derive calls=%d", deriver.count())
	}
	if len(recovery.cleared) != 2 {
		t.Fatalf("cleared=%#v", recovery.cleared)
	}
	if recovery.cleared[0] != existingID && recovery.cleared[1] != existingID {
		t.Fatalf("existing candidate not cleared: %#v", recovery.cleared)
	}
}

func TestEvidenceWorkerDerivationFailureLeavesCandidateForRecovery(t *testing.T) {
	deriver := &workerDeriverFake{err: errors.New("derive failed")}
	recovery := &workerRecoveryFake{}
	worker, err := NewWorker(deriver, recovery, WorkerOptions{MaxWorkers: 1, QueueDepth: 1, MaxDuration: time.Second, RecoveryLimit: 8})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.ScheduleTerminal(context.Background(), workerTerminalReceipt("failed-op", "failed-session")); err != nil {
		t.Fatal(err)
	}
	if err := worker.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(recovery.cleared) != 0 {
		t.Fatalf("failed derivation cleared candidate: %#v", recovery.cleared)
	}
}

func workerTerminalReceipt(op, sid string) receipt.Receipt {
	zero := 0
	return receipt.Receipt{SchemaVersion: 2, OperationID: op, SessionID: sid, RequestFingerprint: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ExecutionFingerprint: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", ObservationBindingFingerprint: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", DaemonIncarnation: "d", ExecutionMode: string(operation.ExecutionModeShell), Executable: "/bin/sh", Shell: "/bin/sh", CWD: "/", State: session.Completed, Outcome: session.Success, OutputComplete: true, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}, Exit: receipt.ExitEvidence{Reaped: true, Code: &zero}}
}

func TestEvidenceWorkerRecoverClearsOrphanCandidate(t *testing.T) {
	orphan := operation.ID("recover-orphan")
	recovery := &workerRecoveryFake{candidates: []operation.ID{orphan}, reservations: map[operation.ID]operation.Reservation{}, existing: map[operation.ID]core.Record{}}
	worker, err := NewWorker(&workerDeriverFake{}, recovery, WorkerOptions{MaxWorkers: 1, QueueDepth: 1, MaxDuration: time.Second, RecoveryLimit: 8})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := worker.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(recovery.cleared) != 1 || recovery.cleared[0] != orphan {
		t.Fatalf("cleared=%#v", recovery.cleared)
	}
}
