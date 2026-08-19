package structuredresult

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

const (
	ArtifactSourceIdentityUnixV1 = "unix-stat-v1"
	MaxTerminalAcquireDuration   = 250 * time.Millisecond

	DefaultArtifactAcquisitionConcurrency = 2
	DefaultPinnedArtifactHandlesGlobal    = 4
	DefaultMaterializationQueueDepth      = 4
)

var (
	ErrArtifactSourceMissing        = errors.New("artifact_source_missing")
	ErrArtifactSourceKindMismatch   = errors.New("artifact_source_kind_mismatch")
	ErrArtifactSourceBudgetExceeded = errors.New("artifact_source_budget_exceeded")
	ErrArtifactCaptureUnavailable   = errors.New("artifact_capture_unavailable")
)

type ArtifactSourceIdentity struct {
	Scheme string
	Digest string
	Size   int64
}

func (i ArtifactSourceIdentity) Validate() error {
	if i.Scheme != ArtifactSourceIdentityUnixV1 || i.Size < 0 || len(i.Digest) != 64 {
		return fmt.Errorf("invalid artifact source identity")
	}
	if _, err := hex.DecodeString(i.Digest); err != nil {
		return fmt.Errorf("invalid artifact source identity digest")
	}
	return nil
}

type ArtifactSourceHandle interface {
	Read([]byte) (int, error)
	StatIdentity() (ArtifactSourceIdentity, error)
	Close() error
}

type ArtifactSourceOpener interface {
	OpenArtifactSource(context.Context, string, int64) (ArtifactSourceHandle, ArtifactSourceIdentity, error)
}

type BlobBudgetCapability interface {
	Release() error
}

type BlobBudgetCapabilityProvider interface {
	AcquireBlobBudgetCapability(context.Context, string, int64) (BlobBudgetCapability, error)
}

type TerminalCaptureLimits struct {
	AcquisitionConcurrency    int
	PinnedHandles             int
	MaterializationQueueDepth int
	MaxAcquireDuration        time.Duration
}

func DefaultTerminalCaptureLimits() TerminalCaptureLimits {
	return TerminalCaptureLimits{
		AcquisitionConcurrency:    DefaultArtifactAcquisitionConcurrency,
		PinnedHandles:             DefaultPinnedArtifactHandlesGlobal,
		MaterializationQueueDepth: DefaultMaterializationQueueDepth,
		MaxAcquireDuration:        MaxTerminalAcquireDuration,
	}
}

type TerminalCaptureRequest struct {
	CaptureAuthorityID string
	MaxBlobBytes       int64
	Opener             ArtifactSourceOpener
}

type TerminalCaptureState string

const (
	TerminalCaptureAcquired       TerminalCaptureState = "acquired"
	TerminalCaptureMissing        TerminalCaptureState = "missing"
	TerminalCaptureKindMismatch   TerminalCaptureState = "kind_mismatch"
	TerminalCaptureBudgetExceeded TerminalCaptureState = "budget_exceeded"
	TerminalCaptureUnavailable    TerminalCaptureState = "unavailable"
)

type TerminalCaptureResult struct {
	State              TerminalCaptureState
	CaptureAuthorityID string
	SourceIdentity     ArtifactSourceIdentity
	DiagnosticCode     string
	owner              *terminalCaptureOwnership
}

func (r TerminalCaptureResult) Source() ArtifactSourceHandle {
	if r.owner == nil || r.State != TerminalCaptureAcquired {
		return nil
	}
	r.owner.mu.Lock()
	defer r.owner.mu.Unlock()
	if r.owner.closed {
		return nil
	}
	return r.owner.source
}

func (r TerminalCaptureResult) BlobBudgetCapability() BlobBudgetCapability {
	if r.owner == nil || r.State != TerminalCaptureAcquired {
		return nil
	}
	r.owner.mu.Lock()
	defer r.owner.mu.Unlock()
	if r.owner.closed {
		return nil
	}
	return r.owner.budget
}

func (r TerminalCaptureResult) Close() error {
	if r.owner == nil {
		return nil
	}
	return r.owner.close()
}

type terminalCaptureOwnership struct {
	mu           sync.Mutex
	closed       bool
	source       ArtifactSourceHandle
	budget       BlobBudgetCapability
	releaseQueue func()
	releasePin   func()
}

