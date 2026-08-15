package daemon_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	evidenceapp "github.com/maemreyo/shellbeam/internal/app/evidence"
	core "github.com/maemreyo/shellbeam/internal/core/evidence"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

type recordingEvidenceWorker struct {
	mu    sync.Mutex
	calls []receipt.Receipt
	store interface {
		LoadReceipt(context.Context, operation.SessionID) (receipt.Receipt, error)
	}
	durableAtSchedule atomic.Bool
	scheduleErr       error
}

func (w *recordingEvidenceWorker) ScheduleTerminal(ctx context.Context, rec receipt.Receipt) error {
	if w.store != nil {
		stored, err := w.store.LoadReceipt(ctx, operation.SessionID(rec.SessionID))
		storedDigest, storedDigestErr := receipt.Digest(stored)
		scheduledDigest, scheduledDigestErr := receipt.Digest(rec)
		w.durableAtSchedule.Store(err == nil && stored.State.Terminal() && storedDigestErr == nil && scheduledDigestErr == nil && storedDigest == scheduledDigest)
	}
	w.mu.Lock()
	w.calls = append(w.calls, rec)
	w.mu.Unlock()
	return w.scheduleErr
}
func (w *recordingEvidenceWorker) count() int { w.mu.Lock(); defer w.mu.Unlock(); return len(w.calls) }

type evidenceNoTaxOwner struct {
	worker *recordingEvidenceWorker
	starts atomic.Int32
}

func (o *evidenceNoTaxOwner) Start(_ context.Context, _ operation.ExecutionSpec, _ app.OutputSink) (app.ProcessHandle, receipt.SpawnEvidence, error) {
	if o.worker.count() != 0 {
		o.worker.durableAtSchedule.Store(false)
	}
	o.starts.Add(1)
	return workerHandle{}, receipt.SpawnEvidence{Attempted: true, Succeeded: true}, nil
}

type evidenceSpawnFailOwner struct {
	worker *recordingEvidenceWorker
	starts atomic.Int32
}

func (o *evidenceSpawnFailOwner) Start(_ context.Context, _ operation.ExecutionSpec, _ app.OutputSink) (app.ProcessHandle, receipt.SpawnEvidence, error) {
	if o.worker.count() != 0 {
		o.worker.durableAtSchedule.Store(false)
	}
	o.starts.Add(1)
	return nil, receipt.SpawnEvidence{Attempted: true, Succeeded: false, ErrorCode: "spawn_failed"}, errors.New("spawn failed")
}

func TestEvidenceScheduledOnlyForEligibleReservationAfterDurableTerminal(t *testing.T) {
	store := openStructuredDaemonStore(t)
	worker := &recordingEvidenceWorker{store: store}
	owner := &evidenceNoTaxOwner{worker: worker}
	svc := app.NewService(store, owner, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100, EvidenceWorker: worker})

	plain, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "evidence-no-tax", Command: "true", CWD: "/", YieldMS: 100})
	if err != nil {
		t.Fatal(err)
	}
	waitStructuredTerminal(t, svc, plain.SessionID)
	if worker.count() != 0 || owner.starts.Load() != 1 {
		t.Fatalf("plain worker=%d starts=%d", worker.count(), owner.starts.Load())
	}

	eligible, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "evidence-eligible", Command: "true", CWD: "/", YieldMS: 100, Evidence: &core.Contract{VerificationKind: core.VerificationTest}})
	if err != nil {
		t.Fatal(err)
	}
	terminal := waitStructuredTerminal(t, svc, eligible.SessionID)
	if terminal.Outcome != session.Success || worker.count() != 1 || owner.starts.Load() != 2 || !worker.durableAtSchedule.Load() {
		t.Fatalf("terminal=%#v worker=%d starts=%d durable=%v", terminal, worker.count(), owner.starts.Load(), worker.durableAtSchedule.Load())
	}
}

func TestEvidenceScheduledAfterDurableSpawnFailureWhenVerificationDeclared(t *testing.T) {
	store := openStructuredDaemonStore(t)
	worker := &recordingEvidenceWorker{store: store}
	owner := &evidenceSpawnFailOwner{worker: worker}
	svc := app.NewService(store, owner, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100, EvidenceWorker: worker})
	view, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "evidence-spawn-fail", Command: "missing", CWD: "/", YieldMS: 100, Intent: &operation.DeclaredIntent{Kind: operation.IntentKindTest}})
	if err != nil {
		t.Fatal(err)
	}
	if !view.State.Terminal() {
		view = waitStructuredTerminal(t, svc, view.SessionID)
	}
	if view.Outcome != session.Failure || worker.count() != 1 || !worker.durableAtSchedule.Load() {
		t.Fatalf("view=%#v worker=%d durable=%v", view, worker.count(), worker.durableAtSchedule.Load())
	}
}

func TestEvidenceBackpressureDoesNotRewriteTerminalTruth(t *testing.T) {
	store := openStructuredDaemonStore(t)
	worker := &recordingEvidenceWorker{store: store, scheduleErr: evidenceapp.ErrWorkerQueueFull}
	owner := &evidenceNoTaxOwner{worker: worker}
	svc := app.NewService(store, owner, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100, EvidenceWorker: worker})
	started, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "evidence-backpressure", Command: "true", CWD: "/", YieldMS: 100, Evidence: &core.Contract{VerificationKind: core.VerificationBuild}})
	if err != nil {
		t.Fatal(err)
	}
	terminal := waitStructuredTerminal(t, svc, started.SessionID)
	rec, err := store.LoadReceipt(context.Background(), operation.SessionID(terminal.SessionID))
	if err != nil || terminal.Outcome != session.Success || rec.Outcome != session.Success || rec.State != session.Completed || worker.count() != 1 || !worker.durableAtSchedule.Load() {
		t.Fatalf("terminal=%#v receipt=%#v worker=%d durable=%v err=%v", terminal, rec, worker.count(), worker.durableAtSchedule.Load(), err)
	}
}

func TestEvidenceTerminalReplayDoesNotReschedule(t *testing.T) {
	store := openStructuredDaemonStore(t)
	worker := &recordingEvidenceWorker{store: store}
	owner := &evidenceNoTaxOwner{worker: worker}
	svc := app.NewService(store, owner, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100, EvidenceWorker: worker})
	request := app.StartRequest{ProtocolVersion: 2, OperationID: "evidence-replay", Command: "true", CWD: "/", YieldMS: 100, Evidence: &core.Contract{VerificationKind: core.VerificationTest}}
	first, err := svc.Start(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	waitStructuredTerminal(t, svc, first.SessionID)
	if _, err := svc.Start(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if owner.starts.Load() != 1 || worker.count() != 1 {
		t.Fatalf("starts=%d worker=%d", owner.starts.Load(), worker.count())
	}
}
