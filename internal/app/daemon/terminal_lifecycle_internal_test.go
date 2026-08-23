package daemon

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

// A session holds two kinds of resource, and they are given back at different
// moments. The child's descriptors are dead weight the instant it is reaped, so
// they go before the receipt is made durable -- publication retries a failing
// store indefinitely, and a dead child's stdin must not be held open for the
// duration. The live session itself is the opposite: it represents the session
// while it is finalizing, so it survives until the terminal receipt is durable,
// or a poll falling through to the store would find a terminal state with
// nothing behind it.

// LiveSessionCount reports how many sessions the daemon still represents in
// memory. It exists for tests: the live set is the thing terminal lifecycle
// handling is supposed to drain, and there is no other way to observe it.
func (s *Service) LiveSessionCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.live)
}

// LifecycleHandle is a process handle whose reaping and closing tests drive.
type LifecycleHandle struct {
	closes  atomic.Int32
	exit    chan receipt.ExitEvidence
	closed  chan struct{}
	oneShot sync.Once
	onClose func()
}

func NewLifecycleHandle() *LifecycleHandle {
	return &LifecycleHandle{exit: make(chan receipt.ExitEvidence, 1), closed: make(chan struct{})}
}

func (h *LifecycleHandle) Write([]byte) error { return nil }
func (h *LifecycleHandle) CloseStdin() error  { return nil }
func (h *LifecycleHandle) Signal(string) receipt.SignalEvidence {
	return receipt.SignalEvidence{Attempted: true, Succeeded: true}
}
func (h *LifecycleHandle) Wait(ctx context.Context) receipt.ExitEvidence {
	select {
	case e := <-h.exit:
		return e
	case <-ctx.Done():
		return receipt.ExitEvidence{}
	}
}
func (h *LifecycleHandle) Close() error {
	h.closes.Add(1)
	h.oneShot.Do(func() {
		if h.onClose != nil {
			h.onClose()
		}
		close(h.closed)
	})
	return nil
}

// Reap makes the child exit.
func (h *LifecycleHandle) Reap() {
	zero := 0
	h.exit <- receipt.ExitEvidence{Reaped: true, Code: &zero}
}

// Closed is signalled once the handle's resources have been released.
func (h *LifecycleHandle) Closed() <-chan struct{} { return h.closed }

// Closes counts how often release was attempted.
func (h *LifecycleHandle) Closes() int32 { return h.closes.Load() }

// LifecycleOwner hands out one prepared handle.
type LifecycleOwner struct{ Handle *LifecycleHandle }

func (o *LifecycleOwner) Start(context.Context, operation.ExecutionSpec, OutputSink) (ProcessHandle, receipt.SpawnEvidence, error) {
	return o.Handle, receipt.SpawnEvidence{Attempted: true, Succeeded: true}, nil
}

// SetOwner swaps the process owner between sessions.
func (s *Service) SetOwner(owner ProcessOwner) { s.owner = owner }

// TestPersistentSessionsAreNotEvictedByOrdinaryTerminalHandling: a persistent
// session stays addressable after a terminal receipt, because its lifecycle
// continues past it.
func TestPersistentSessionsAreNotEvictedByOrdinaryTerminalHandling(t *testing.T) {
	s := &Service{live: map[string]*liveSession{}}
	live := &liveSession{sessionID: "persistent-session", persistent: true, state: session.Completed}
	s.put(live)
	s.evictTerminated(live)
	if s.LiveSessionCount() != 1 {
		t.Fatal("a persistent session was evicted by ordinary terminal handling")
	}

	ordinary := &liveSession{sessionID: "ordinary-session", state: session.Completed}
	s.put(ordinary)
	s.evictTerminated(ordinary)
	if s.get("ordinary-session") != nil {
		t.Fatal("an ordinary terminal session was left live")
	}
	if s.get("persistent-session") == nil {
		t.Fatal("evicting an ordinary session removed the persistent one")
	}
}

// TestReleasingProcessResourcesIsIdempotentAndSkipsPersistent.
func TestReleasingProcessResourcesIsIdempotentAndSkipsPersistent(t *testing.T) {
	s := &Service{live: map[string]*liveSession{}}
	handle := NewLifecycleHandle()
	ordinary := &liveSession{sessionID: "ordinary", handle: handle}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); s.releaseProcessResources(ordinary) }()
	}
	wg.Wait()
	if handle.Closes() < 1 {
		t.Fatal("process resources were never released")
	}

	persistentHandle := NewLifecycleHandle()
	persistent := &liveSession{sessionID: "persistent", handle: persistentHandle, persistent: true}
	s.releaseProcessResources(persistent)
	if persistentHandle.Closes() != 0 {
		t.Fatal("a persistent session's handle was closed by ordinary terminal handling")
	}
}

// TestReleasingWithoutAHandleIsSafe covers the spawn-failure shape, which never
// had one.
func TestReleasingWithoutAHandleIsSafe(t *testing.T) {
	s := &Service{live: map[string]*liveSession{}}
	s.releaseProcessResources(&liveSession{sessionID: "never-spawned"})
}

