package telemetry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/session"
	core "github.com/maemreyo/shellbeam/internal/core/telemetry"
)

func TestWorkerRetryAndRestartKeepOneLogicalTelemetrySample(t *testing.T) {
	now := time.Now().UTC()
	repo := telemetryFixture(now.Add(-time.Second), now, session.Completed, session.Success)
	repo.putCh = make(chan core.PerformanceRecord, 4)
	worker, err := NewWorker(repo, WorkerOptions{MaxWorkers: 1, QueueDepth: 4, MaxDuration: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.ScheduleTerminal(context.Background(), repo.receipt); err != nil {
		t.Fatal(err)
	}
	if err := worker.ScheduleTerminal(context.Background(), repo.receipt); err != nil {
		t.Fatal(err)
	}
	first := waitTelemetryPut(t, repo.putCh)
	second := waitTelemetryPut(t, repo.putCh)
	if first.DerivationKey != second.DerivationKey {
		t.Fatalf("retry keys differ: %q %q", first.DerivationKey, second.DerivationKey)
	}
	if err := worker.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	repo.mu.Lock()
	if len(repo.records) != 1 {
		t.Fatalf("logical samples after retry=%d", len(repo.records))
	}
	repo.mu.Unlock()

	restarted, err := NewWorker(repo, WorkerOptions{MaxWorkers: 1, QueueDepth: 1, MaxDuration: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.ScheduleTerminal(context.Background(), repo.receipt); err != nil {
		t.Fatal(err)
	}
	third := waitTelemetryPut(t, repo.putCh)
	if third.DerivationKey != first.DerivationKey {
		t.Fatalf("restart changed derivation key: %q -> %q", first.DerivationKey, third.DerivationKey)
	}
	if err := restarted.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if len(repo.records) != 1 {
		t.Fatalf("logical samples after restart=%d", len(repo.records))
	}
}

func TestWorkerQueueIsBoundedAndScheduleIsNonBlocking(t *testing.T) {
	now := time.Now().UTC()
	repo := telemetryFixture(now.Add(-time.Second), now, session.Completed, session.Success)
	release := make(chan struct{})
	repo.loadBlock = release
	repo.loadStarted = make(chan struct{})
	worker, err := NewWorker(repo, WorkerOptions{MaxWorkers: 1, QueueDepth: 1, MaxDuration: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.ScheduleTerminal(context.Background(), repo.receipt); err != nil {
		t.Fatal(err)
	}
	select {
	case <-repo.loadStarted:
	case <-time.After(time.Second):
		t.Fatal("worker did not start blocked load")
	}
	if err := worker.ScheduleTerminal(context.Background(), repo.receipt); err != nil {
		t.Fatalf("queue slot: %v", err)
	}
	started := time.Now()
	if err := worker.ScheduleTerminal(context.Background(), repo.receipt); !errors.Is(err, ErrWorkerQueueFull) {
		t.Fatalf("queue overflow err=%v", err)
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("queue overflow blocked for %v", elapsed)
	}
	close(release)
	if err := worker.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestWorkerRejectsInvalidClosedAndCancelledJobs(t *testing.T) {
	now := time.Now().UTC()
	repo := telemetryFixture(now.Add(-time.Second), now, session.Completed, session.Success)
	worker, err := NewWorker(repo, WorkerOptions{MaxWorkers: 1, QueueDepth: 1, MaxDuration: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	bad := repo.receipt
	bad.State = session.Running
	bad.Outcome = session.NoOutcome
	if err := worker.ScheduleTerminal(context.Background(), bad); err == nil {
		t.Fatal("non-terminal telemetry job accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := worker.ScheduleTerminal(ctx, repo.receipt); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled schedule err=%v", err)
	}
	if err := worker.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := worker.ScheduleTerminal(context.Background(), repo.receipt); !errors.Is(err, ErrWorkerClosed) {
		t.Fatalf("closed schedule err=%v", err)
	}
}

func TestNewWorkerRejectsUnboundedOptions(t *testing.T) {
	now := time.Now().UTC()
	repo := telemetryFixture(now.Add(-time.Second), now, session.Completed, session.Success)
	for _, options := range []WorkerOptions{
		{}, {MaxWorkers: 17, QueueDepth: 1, MaxDuration: time.Second},
		{MaxWorkers: 1, QueueDepth: 1025, MaxDuration: time.Second}, {MaxWorkers: 1, QueueDepth: 1, MaxDuration: 0},
		{MaxWorkers: 1, QueueDepth: 1, MaxDuration: 2 * time.Minute},
	} {
		if worker, err := NewWorker(repo, options); err == nil || worker != nil {
			t.Fatalf("invalid options accepted: %#v", options)
		}
	}
}

func waitTelemetryPut(t *testing.T, ch <-chan core.PerformanceRecord) core.PerformanceRecord {
	t.Helper()
	select {
	case record := <-ch:
		return record
	case <-time.After(time.Second):
		t.Fatal("telemetry worker did not persist record")
		return core.PerformanceRecord{}
	}
}
