package structuredresult

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

const (
	workerDerivationSchemaVersion = 1
	workerCapabilityVersion       = 1
	maxWorkerCount                = 16
	maxWorkerQueueDepth           = 1024
)

var (
	ErrWorkerQueueFull = errors.New("structured worker queue full")
	ErrWorkerClosed    = errors.New("structured worker closed")
)

type WorkerOptions struct {
	MaxWorkers int
	QueueDepth int
	Limits     Limits
}

type Worker struct {
	binder     InputBinder
	repository Repository
	index      OperationDerivationBinder
	reader     Reader
	adapters   map[string]Adapter
	limits     Limits
	configHash string
	jobs       chan workerJob
	mu         sync.Mutex
	closed     bool
	closeOnce  sync.Once
	wg         sync.WaitGroup
	done       chan struct{}
}

type workerJob struct {
	receipt receipt.Receipt
	adapter string
}

func NewWorker(binder InputBinder, repository Repository, reader Reader, adapters []Adapter, options WorkerOptions) (*Worker, error) {
	if binder == nil || repository == nil || reader == nil || options.MaxWorkers < 1 || options.MaxWorkers > maxWorkerCount || options.QueueDepth < 1 || options.QueueDepth > maxWorkerQueueDepth || options.Limits.Validate() != nil {
		return nil, fmt.Errorf("invalid structured worker options")
	}
	registry := make(map[string]Adapter, len(adapters))
	for _, adapter := range adapters {
		if adapter == nil || adapter.ID() == "" || adapter.Version() < 1 {
			return nil, fmt.Errorf("invalid structured adapter")
		}
		if _, exists := registry[adapter.ID()]; exists {
			return nil, fmt.Errorf("duplicate structured adapter")
		}
		registry[adapter.ID()] = adapter
	}
	if len(registry) == 0 {
		return nil, fmt.Errorf("structured adapter registry empty")
	}
	index, ok := repository.(OperationDerivationBinder)
	if !ok {
		return nil, fmt.Errorf("structured operation index unavailable")
	}
	configHash, err := workerConfigDigest(options.Limits)
	if err != nil {
		return nil, err
	}
	worker := &Worker{binder: binder, repository: repository, index: index, reader: reader, adapters: registry, limits: options.Limits, configHash: configHash, jobs: make(chan workerJob, options.QueueDepth), done: make(chan struct{})}
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

func (w *Worker) ScheduleTerminal(ctx context.Context, rec receipt.Receipt, adapter string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if w == nil || rec.Validate() != nil || !rec.State.Terminal() {
		return fmt.Errorf("invalid structured terminal job")
	}
	if _, ok := w.adapters[adapter]; !ok {
		return fmt.Errorf("unsupported structured adapter")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return ErrWorkerClosed
	}
	select {
	case w.jobs <- workerJob{receipt: rec, adapter: adapter}:
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
	for job := range w.jobs {
		w.process(job)
	}
}

func (w *Worker) process(job workerJob) {
	ctx, cancel := context.WithTimeout(context.Background(), w.limits.MaxDuration)
	defer cancel()
	ref, err := w.binder.BindTerminalOutput(ctx, job.receipt)
	if err != nil {
		return
	}
	adapter := w.adapters[job.adapter]
	producer := core.Producer{AdapterID: adapter.ID(), AdapterVersion: adapter.Version(), CapabilityVersion: workerCapabilityVersion}
	key, err := core.DerivationKeyForInputs([]core.StructuredInputRef{ref}, producer, workerDerivationSchemaVersion, w.configHash)
	if err != nil {
		return
	}
	service := New(w.repository)
	derivation, err := service.Begin(ctx, []core.StructuredInputRef{ref}, producer, workerDerivationSchemaVersion, w.configHash)
	if err != nil {
		derivation, err = w.repository.GetDerivation(ctx, key)
		if err != nil {
			return
		}
	}
	operationID, err := operation.ParseID(job.receipt.OperationID)
	if err != nil || w.index.BindOperationDerivation(ctx, operationID, key) != nil {
		return
	}
	if derivation.Lifecycle == core.LifecycleTerminal {
		return
	}
	if derivation.Lifecycle == core.LifecyclePending {
		derivation, err = service.MarkProcessing(ctx, key)
		if err != nil {
			return
		}
	}
	if derivation.Lifecycle != core.LifecycleProcessing {
		return
	}
	result, err := adapter.Parse(ctx, ref, w.reader, w.limits)
	if err != nil {
		return
	}
	if !job.receipt.OutputComplete && result.Outcome == core.ParseComplete {
		result.Outcome = core.ParsePartial
		result.Completeness = core.CompletenessPartial
	}
	if result.Outcome == "" || result.Completeness == "" {
		return
	}
	_, _ = service.Complete(ctx, key, result.Outcome, result.Completeness, result.Records)
}

func workerConfigDigest(limits Limits) (string, error) {
	data, err := json.Marshal(struct {
		SchemaVersion  int   `json:"schema_version"`
		MaxBytes       int64 `json:"max_bytes"`
		MaxRecords     int   `json:"max_records"`
		MaxStringBytes int   `json:"max_string_bytes"`
		MaxDepth       int   `json:"max_depth"`
		MaxDurationNS  int64 `json:"max_duration_ns"`
	}{1, limits.MaxBytes, limits.MaxRecords, limits.MaxStringBytes, limits.MaxDepth, int64(limits.MaxDuration)})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