type lifecycleManagedLease struct {
	mu    sync.Mutex
	order *[]string
	ended bool
}

func (l *lifecycleManagedLease) End() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ended = true
	if l.order != nil {
		*l.order = append(*l.order, "lease")
	}
}

func (l *lifecycleManagedLease) Ended() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.ended
}

type lifecycleCaptureTerminal struct {
	mu            sync.Mutex
	order         *[]string
	acquireCalls  int
	scheduleCalls int
	block         chan struct{}
	lateReturned  chan struct{}
}

func (c *lifecycleCaptureTerminal) AcquireTerminal(_ context.Context, reservation operation.Reservation) structuredapp.TerminalCaptureResult {
	c.mu.Lock()
	c.acquireCalls++
	if c.order != nil {
		*c.order = append(*c.order, "capture")
	}
	c.mu.Unlock()
	if c.block != nil {
		<-c.block // deliberately ignores context to verify daemon-side deadline
	}
	if c.lateReturned != nil {
		close(c.lateReturned)
	}
	return structuredapp.TerminalCaptureResult{
		State:              structuredapp.TerminalCaptureUnavailable,
		CaptureAuthorityID: reservation.StructuredCaptureDigest,
		DiagnosticCode:     "artifact_capture_unavailable",
	}
}

func (c *lifecycleCaptureTerminal) ScheduleTerminal(_ context.Context, _ receipt.Receipt, result structuredapp.TerminalCaptureResult) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.scheduleCalls++
	if c.order != nil {
		*c.order = append(*c.order, "schedule")
	}
	if result.State == "" {
		return errors.New("empty terminal capture result")
	}
	return nil
}

func (c *lifecycleCaptureTerminal) counts() (int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.acquireCalls, c.scheduleCalls
}

type lifecycleTerminalStore struct {
	mu      sync.Mutex
	order   *[]string
	receipt receipt.Receipt
}

func (s *lifecycleTerminalStore) ReserveOperation(_ context.Context, v operation.Reservation) (operation.Reservation, bool, StoreResult) {
	return v, true, StoreResult{}
}
func (s *lifecycleTerminalStore) LoadOperation(context.Context, operation.ID) (operation.Reservation, error) {
	return operation.Reservation{}, errors.New("not found")
}
func (s *lifecycleTerminalStore) FindOperation(context.Context, operation.ID) (operation.Reservation, bool, error) {
	return operation.Reservation{}, false, nil
}
func (s *lifecycleTerminalStore) ReserveTypedIntent(_ context.Context, v operation.TypedIntentClaim) (operation.TypedIntentClaim, bool, StoreResult) {
	return v, true, StoreResult{}
}
func (s *lifecycleTerminalStore) FindTypedIntent(context.Context, operation.ID) (operation.TypedIntentClaim, bool, error) {
	return operation.TypedIntentClaim{}, false, nil
}
func (s *lifecycleTerminalStore) CommitTypedBinding(_ context.Context, _ operation.ID, v operation.Reservation) (operation.Reservation, bool, StoreResult) {
	return v, true, StoreResult{}
}
func (s *lifecycleTerminalStore) LoadSession(context.Context, operation.SessionID) (session.Snapshot, error) {
	return session.Snapshot{}, errors.New("not found")
}
func (s *lifecycleTerminalStore) AdvanceSession(context.Context, session.Snapshot) StoreResult {
	return StoreResult{}
}
func (s *lifecycleTerminalStore) PublishTerminal(_ context.Context, rec receipt.Receipt) StoreResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.order != nil {
		*s.order = append(*s.order, "publish")
	}
	s.receipt = rec
	return StoreResult{}
}
func (s *lifecycleTerminalStore) LoadReceipt(context.Context, operation.SessionID) (receipt.Receipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.receipt.SessionID == "" {
		return receipt.Receipt{}, errors.New("not found")
	}
	return s.receipt, nil
}
func (s *lifecycleTerminalStore) AppendOutput(_ context.Context, _ operation.SessionID, data []byte) (int, StoreResult) {
	return len(data), StoreResult{}
}
func (s *lifecycleTerminalStore) ReadOutput(context.Context, operation.SessionID, int64, int) ([]byte, int64, error) {
	return nil, 0, nil
}
func (s *lifecycleTerminalStore) Compact(context.Context, operation.SessionID) StoreResult {
	return StoreResult{}
}

func lifecycleCaptureLive(digest, adapter string, handle ProcessHandle, lease ManagedShellLease) *liveSession {
	writerDone := make(chan struct{})
	close(writerDone)
	return &liveSession{
		operationID: "capture-terminal-op", sessionID: "capture-terminal-session",
		reservation: operation.Reservation{
			SchemaVersion: 2, OperationID: "capture-terminal-op", SessionID: "capture-terminal-session",
			ExecutionMode: operation.ExecutionModeArgv, Executable: "pytest", CWD: "/tmp",
			StructuredAdapter: adapter, StructuredCaptureDigest: digest,
		},
		spec:  operation.ExecutionSpec{Mode: operation.ExecutionModeArgv, Executable: "pytest", CWD: "/tmp"},
		state: session.Running, handle: handle, spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true},
		jobs: make(chan inputJob), writerDone: writerDone, done: make(chan struct{}), changed: make(chan struct{}), coherenceLease: lease,
	}
}