func (o *terminalCaptureOwnership) close() error {
	if o == nil {
		return nil
	}
	o.mu.Lock()
	if o.closed {
		o.mu.Unlock()
		return nil
	}
	o.closed = true
	source, budget := o.source, o.budget
	releaseQueue, releasePin := o.releaseQueue, o.releasePin
	o.source, o.budget, o.releaseQueue, o.releasePin = nil, nil, nil, nil
	o.mu.Unlock()
	var err error
	if source != nil {
		err = errors.Join(err, source.Close())
	}
	if budget != nil {
		err = errors.Join(err, budget.Release())
	}
	if releaseQueue != nil {
		releaseQueue()
	}
	if releasePin != nil {
		releasePin()
	}
	return err
}

type terminalCaptureHooks struct {
	onAcquire func(string)
}

type TerminalCaptureAcquirer struct {
	limits      TerminalCaptureLimits
	budget      BlobBudgetCapabilityProvider
	workerSlots chan struct{}
	pinnedSlots chan struct{}
	queueSlots  chan struct{}
	hooks       terminalCaptureHooks
}

func NewTerminalCaptureAcquirer(limits TerminalCaptureLimits, budget BlobBudgetCapabilityProvider) *TerminalCaptureAcquirer {
	return newTerminalCaptureAcquirerWithHooks(limits, budget, terminalCaptureHooks{})
}

func newTerminalCaptureAcquirerWithHooks(limits TerminalCaptureLimits, budget BlobBudgetCapabilityProvider, hooks terminalCaptureHooks) *TerminalCaptureAcquirer {
	limits = normalizeTerminalCaptureLimits(limits)
	return &TerminalCaptureAcquirer{
		limits: limits, budget: budget, hooks: hooks,
		workerSlots: make(chan struct{}, limits.AcquisitionConcurrency),
		pinnedSlots: make(chan struct{}, limits.PinnedHandles),
		queueSlots:  make(chan struct{}, limits.MaterializationQueueDepth),
	}
}

func normalizeTerminalCaptureLimits(limits TerminalCaptureLimits) TerminalCaptureLimits {
	defaults := DefaultTerminalCaptureLimits()
	if limits.AcquisitionConcurrency < 1 {
		limits.AcquisitionConcurrency = defaults.AcquisitionConcurrency
	}
	if limits.PinnedHandles < 1 {
		limits.PinnedHandles = defaults.PinnedHandles
	}
	if limits.MaterializationQueueDepth < 1 || limits.MaterializationQueueDepth > limits.PinnedHandles {
		limits.MaterializationQueueDepth = min(defaults.MaterializationQueueDepth, limits.PinnedHandles)
	}
	if limits.MaxAcquireDuration <= 0 || limits.MaxAcquireDuration > MaxTerminalAcquireDuration {
		limits.MaxAcquireDuration = MaxTerminalAcquireDuration
	}
	return limits
}

func (a *TerminalCaptureAcquirer) Acquire(ctx context.Context, request TerminalCaptureRequest) TerminalCaptureResult {
	if err := validateTerminalCaptureRequest(request); err != nil || a == nil || a.budget == nil {
		return terminalCaptureUnavailable(request.CaptureAuthorityID, ErrArtifactCaptureUnavailable)
	}
	acquireCtx, cancel := context.WithTimeout(ctx, a.limits.MaxAcquireDuration)
	defer cancel()

	const (
		acquirePending int32 = iota
		acquireCallerOwned
		acquireTimedOut
	)
	var ownership atomic.Int32
	resultCh := make(chan TerminalCaptureResult, 1)
	go func() {
		result := a.acquire(acquireCtx, request)
		if ownership.CompareAndSwap(acquirePending, acquireCallerOwned) {
			resultCh <- result
			return
		}
		_ = result.Close()
	}()

	select {
	case result := <-resultCh:
		return result
	case <-acquireCtx.Done():
		if ownership.CompareAndSwap(acquirePending, acquireTimedOut) {
			return terminalCaptureUnavailable(request.CaptureAuthorityID, acquireCtx.Err())
		}
		// The helper committed ownership before the deadline became observable.
		// Its result is already final and cannot later be replaced.
		return <-resultCh
	}
}

