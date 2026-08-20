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
	id        string
	version   int
	parse     func() (ParseResult, error)
	parseWith func(context.Context, core.StructuredInputRef, Reader, Limits) (ParseResult, error)
}

func (a workerAdapter) ID() string   { return a.id }
func (a workerAdapter) Version() int { return a.version }
func (a workerAdapter) Parse(ctx context.Context, ref core.StructuredInputRef, reader Reader, limits Limits) (ParseResult, error) {
	if a.parseWith != nil {
		return a.parseWith(ctx, ref, reader, limits)
	}
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

func TestStructuredWorkerPersistsSemanticsCoverageAndDerivationContext(t *testing.T) {
	ref := core.RawOutputRef{SessionID: "session-1", StartByte: 0, EndByte: 3, SHA256: strings.Repeat("a", 64)}
	repo := newWorkerRepo()
	coverage := &core.ProducerSemanticsCoverage{Namespace: "pytest", VocabularyVersion: 1, Format: "junit-xml", Family: "xunit2", MechanicallyObservable: []string{"coarse:pass"}, Unavailable: []string{"pytest:xpass_exact"}}
	adapter := workerAdapter{id: "go-test-json", version: 1, parseWith: func(ctx context.Context, input core.StructuredInputRef, reader Reader, _ Limits) (ParseResult, error) {
		description, err := reader.DescribeInput(ctx, input)
		if err != nil || len(description.DerivationKey) != 64 {
			t.Fatalf("description=%#v err=%v", description, err)
		}
		return ParseResult{Outcome: core.ParseComplete, Completeness: core.CompletenessComplete, SemanticsCoverage: coverage}, nil
	}}
	worker, err := NewWorker(&workerBinderFake{ref: ref}, repo, workerReaderFake{}, []Adapter{adapter}, WorkerOptions{MaxWorkers: 1, QueueDepth: 1, Limits: validWorkerLimits()})
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Shutdown(context.Background())
	if err := worker.ScheduleTerminal(context.Background(), workerReceipt(), "go-test-json"); err != nil {
		t.Fatal(err)
	}
	waitWorkerState(t, repo, core.LifecycleTerminal)
	key := onlyWorkerKey(t, repo)
	repo.mu.Lock()
	derivation := repo.derivations[key]
	repo.mu.Unlock()
	if derivation.SemanticsCoverage == nil || !reflect.DeepEqual(*derivation.SemanticsCoverage, *coverage) {
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

type workerRecoverySource struct{ candidates []ArtifactRecoveryCandidate }

func (s workerRecoverySource) ListArtifactRecoveryCandidates(context.Context, int) ([]ArtifactRecoveryCandidate, error) {
	return append([]ArtifactRecoveryCandidate(nil), s.candidates...), nil
}

func workerArtifactAuthority(t *testing.T) CaptureAuthorityRecord {
	t.Helper()
	authority := materializerAuthority(t)
	record, err := NewCaptureAuthorityRecord(authority)
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func workerArtifactBlobRef(t *testing.T, record CaptureAuthorityRecord) core.ArtifactBlobRef {
	t.Helper()
	blobID, err := ArtifactBlobID(record.Authority.Intent)
	if err != nil {
		t.Fatal(err)
	}
	return core.ArtifactBlobRef{
		SchemaVersion: core.ArtifactBlobSchemaVersion, BlobID: blobID,
		OperationID: record.Authority.Intent.OperationID, SessionID: record.Authority.Intent.SessionID,
		RepositoryID: record.Authority.Intent.RepositoryID, WorkspaceID: record.Authority.Intent.WorkspaceID,
		DeclaredPath: record.Authority.Intent.DeclaredPathToken, NormalizedWorkspacePath: record.Authority.Intent.NormalizedWorkspacePath,
		SHA256: strings.Repeat("6", 64), Size: 2,
		TerminalCut:    core.TerminalCutV1{SchemaVersion: core.TerminalCutSchemaVersion, ReceiptSchemaVersion: 1, ReceiptDigest: strings.Repeat("7", 64)},
		ObservationCut: core.ObservationCutV1{SchemaVersion: core.ObservationCutSchemaVersion, Digest: strings.Repeat("8", 64)},
	}
}

func TestStructuredWorkerArtifactDuplicateScheduleRunsParserOnceAndRestartDoesNotRerunTerminal(t *testing.T) {
	repo := newWorkerRepo()
	authority := workerArtifactAuthority(t)
	ref := workerArtifactBlobRef(t, authority)
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	parseCalls := 0
	var parseMu sync.Mutex
	adapter := workerAdapter{id: PytestJUnitAdapterID, version: 1, parse: func() (ParseResult, error) {
		parseMu.Lock()
		parseCalls++
		parseMu.Unlock()
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
		return ParseResult{Outcome: core.ParseComplete, Completeness: core.CompletenessComplete}, nil
	}}
	worker, err := NewWorker(&workerBinderFake{}, repo, workerReaderFake{}, []Adapter{adapter}, WorkerOptions{MaxWorkers: 2, QueueDepth: 4, Limits: validWorkerLimits()})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.ScheduleArtifact(context.Background(), ref, authority); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("artifact parser did not start")
	}
	if err := worker.ScheduleArtifact(context.Background(), ref, authority); err != nil {
		t.Fatal(err)
	}
	close(release)
	waitWorkerState(t, repo, core.LifecycleTerminal)
	if err := worker.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	parseMu.Lock()
	firstCalls := parseCalls
	parseMu.Unlock()
	if firstCalls != 1 {
		t.Fatalf("duplicate artifact parse calls=%d", firstCalls)
	}
	firstKey := onlyWorkerKey(t, repo)

	adapter.parse = func() (ParseResult, error) {
		parseMu.Lock()
		parseCalls++
		parseMu.Unlock()
		return ParseResult{Outcome: core.ParseComplete, Completeness: core.CompletenessComplete}, nil
	}
	worker, err = NewWorker(&workerBinderFake{}, repo, workerReaderFake{}, []Adapter{adapter}, WorkerOptions{MaxWorkers: 1, QueueDepth: 2, Limits: validWorkerLimits()})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.ScheduleArtifact(context.Background(), ref, authority); err != nil {
		t.Fatal(err)
	}
	if err := worker.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	parseMu.Lock()
	gotCalls := parseCalls
	parseMu.Unlock()
	if gotCalls != 1 {
		t.Fatalf("terminal artifact reran parser calls=%d", gotCalls)
	}
	if got := onlyWorkerKey(t, repo); got != firstKey {
		t.Fatalf("artifact key changed %s -> %s", firstKey, got)
	}
}

func TestStructuredWorkerArtifactRecoveryResumesProcessingWithSameKey(t *testing.T) {
	repo := newWorkerRepo()
	authority := workerArtifactAuthority(t)
	ref := workerArtifactBlobRef(t, authority)
	crash := true
	parseCalls := 0
	adapter := workerAdapter{id: PytestJUnitAdapterID, version: 1, parse: func() (ParseResult, error) {
		parseCalls++
		if crash {
			return ParseResult{}, errors.New("artifact parser crash")
		}
		return ParseResult{Outcome: core.ParseComplete, Completeness: core.CompletenessComplete}, nil
	}}
	worker, err := NewWorker(&workerBinderFake{}, repo, workerReaderFake{}, []Adapter{adapter}, WorkerOptions{MaxWorkers: 1, QueueDepth: 2, Limits: validWorkerLimits()})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.ScheduleArtifact(context.Background(), ref, authority); err != nil {
		t.Fatal(err)
	}
	waitWorkerState(t, repo, core.LifecycleProcessing)
	if err := worker.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	firstKey := onlyWorkerKey(t, repo)
	crash = false
	worker, err = NewWorker(&workerBinderFake{}, repo, workerReaderFake{}, []Adapter{adapter}, WorkerOptions{MaxWorkers: 1, QueueDepth: 2, Limits: validWorkerLimits()})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.RecoverArtifacts(context.Background(), workerRecoverySource{candidates: []ArtifactRecoveryCandidate{{Ref: ref, CaptureAuthority: authority}}}); err != nil {
		t.Fatal(err)
	}
	if err := worker.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if key := onlyWorkerKey(t, repo); key != firstKey {
		t.Fatalf("recovery key changed %s -> %s", firstKey, key)
	}
	repo.mu.Lock()
	terminal := repo.derivations[firstKey]
	repo.mu.Unlock()
	if terminal.Lifecycle != core.LifecycleTerminal {
		t.Fatalf("recovery derivation=%#v", terminal)
	}
	if parseCalls != 2 {
		t.Fatalf("artifact recovery parse calls=%d", parseCalls)
	}
}

func TestStructuredWorkerArtifactIdentityBindsTerminalAndObservationCuts(t *testing.T) {
	authority := workerArtifactAuthority(t)
	ref := workerArtifactBlobRef(t, authority)
	producer := core.Producer{AdapterID: PytestJUnitAdapterID, AdapterVersion: 1, CapabilityVersion: workerCapabilityVersion}
	config := strings.Repeat("9", 64)
	first, err := workerDerivationKey(core.ArtifactInputRef(ref), producer, config)
	if err != nil {
		t.Fatal(err)
	}
	changedTerminal := ref
	changedTerminal.TerminalCut.ReceiptDigest = strings.Repeat("a", 64)
	second, err := workerDerivationKey(core.ArtifactInputRef(changedTerminal), producer, config)
	if err != nil || second == first {
		t.Fatalf("terminal cut not bound first=%s second=%s err=%v", first, second, err)
	}
	changedObservation := ref
	changedObservation.ObservationCut.Digest = strings.Repeat("b", 64)
	third, err := workerDerivationKey(core.ArtifactInputRef(changedObservation), producer, config)
	if err != nil || third == first {
		t.Fatalf("observation cut not bound first=%s third=%s err=%v", first, third, err)
	}
}

type artifactRecoverySourceMany struct{ candidates []ArtifactRecoveryCandidate }

func (s artifactRecoverySourceMany) ListArtifactRecoveryCandidates(context.Context, int) ([]ArtifactRecoveryCandidate, error) {
	return append([]ArtifactRecoveryCandidate(nil), s.candidates...), nil
}

func TestStructuredWorkerRecoveryBackpressureDoesNotFailStartup(t *testing.T) {
	repo := newWorkerRepo()
	adapter := workerAdapter{id: "pytest-junit-xml", version: 1, parse: func() (ParseResult, error) {
		return ParseResult{Outcome: core.ParseComplete, Completeness: core.CompletenessComplete}, nil
	}}
	worker, err := NewWorker(&workerBinderFake{}, repo, workerReaderFake{}, []Adapter{adapter}, WorkerOptions{MaxWorkers: 1, QueueDepth: 1, Limits: validWorkerLimits()})
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Shutdown(context.Background())
	authority := workerArtifactAuthority(t)
	ref := workerArtifactBlobRef(t, authority)
	var candidates []ArtifactRecoveryCandidate
	for i := 0; i < 6; i++ {
		candidates = append(candidates, ArtifactRecoveryCandidate{Ref: ref, CaptureAuthority: authority})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := worker.RecoverArtifacts(ctx, artifactRecoverySourceMany{candidates: candidates}); err != nil {
		t.Fatalf("startup recovery failed under queue backpressure: %v", err)
	}
}
