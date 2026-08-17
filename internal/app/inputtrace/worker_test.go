package inputtrace

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/inputtrace"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

type traceWorkerMaterializer struct {
	mu      sync.Mutex
	calls   []receipt.Receipt
	block   <-chan struct{}
	started chan<- struct{}
	err     error
}

func (m *traceWorkerMaterializer) MaterializeTerminal(ctx context.Context, rec receipt.Receipt) (core.Record, error) {
	m.mu.Lock()
	m.calls = append(m.calls, rec)
	m.mu.Unlock()
	if m.started != nil {
		select {
		case m.started <- struct{}{}:
		default:
		}
	}
	if m.block != nil {
		select {
		case <-m.block:
		case <-ctx.Done():
			return core.Record{}, ctx.Err()
		}
	}
	return core.Record{}, m.err
}
func (m *traceWorkerMaterializer) count() int { m.mu.Lock(); defer m.mu.Unlock(); return len(m.calls) }

func TestE27TraceWorkerSchedulesTerminalNonBlockingAndIgnoresMaterializerFailure(t *testing.T) {
	materializer := &traceWorkerMaterializer{err: errors.New("derive failed")}
	worker, err := NewWorker(materializer, WorkerOptions{MaxWorkers: 1, QueueDepth: 1, MaxDuration: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	rec := validWorkerReceipt(t, "worker-one")
	if err := worker.ScheduleTerminal(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for materializer.count() != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if materializer.count() != 1 {
		t.Fatalf("calls=%d", materializer.count())
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := worker.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestE27TraceWorkerQueueIsBounded(t *testing.T) {
	block := make(chan struct{})
	started := make(chan struct{}, 1)
	materializer := &traceWorkerMaterializer{block: block, started: started}
	worker, err := NewWorker(materializer, WorkerOptions{MaxWorkers: 1, QueueDepth: 1, MaxDuration: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.ScheduleTerminal(context.Background(), validWorkerReceipt(t, "q1")); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := worker.ScheduleTerminal(context.Background(), validWorkerReceipt(t, "q2")); err != nil {
		t.Fatal(err)
	}
	if err := worker.ScheduleTerminal(context.Background(), validWorkerReceipt(t, "q3")); !errors.Is(err, ErrWorkerQueueFull) {
		t.Fatalf("queue err=%v", err)
	}
	close(block)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := worker.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestE27TraceWorkerRejectsNonTerminalJob(t *testing.T) {
	worker, err := NewWorker(&traceWorkerMaterializer{}, WorkerOptions{MaxWorkers: 1, QueueDepth: 1, MaxDuration: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	rec := validWorkerReceipt(t, "bad")
	rec.State = session.Running
	rec.Outcome = ""
	if err := worker.ScheduleTerminal(context.Background(), rec); err == nil {
		t.Fatal("nonterminal trace job accepted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = worker.Shutdown(ctx)
}

func validWorkerReceipt(t *testing.T, id string) receipt.Receipt {
	t.Helper()
	zero := 0
	rec := receipt.Receipt{SchemaVersion: 2, OperationID: id, SessionID: id + "-session", RequestFingerprint: "req", ExecutionFingerprint: "exec", DaemonIncarnation: "d", ExecutionMode: "shell", Executable: "/bin/sh", State: session.Completed, Outcome: session.Success, Shell: "/bin/sh", CWD: "/tmp", OutputComplete: true, StdinClosed: true, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}, Exit: receipt.ExitEvidence{Reaped: true, Code: &zero}}
	if err := rec.Validate(); err != nil {
		t.Fatal(err)
	}
	return rec
}
