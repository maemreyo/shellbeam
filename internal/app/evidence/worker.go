package evidence

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/evidence"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

var ErrWorkerQueueFull = errors.New("evidence worker queue full")

type TerminalDeriver interface {
	DeriveTerminal(context.Context, receipt.Receipt) (core.Record, bool, error)
}

type RecoveryRepository interface {
	ListEvidenceCandidates(context.Context, int) ([]operation.ID, error)
	FindOperation(context.Context, operation.ID) (operation.Reservation, bool, error)
	LoadSession(context.Context, operation.SessionID) (session.Snapshot, error)
	LoadReceipt(context.Context, operation.SessionID) (receipt.Receipt, error)
	FindEvidenceByOperation(context.Context, operation.ID) (core.Record, bool, error)
	ClearEvidenceCandidate(context.Context, operation.ID) error
}

type WorkerOptions struct {
	MaxWorkers    int
	QueueDepth    int
	MaxDuration   time.Duration
	RecoveryLimit int
}

type Worker struct {
	deriver  TerminalDeriver
	recovery RecoveryRepository
	options  WorkerOptions
	jobs     chan receipt.Receipt
	stop     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

func NewWorker(deriver TerminalDeriver, recovery RecoveryRepository, options WorkerOptions) (*Worker, error) {
	if deriver == nil || recovery == nil || options.MaxWorkers < 1 || options.QueueDepth < 1 || options.MaxDuration <= 0 || options.RecoveryLimit < 1 {
		return nil, fmt.Errorf("invalid evidence worker options")
	}
	worker := &Worker{deriver: deriver, recovery: recovery, options: options, jobs: make(chan receipt.Receipt, options.QueueDepth), stop: make(chan struct{})}
	for range options.MaxWorkers {
		worker.wg.Add(1)
		go worker.run()
	}
	return worker, nil
}

func (w *Worker) ScheduleTerminal(ctx context.Context, rec receipt.Receipt) error {
	if w == nil {
		return fmt.Errorf("evidence worker unavailable")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := rec.Validate(); err != nil {
		return err
	}
	if !rec.State.Terminal() {
		return fmt.Errorf("evidence requires terminal receipt")
	}
	select {
	case <-w.stop:
		return fmt.Errorf("evidence worker stopped")
	default:
	}
	select {
	case w.jobs <- rec:
		return nil
	case <-w.stop:
		return fmt.Errorf("evidence worker stopped")
	case <-ctx.Done():
		return ctx.Err()
	default:
		return ErrWorkerQueueFull
	}
}

func (w *Worker) Recover(ctx context.Context) error {
	if w == nil {
		return fmt.Errorf("evidence worker unavailable")
	}
	candidates, err := w.recovery.ListEvidenceCandidates(ctx, w.options.RecoveryLimit)
	if err != nil {
		return err
	}
	for _, id := range candidates {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, found, err := w.recovery.FindEvidenceByOperation(ctx, id); err != nil {
			return err
		} else if found {
			if err := w.recovery.ClearEvidenceCandidate(ctx, id); err != nil {
				return err
			}
			continue
		}
		reservation, found, err := w.recovery.FindOperation(ctx, id)
		if err != nil {
			return err
		}
		if !found || !reservation.EvidenceEligible() {
			if err := w.recovery.ClearEvidenceCandidate(ctx, id); err != nil {
				return err
			}
			continue
		}
		snapshot, err := w.recovery.LoadSession(ctx, reservation.SessionID)
		if err != nil {
			return err
		}
		if !snapshot.State.Terminal() {
			continue
		}
		rec, err := w.recovery.LoadReceipt(ctx, reservation.SessionID)
		if err != nil {
			return err
		}
		if rec.OperationID != string(reservation.OperationID) || rec.SessionID != string(reservation.SessionID) || !rec.State.Terminal() {
			return fmt.Errorf("evidence recovery authority mismatch")
		}
		if err := w.ScheduleTerminal(ctx, rec); err != nil {
			return err
		}
	}
	return nil
}

func (w *Worker) Shutdown(ctx context.Context) error {
	if w == nil {
		return nil
	}
	w.stopOnce.Do(func() { close(w.stop) })
	done := make(chan struct{})
	go func() { w.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *Worker) run() {
	defer w.wg.Done()
	for {
		select {
		case rec := <-w.jobs:
			w.process(rec)
		case <-w.stop:
			for {
				select {
				case rec := <-w.jobs:
					w.process(rec)
				default:
					return
				}
			}
		}
	}
}

func (w *Worker) process(rec receipt.Receipt) {
	ctx, cancel := context.WithTimeout(context.Background(), w.options.MaxDuration)
	defer cancel()
	if _, _, err := w.deriver.DeriveTerminal(ctx, rec); err != nil {
		return
	}
	id, err := operation.ParseID(rec.OperationID)
	if err != nil {
		return
	}
	_ = w.recovery.ClearEvidenceCandidate(context.Background(), id)
}
