package structuredresult

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

type workerBinderFake struct {
	ref   core.RawOutputRef
	calls int
}

func (b *workerBinderFake) BindTerminalOutput(context.Context, receipt.Receipt) (core.StructuredInputRef, error) {
	b.calls++
	return core.RawInputRef(b.ref), nil
}
func (b *workerBinderFake) ReadInputRange(context.Context, core.StructuredInputRef, int64, int) ([]byte, error) {
	return nil, nil
}

type workerReaderFake struct{}

func (workerReaderFake) ReadInputRange(context.Context, core.StructuredInputRef, int64, int) ([]byte, error) {
	return []byte(`{}`), nil
}
func (workerReaderFake) DescribeInput(context.Context, core.StructuredInputRef) (InputContext, error) {
	return InputContext{OperationID: "op-worker"}, nil
}

type workerRepo struct {
	mu          sync.Mutex
	derivations map[string]core.Derivation
	records     map[string][]core.Record
}

func newWorkerRepo() *workerRepo {
	return &workerRepo{derivations: map[string]core.Derivation{}, records: map[string][]core.Record{}}
}
func (r *workerRepo) BindOperationDerivation(_ context.Context, _ operation.ID, key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.derivations[key]; !ok {
		return errors.New("derivation missing")
	}
	return nil
}
func (r *workerRepo) PutDerivation(_ context.Context, d core.Derivation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cur, ok := r.derivations[d.DerivationKey]
	if !ok {
		r.derivations[d.DerivationKey] = d
		return nil
	}
	if reflect.DeepEqual(cur, d) {
		return nil
	}
	allowed := cur.Lifecycle == core.LifecyclePending && d.Lifecycle == core.LifecycleProcessing || cur.Lifecycle == core.LifecycleProcessing && d.Lifecycle == core.LifecycleTerminal
	if !allowed {
		return errors.New("derivation transition conflict")
	}
	r.derivations[d.DerivationKey] = d
	return nil
}
func (r *workerRepo) GetDerivation(_ context.Context, key string) (core.Derivation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.derivations[key]
	if !ok {
		return core.Derivation{}, errors.New("not found")
	}
	return d, nil
}
func (r *workerRepo) PutRecords(_ context.Context, key string, records []core.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records[key] = append([]core.Record(nil), records...)
	return nil
}
func (r *workerRepo) ListRecords(context.Context, string, RecordQuery) ([]core.Record, error) {
	return nil, nil
}
func (r *workerRepo) CompactRecords(context.Context, string) error { return nil }

type workerAdapter struct {
	id      string
	version int
	parse   func() (ParseResult, error)
}

func (a workerAdapter) ID() string   { return a.id }
func (a workerAdapter) Version() int { return a.version }
func (a workerAdapter) Parse(context.Context, core.StructuredInputRef, Reader, Limits) (ParseResult, error) {
	return a.parse()
}

func TestStructuredWorkerPublishesPendingProcessingTerminalAfterSchedule(t *testing.T) {
	ref := core.RawOutputRef{SessionID: "session-1", StartByte: 0, EndByte: 3, SHA256: strings.Repeat("a", 64)}
	binder := &workerBinderFake{ref: ref}
	repo := newWorkerRepo()
	adapter := workerAdapter{id: "go-test-json", version: 1, parse: func() (ParseResult, error) {
		return ParseResult{Outcome: core.ParseComplete, Completeness: core.CompletenessComplete}, nil
	}}
	worker, err := NewWorker(binder, repo, workerReaderFake{}, []Adapter{adapter}, WorkerOptions{MaxWorkers: 1, QueueDepth: 2, Limits: validWorkerLimits()})
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Shutdown(context.Background())
	rec := workerReceipt()
	if err := worker.ScheduleTerminal(context.Background(), rec, "go-test-json"); err != nil {
		t.Fatal(err)
	}
	waitWorkerState(t, repo, core.LifecycleTerminal)
	if binder.calls != 1 {
		t.Fatalf("bind calls=%d", binder.calls)
	}
}

func TestStructuredWorkerParserCrashLeavesProcessingAndRetryUsesSameKey(t *testing.T) {
	ref := core.RawOutputRef{SessionID: "session-1", StartByte: 0, EndByte: 3, SHA256: strings.Repeat("a", 64)}
	binder := &workerBinderFake{ref: ref}
	repo := newWorkerRepo()
	crash := true
	adapter := workerAdapter{id: "go-test-json", version: 1, parse: func() (ParseResult, error) {
		if crash {
			return ParseResult{}, errors.New("parser crash")
		}
		return ParseResult{Outcome: core.ParseComplete, Completeness: core.CompletenessComplete}, nil
	}}
	worker, err := NewWorker(binder, repo, workerReaderFake{}, []Adapter{adapter}, WorkerOptions{MaxWorkers: 1, QueueDepth: 2, Limits: validWorkerLimits()})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.ScheduleTerminal(context.Background(), workerReceipt(), "go-test-json"); err != nil {
		t.Fatal(err)
	}
	waitWorkerState(t, repo, core.LifecycleProcessing)
	worker.Shutdown(context.Background())
	firstKey := onlyWorkerKey(t, repo)
	crash = false
	worker, err = NewWorker(binder, repo, workerReaderFake{}, []Adapter{adapter}, WorkerOptions{MaxWorkers: 1, QueueDepth: 2, Limits: validWorkerLimits()})
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Shutdown(context.Background())
	if err := worker.ScheduleTerminal(context.Background(), workerReceipt(), "go-test-json"); err != nil {
		t.Fatal(err)
	}
	waitWorkerState(t, repo, core.LifecycleTerminal)
	if key := onlyWorkerKey(t, repo); key != firstKey {
		t.Fatalf("derivation key changed %s -> %s", firstKey, key)
	}
}