func TestArtifactTerminalCapturePrecedesLeaseProcessAndReceiptAndSchedulesAfter(t *testing.T) {
	var order []string
	handle := NewLifecycleHandle()
	handle.onClose = func() { order = append(order, "process") }
	capture := &lifecycleCaptureTerminal{order: &order}
	store := &lifecycleTerminalStore{order: &order}
	svc := NewService(store, nil, Options{Incarnation: "capture-order", StructuredCaptureTerminal: capture})
	lease := &lifecycleManagedLease{order: &order}
	live := lifecycleCaptureLive(strings.Repeat("a", 64), "pytest-junit-xml", handle, lease)
	svc.put(live)
	go svc.waitLoop(live)
	handle.Reap()
	select {
	case <-live.done:
	case <-time.After(5 * time.Second):
		t.Fatal("terminal finalizer did not finish")
	}
	if got := strings.Join(order, ","); got != "capture,lease,process,publish,schedule" {
		t.Fatalf("terminal order=%q", got)
	}
	if acquire, schedule := capture.counts(); acquire != 1 || schedule != 1 {
		t.Fatalf("capture calls acquire=%d schedule=%d", acquire, schedule)
	}
}

func TestArtifactAdmittedStartFailureCapturesBeforeReleaseAndSchedulesAfterReceipt(t *testing.T) {
	var order []string
	handle := NewLifecycleHandle()
	handle.onClose = func() { order = append(order, "process") }
	capture := &lifecycleCaptureTerminal{order: &order}
	store := &lifecycleTerminalStore{order: &order}
	svc := NewService(store, nil, Options{Incarnation: "capture-spawn-failure", StructuredCaptureTerminal: capture})
	lease := &lifecycleManagedLease{order: &order}
	live := lifecycleCaptureLive(strings.Repeat("c", 64), "pytest-junit-xml", handle, lease)
	live.spawn = receipt.SpawnEvidence{Attempted: true, Succeeded: false, ErrorCode: "spawn_failed"}
	svc.put(live)
	svc.finalizeAdmittedStartFailure(live, "spawn_failed")

	if got := strings.Join(order, ","); got != "capture,lease,process,publish,schedule" {
		t.Fatalf("spawn failure terminal order=%q", got)
	}
	if acquire, schedule := capture.counts(); acquire != 1 || schedule != 1 {
		t.Fatalf("spawn failure capture calls acquire=%d schedule=%d", acquire, schedule)
	}
	store.mu.Lock()
	gotReceipt := store.receipt
	store.mu.Unlock()
	if gotReceipt.State != session.Failed || gotReceipt.Outcome != session.Failure || gotReceipt.FailureReason != "spawn_failed" || gotReceipt.Spawn.Succeeded {
		t.Fatalf("capture rewrote spawn failure receipt truth: %#v", gotReceipt)
	}
}

func TestArtifactTerminalCaptureDeadlineCannotDelayReceiptTruth(t *testing.T) {
	handle := NewLifecycleHandle()
	block := make(chan struct{})
	late := make(chan struct{})
	capture := &lifecycleCaptureTerminal{block: block, lateReturned: late}
	store := &lifecycleTerminalStore{}
	svc := NewService(store, nil, Options{Incarnation: "capture-timeout", StructuredCaptureTerminal: capture})
	live := lifecycleCaptureLive(strings.Repeat("b", 64), "pytest-junit-xml", handle, nil)
	svc.put(live)
	start := time.Now()
	go svc.waitLoop(live)
	handle.Reap()
	select {
	case <-live.done:
	case <-time.After(750 * time.Millisecond):
		t.Fatal("terminal receipt waited too long for blocked capture owner")
	}
	if elapsed := time.Since(start); elapsed > 750*time.Millisecond {
		t.Fatalf("terminal receipt waited %s for blocked capture owner", elapsed)
	}
	if _, schedule := capture.counts(); schedule != 1 {
		t.Fatalf("timeout result schedule calls=%d", schedule)
	}
	close(block)
	select {
	case <-late:
	case <-time.After(time.Second):
		t.Fatal("late capture owner did not return")
	}
	if _, schedule := capture.counts(); schedule != 1 {
		t.Fatalf("late result resurrected scheduling: %d", schedule)
	}
}

func TestRawStructuredTerminalSkipsArtifactPhaseA(t *testing.T) {
	capture := &lifecycleCaptureTerminal{}
	svc := &Service{options: Options{StructuredCaptureTerminal: capture}}
	reservation := operation.Reservation{StructuredAdapter: "go-test-json"}
	result := svc.acquireStructuredCaptureTerminal(reservation)
	if result.State != "" {
		t.Fatalf("raw reservation produced artifact result=%#v", result)
	}
	if acquire, schedule := capture.counts(); acquire != 0 || schedule != 0 {
		t.Fatalf("raw reservation touched artifact owner acquire=%d schedule=%d", acquire, schedule)
	}
}