func (a *TerminalCaptureAcquirer) acquire(ctx context.Context, request TerminalCaptureRequest) TerminalCaptureResult {
	if !acquireSlot(ctx, a.workerSlots) {
		return terminalCaptureUnavailable(request.CaptureAuthorityID, ctx.Err())
	}
	a.hook("worker")
	defer releaseSlot(a.workerSlots)

	if !tryAcquireSlot(a.pinnedSlots) {
		return terminalCaptureUnavailable(request.CaptureAuthorityID, ErrArtifactPathAuthorityCapacity)
	}
	a.hook("pinned")
	pinOwned := true
	defer func() {
		if pinOwned {
			releaseSlot(a.pinnedSlots)
		}
	}()

	if !tryAcquireSlot(a.queueSlots) {
		return terminalCaptureUnavailable(request.CaptureAuthorityID, ErrWorkerQueueFull)
	}
	a.hook("queue")
	queueOwned := true
	defer func() {
		if queueOwned {
			releaseSlot(a.queueSlots)
		}
	}()

	budget, err := a.budget.AcquireBlobBudgetCapability(ctx, request.CaptureAuthorityID, request.MaxBlobBytes)
	if err != nil || budget == nil {
		if errors.Is(err, ErrArtifactSourceBudgetExceeded) {
			return terminalCaptureFailure(request.CaptureAuthorityID, TerminalCaptureBudgetExceeded, "artifact_budget_exceeded")
		}
		return terminalCaptureUnavailable(request.CaptureAuthorityID, err)
	}
	budgetOwned := true
	defer func() {
		if budgetOwned {
			_ = budget.Release()
		}
	}()

	source, identity, err := request.Opener.OpenArtifactSource(ctx, request.CaptureAuthorityID, request.MaxBlobBytes)
	if err != nil {
		if source != nil {
			_ = source.Close()
		}
		return terminalCaptureFromSourceError(request.CaptureAuthorityID, err)
	}
	if source == nil || identity.Validate() != nil || identity.Size > request.MaxBlobBytes {
		if source != nil {
			_ = source.Close()
		}
		return terminalCaptureUnavailable(request.CaptureAuthorityID, ErrArtifactCaptureUnavailable)
	}

	owner := &terminalCaptureOwnership{
		source: source, budget: budget,
		releaseQueue: func() { releaseSlot(a.queueSlots) },
		releasePin:   func() { releaseSlot(a.pinnedSlots) },
	}
	pinOwned, queueOwned, budgetOwned = false, false, false
	return TerminalCaptureResult{
		State: TerminalCaptureAcquired, CaptureAuthorityID: request.CaptureAuthorityID,
		SourceIdentity: identity, owner: owner,
	}
}

func validateTerminalCaptureRequest(request TerminalCaptureRequest) error {
	if !validStructuredAuthorityDigest(request.CaptureAuthorityID) || request.MaxBlobBytes < 1 || request.MaxBlobBytes > MaxArtifactBlobBytes || request.Opener == nil {
		return fmt.Errorf("invalid terminal capture request")
	}
	return nil
}

func terminalCaptureFromSourceError(id string, err error) TerminalCaptureResult {
	switch {
	case errors.Is(err, ErrArtifactSourceMissing):
		return terminalCaptureFailure(id, TerminalCaptureMissing, "artifact_missing")
	case errors.Is(err, ErrArtifactSourceKindMismatch):
		return terminalCaptureFailure(id, TerminalCaptureKindMismatch, "artifact_kind_mismatch")
	case errors.Is(err, ErrArtifactSourceBudgetExceeded):
		return terminalCaptureFailure(id, TerminalCaptureBudgetExceeded, "artifact_budget_exceeded")
	default:
		return terminalCaptureUnavailable(id, err)
	}
}

func terminalCaptureUnavailable(id string, _ error) TerminalCaptureResult {
	return terminalCaptureFailure(id, TerminalCaptureUnavailable, "artifact_capture_unavailable")
}

func terminalCaptureFailure(id string, state TerminalCaptureState, code string) TerminalCaptureResult {
	return TerminalCaptureResult{State: state, CaptureAuthorityID: id, DiagnosticCode: code}
}

func acquireSlot(ctx context.Context, slots chan struct{}) bool {
	select {
	case slots <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

func tryAcquireSlot(slots chan struct{}) bool {
	select {
	case slots <- struct{}{}:
		return true
	default:
		return false
	}
}

func releaseSlot(slots chan struct{}) {
	select {
	case <-slots:
	default:
	}
}

func (a *TerminalCaptureAcquirer) hook(stage string) {
	if a.hooks.onAcquire != nil {
		a.hooks.onAcquire(stage)
	}
}

func (a *TerminalCaptureAcquirer) activeWorkers() int { return len(a.workerSlots) }
func (a *TerminalCaptureAcquirer) activePinned() int  { return len(a.pinnedSlots) }
func (a *TerminalCaptureAcquirer) activeQueued() int  { return len(a.queueSlots) }
