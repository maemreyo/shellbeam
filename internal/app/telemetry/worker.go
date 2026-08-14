package telemetry

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

const (
	maxWorkerCount      = 16
	maxWorkerQueueDepth = 1024
	maxWorkerDuration   = time.Minute
)

var (
	ErrWorkerQueueFull = errors.New("telemetry worker queue full")
	ErrWorkerClosed    = errors.New("telemetry worker closed")
)

type WorkerOptions struct {
	MaxWorkers  int
	QueueDepth  int
	MaxDuration time.Duration
}

type Worker struct {
	service     *Service
	maxDuration time.Duration
	jobs        chan receipt.Receipt
	mu          sync.Mutex
	closed      bool
	closeOnce   sync.Once
	wg          sync.WaitGroup
	done        chan struct{}
}

func NewWorker(repository Repository, options WorkerOptions) (*Worker, error) {
	if repository == nil {
		return nil, fmt.Errorf("invalid telemetry worker repository")
	}
	service, err := New(repository)
	if err != nil {
		return nil, err
	}
	return NewWorkerWithService(service, options)
}

func NewWorkerWithService(service *Service, options WorkerOptions) (*Worker, error) {
	if service == nil || service.repository == nil || options.MaxWorkers < 1 || options.MaxWorkers > maxWorkerCount || options.QueueDepth < 1 || options.QueueDepth > maxWorkerQueueDepth || options.MaxDuration <= 0 || options.MaxDuration > maxWorkerDuration {
		return nil, fmt.Errorf("invalid telemetry worker options")
	}
	worker := &Worker{service: service, maxDuration: options.MaxDuration, jobs: make(chan receipt.Receipt, options.QueueDepth), done: make(chan struct{})}
	worker.wg.Add(options.MaxWorkers)
	for range options.MaxWorkers {
		go worker.run()
	}
	go func() {
		worker.wg.Wait()
		close(worker.done)
	}()
	return worker, nil
}

func (w *Worker) ScheduleTerminal(ctx context.Context, rec receipt.Receipt) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if w == nil || rec.Validate() != nil || !rec.State.Terminal() {
		return fmt.Errorf("invalid telemetry terminal job")
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
	w.closeOnce.Do(func() {
		w.mu.Lock()
		w.closed = true
		close(w.jobs)
		w.mu.Unlock()
	})
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
		_, _ = w.service.DeriveTerminal(ctx, rec)
		cancel()
	}
}