func TestStructuredWorkerDowngradesCompleteParseWhenTerminalOutputWasIncomplete(t *testing.T) {
	ref := core.RawOutputRef{SessionID: "session-1", StartByte: 0, EndByte: 3, SHA256: strings.Repeat("a", 64)}
	repo := newWorkerRepo()
	adapter := workerAdapter{id: "go-test-json", version: 1, parse: func() (ParseResult, error) {
		return ParseResult{Outcome: core.ParseComplete, Completeness: core.CompletenessComplete}, nil
	}}
	worker, err := NewWorker(&workerBinderFake{ref: ref}, repo, workerReaderFake{}, []Adapter{adapter}, WorkerOptions{MaxWorkers: 1, QueueDepth: 1, Limits: validWorkerLimits()})
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Shutdown(context.Background())
	rec := receipt.Receipt{SchemaVersion: 1, OperationID: "op-worker", SessionID: "session-1", Fingerprint: "fp", DaemonIncarnation: "d", State: session.Failed, Outcome: session.Failure, OutputBytes: 3, OutputComplete: false}
	if err := worker.ScheduleTerminal(context.Background(), rec, "go-test-json"); err != nil {
		t.Fatal(err)
	}
	waitWorkerState(t, repo, core.LifecycleTerminal)
	key := onlyWorkerKey(t, repo)
	repo.mu.Lock()
	derivation := repo.derivations[key]
	repo.mu.Unlock()
	if derivation.ParseOutcome != core.ParsePartial || derivation.Completeness != core.CompletenessPartial {
		t.Fatalf("derivation=%#v", derivation)
	}
}

func TestStructuredWorkerQueueIsBounded(t *testing.T) {
	ref := core.RawOutputRef{SessionID: "session-1", StartByte: 0, EndByte: 3, SHA256: strings.Repeat("a", 64)}
	blocker := make(chan struct{})
	started := make(chan struct{}, 1)
	adapter := workerAdapter{id: "go-test-json", version: 1, parse: func() (ParseResult, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-blocker
		return ParseResult{Outcome: core.ParseComplete, Completeness: core.CompletenessComplete}, nil
	}}
	worker, err := NewWorker(&workerBinderFake{ref: ref}, newWorkerRepo(), workerReaderFake{}, []Adapter{adapter}, WorkerOptions{MaxWorkers: 1, QueueDepth: 1, Limits: validWorkerLimits()})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.ScheduleTerminal(context.Background(), workerReceipt(), "go-test-json"); err != nil {
		t.Fatal(err)
	}
	// Wait until the sole worker consumed the first job, then fill the only queue slot.
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker did not begin parse")
	}
	if err := worker.ScheduleTerminal(context.Background(), workerReceipt(), "go-test-json"); err != nil {
		t.Fatal(err)
	}
	if err := worker.ScheduleTerminal(context.Background(), workerReceipt(), "go-test-json"); !errors.Is(err, ErrWorkerQueueFull) {
		t.Fatalf("queue error=%v", err)
	}
	close(blocker)
	worker.Shutdown(context.Background())
}

func workerReceipt() receipt.Receipt {
	code := 0
	return receipt.Receipt{SchemaVersion: 1, OperationID: "op-worker", SessionID: "session-1", Fingerprint: "fp", DaemonIncarnation: "d", State: session.Completed, Outcome: session.Success, OutputBytes: 3, OutputComplete: true, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}, Exit: receipt.ExitEvidence{Reaped: true, Code: &code}}
}
func validWorkerLimits() Limits {
	return Limits{MaxBytes: 1024, MaxRecords: 32, MaxStringBytes: 256, MaxDepth: 8, MaxDuration: time.Second}
}
func onlyWorkerKey(t *testing.T, r *workerRepo) string {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.derivations) != 1 {
		t.Fatalf("derivations=%#v", r.derivations)
	}
	for k := range r.derivations {
		return k
	}
	return ""
}
func waitWorkerState(t *testing.T, r *workerRepo, want core.Lifecycle) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		for _, d := range r.derivations {
			if d.Lifecycle == want {
				r.mu.Unlock()
				return
			}
		}
		r.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("state %s not reached", want)
}
