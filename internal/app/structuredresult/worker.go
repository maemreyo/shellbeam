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
	maxArtifactRecoveryCandidates = 256
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
	inflight   map[string]struct{}
	closeOnce  sync.Once
	wg         sync.WaitGroup
	done       chan struct{}
}

type workerJob struct {
	receipt          *receipt.Receipt
	input            *core.StructuredInputRef
	captureAuthority *CaptureAuthorityRecord
	adapter          string
	operationID      string
	outputComplete   bool
}

type ArtifactRecoveryCandidate struct {
	Ref              core.ArtifactBlobRef
	CaptureAuthority CaptureAuthorityRecord
}

type ArtifactRecoverySource interface {
	ListArtifactRecoveryCandidates(context.Context, int) ([]ArtifactRecoveryCandidate, error)
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
	worker := &Worker{
		binder: binder, repository: repository, index: index, reader: reader, adapters: registry,
		limits: options.Limits, configHash: configHash, jobs: make(chan workerJob, options.QueueDepth),
		inflight: map[string]struct{}{}, done: make(chan struct{}),
	}
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
	copy := rec
	return w.enqueue(workerJob{receipt: &copy, adapter: adapter, operationID: rec.OperationID, outputComplete: rec.OutputComplete})
}

func (w *Worker) ScheduleArtifact(ctx context.Context, ref core.ArtifactBlobRef, authority CaptureAuthorityRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if w == nil || ref.Validate() != nil || authority.Validate() != nil || authority.State != CaptureAuthorityPrepared {
		return fmt.Errorf("invalid structured artifact job")
	}
	intent := authority.Authority.Intent
	blobID, err := ArtifactBlobID(intent)
	if err != nil || blobID != ref.BlobID || ref.OperationID != intent.OperationID || ref.SessionID != intent.SessionID ||
		ref.RepositoryID != intent.RepositoryID || ref.WorkspaceID != intent.WorkspaceID || ref.DeclaredPath != intent.DeclaredPathToken ||
		ref.NormalizedWorkspacePath != intent.NormalizedWorkspacePath || intent.AdapterID == "" {
		return fmt.Errorf("structured artifact authority mismatch")
	}
	if _, ok := w.adapters[intent.AdapterID]; !ok {
		return fmt.Errorf("unsupported structured adapter")
	}
	input := core.ArtifactInputRef(ref)
	copyAuthority := authority
	return w.enqueue(workerJob{input: &input, captureAuthority: &copyAuthority, adapter: intent.AdapterID, operationID: intent.OperationID, outputComplete: true})
}

func (w *Worker) RecoverArtifacts(ctx context.Context, source ArtifactRecoverySource) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if w == nil || source == nil {
		return fmt.Errorf("structured artifact recovery unavailable")
	}
	candidates, err := source.ListArtifactRecoveryCandidates(ctx, maxArtifactRecoveryCandidates)
	if err != nil {
		return err
	}
	if len(candidates) > maxArtifactRecoveryCandidates {
		return fmt.Errorf("structured artifact recovery candidate limit exceeded")
	}
	for _, candidate := range candidates {
		if err := w.ScheduleArtifact(ctx, candidate.Ref, candidate.CaptureAuthority); err != nil {
			return err
		}
	}
	return nil
}

func (w *Worker) enqueue(job workerJob) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return ErrWorkerClosed
	}
	select {
	case w.jobs <- job:
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
	input := job.input
	if input == nil {
		if job.receipt == nil {
			return
		}
		bound, err := w.binder.BindTerminalOutput(ctx, *job.receipt)
		if err != nil {
			return
		}
		input = &bound
	}
	w.processInput(ctx, *input, job.adapter, job.operationID, job.outputComplete)
}

func (w *Worker) processInput(ctx context.Context, input core.StructuredInputRef, adapterID, operationID string, outputComplete bool) {
	adapter := w.adapters[adapterID]
	if adapter == nil || input.Validate() != nil {
		return
	}
	producer := core.Producer{AdapterID: adapter.ID(), AdapterVersion: adapter.Version(), CapabilityVersion: workerCapabilityVersion}
	key, err := workerDerivationKey(input, producer, w.configHash)
	if err != nil || !w.acquireInFlight(key) {
		return
	}
	defer w.releaseInFlight(key)

	service := New(w.repository)
	derivation, err := service.Begin(ctx, []core.StructuredInputRef{input}, producer, workerDerivationSchemaVersion, w.configHash)
	if err != nil {
		derivation, err = w.repository.GetDerivation(ctx, key)
		if err != nil {
			return
		}
	} else {
		// PutDerivation is idempotent. Re-read so a replay sees the canonical
		// lifecycle rather than the freshly constructed pending value.
		if current, getErr := w.repository.GetDerivation(ctx, key); getErr == nil {
			derivation = current
		}
	}
	opID, err := operation.ParseID(operationID)
	if err != nil || w.index.BindOperationDerivation(ctx, opID, key) != nil {
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
	result, err := adapter.Parse(ctx, input, w.reader, w.limits)
	if err != nil {
		return
	}
	if !outputComplete && result.Outcome == core.ParseComplete {
		result.Outcome = core.ParsePartial
		result.Completeness = core.CompletenessPartial
	}
	if result.Outcome == "" || result.Completeness == "" {
		return
	}
	_, _ = service.Complete(ctx, key, result.Outcome, result.Completeness, result.Records)
}

func (w *Worker) acquireInFlight(key string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, exists := w.inflight[key]; exists {
		return false
	}
	w.inflight[key] = struct{}{}
	return true
}

func (w *Worker) releaseInFlight(key string) {
	w.mu.Lock()
	delete(w.inflight, key)
	w.mu.Unlock()
}

func workerDerivationKey(input core.StructuredInputRef, producer core.Producer, configDigest string) (string, error) {
	return core.DerivationKeyForInputs([]core.StructuredInputRef{input}, producer, workerDerivationSchemaVersion, configDigest)
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
