package inputtrace

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/inputtrace"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

const maxTraceWorkers = 16

var (
	ErrWorkerQueueFull = errors.New("input trace worker queue full")
	ErrWorkerClosed    = errors.New("input trace worker closed")
)

type TerminalMaterializer interface {
	MaterializeTerminal(context.Context, receipt.Receipt) (core.Record, error)
}

type WorkerOptions struct {
	MaxWorkers  int
	QueueDepth  int
	MaxDuration time.Duration
}

type Worker struct {
	materializer TerminalMaterializer
	maxDuration  time.Duration
	jobs         chan receipt.Receipt
	mu           sync.Mutex
	closed       bool
	closeOnce    sync.Once
	wg           sync.WaitGroup
	done         chan struct{}
}

func NewWorker(materializer TerminalMaterializer, options WorkerOptions) (*Worker, error) {
	if materializer == nil || options.MaxWorkers < 1 || options.MaxWorkers > maxTraceWorkers || options.QueueDepth < 1 || options.QueueDepth > core.WorkerQueueDepth || options.MaxDuration <= 0 || options.MaxDuration > core.MaxTraceCaptureDuration {
		return nil, fmt.Errorf("invalid input trace worker options")
	}
	worker := &Worker{materializer: materializer, maxDuration: options.MaxDuration, jobs: make(chan receipt.Receipt, options.QueueDepth), done: make(chan struct{})}
	worker.wg.Add(options.MaxWorkers)
	for range options.MaxWorkers {
		go worker.run()
	}
	go func() { worker.wg.Wait(); close(worker.done) }()
	return worker, nil
}

func (w *Worker) ScheduleTerminal(ctx context.Context, rec receipt.Receipt) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if w == nil || rec.Validate() != nil || !rec.State.Terminal() {
		return fmt.Errorf("invalid input trace terminal job")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return ErrWorkerClosed
	}
	select {
	case w.jobs <- rec:
		return nil
	default:
		return ErrWorkerQueueFull
	}
}

func (w *Worker) Shutdown(ctx context.Context) error {
	if w == nil {
		return nil
	}
	w.closeOnce.Do(func() { w.mu.Lock(); w.closed = true; close(w.jobs); w.mu.Unlock() })
	select {
	case <-w.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *Worker) run() {
	defer w.wg.Done()
	for rec := range w.jobs {
		ctx, cancel := context.WithTimeout(context.Background(), w.maxDuration)
		_, _ = w.materializer.MaterializeTerminal(ctx, rec)
		cancel()
	}
}
