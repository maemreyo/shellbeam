package daemon_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	traceapp "github.com/maemreyo/shellbeam/internal/app/inputtrace"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	trace "github.com/maemreyo/shellbeam/internal/core/inputtrace"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

type daemonTracePreparer struct {
	calls    atomic.Int32
	prepared traceapp.Prepared
	err      error
	panic    bool
}

func (p *daemonTracePreparer) Prepare(context.Context, traceapp.PrepareRequest) (traceapp.Prepared, error) {
	if p.panic {
		panic("E27 no-tax preparer reached")
	}
	p.calls.Add(1)
	return p.prepared, p.err
}

type daemonTracePrepared struct {
	binding trace.InstrumentationBinding
	env     []operation.EnvironmentEntry
	aborts  atomic.Int32
}

func (p *daemonTracePrepared) Binding() trace.InstrumentationBinding { return p.binding }
func (p *daemonTracePrepared) EnvironmentAdditions() []operation.EnvironmentEntry {
	return append([]operation.EnvironmentEntry(nil), p.env...)
}
func (p *daemonTracePrepared) Abort() error { p.aborts.Add(1); return nil }

type daemonTraceOwner struct {
	starts atomic.Int32
	mu     sync.Mutex
	specs  []operation.ExecutionSpec
}

func (o *daemonTraceOwner) Start(_ context.Context, spec operation.ExecutionSpec, _ app.OutputSink) (app.ProcessHandle, receipt.SpawnEvidence, error) {
	o.starts.Add(1)
	o.mu.Lock()
	o.specs = append(o.specs, spec)
	o.mu.Unlock()
	return fakeHandle{}, receipt.SpawnEvidence{Attempted: true, Succeeded: true}, nil
}

func (o *daemonTraceOwner) lastSpec() operation.ExecutionSpec {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.specs) == 0 {
		return operation.ExecutionSpec{}
	}
	return o.specs[len(o.specs)-1]
}

func TestE27InputTraceOffAndOmittedHaveZeroProviderWork(t *testing.T) {
	for _, mode := range []trace.Mode{"", trace.ModeOff} {
		t.Run(string(mode), func(t *testing.T) {
			store := openE27DaemonStore(t)
			owner := &daemonTraceOwner{}
			preparer := &daemonTracePreparer{panic: true}
			svc := app.NewService(store, owner, app.Options{Incarnation: "e27-off", Shell: "/bin/sh", MaxQueuedInputBytes: 100, InputTracePreparer: preparer})
			view, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "e27-off-" + string(mode), Command: "true", CWD: "/", YieldMS: 100, TraceMode: mode})
			if err != nil {
				t.Fatal(err)
			}
			waitForTerminal(t, svc, view.SessionID)
			if owner.starts.Load() != 1 || preparer.calls.Load() != 0 || len(owner.lastSpec().EnvironmentAdditions) != 0 {
				t.Fatalf("starts=%d calls=%d spec=%#v", owner.starts.Load(), preparer.calls.Load(), owner.lastSpec())
			}
		})
	}
}

func TestE27InputTraceExactReplayDoesNotPrepareTwice(t *testing.T) {
	store := openE27DaemonStore(t)
	prepared := e27DaemonPrepared()
	preparer := &daemonTracePreparer{prepared: prepared}
	owner := &daemonTraceOwner{}
	svc := app.NewService(store, owner, app.Options{Incarnation: "e27-replay", Shell: "/bin/sh", MaxQueuedInputBytes: 100, InputTracePreparer: preparer})
	req := app.StartRequest{ProtocolVersion: 2, OperationID: "e27-replay", Command: "true", CWD: "/", YieldMS: 100, TraceMode: trace.ModeBestEffort}
	first, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	waitForTerminal(t, svc, first.SessionID)
	second, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if second.SessionID != first.SessionID || preparer.calls.Load() != 1 || owner.starts.Load() != 1 || prepared.aborts.Load() != 0 {
		t.Fatalf("first=%#v second=%#v prepare=%d starts=%d aborts=%d", first, second, preparer.calls.Load(), owner.starts.Load(), prepared.aborts.Load())
	}
}

func TestE27InputTraceRequiredUnavailableAndStartupBudgetFailBeforeSpawn(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want failure.Code
	}{
		{"unavailable", errors.New("provider missing"), failure.InputTraceRequiredUnavailable},
		{"startup-budget", failure.New(failure.InputTraceStartupBudgetExceeded, map[string]string{"provider": "test", "limit_ms": "2000", "reason": "timeout"}, nil), failure.InputTraceStartupBudgetExceeded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			owner := &daemonTraceOwner{}
			preparer := &daemonTracePreparer{err: tc.err}
			svc := app.NewService(openE27DaemonStore(t), owner, app.Options{Incarnation: "e27-required", Shell: "/bin/sh", MaxQueuedInputBytes: 100, InputTracePreparer: preparer})
			_, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "e27-required-" + tc.name, Command: "true", CWD: "/", TraceMode: trace.ModeRequired})
			if got := failure.Public(err).Code; got != tc.want {
				t.Fatalf("code=%q err=%v", got, err)
			}
			if owner.starts.Load() != 0 || preparer.calls.Load() != 1 {
				t.Fatalf("starts=%d prepare=%d", owner.starts.Load(), preparer.calls.Load())
			}
		})
	}
}

func TestE27InputTraceBestEffortPrepareFailureRunsUntraced(t *testing.T) {
	store := openE27DaemonStore(t)
	owner := &daemonTraceOwner{}
	preparer := &daemonTracePreparer{err: errors.New("provider unavailable")}
	svc := app.NewService(store, owner, app.Options{Incarnation: "e27-best-fail", Shell: "/bin/sh", MaxQueuedInputBytes: 100, InputTracePreparer: preparer})
	view, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "e27-best-fail", Command: "true", CWD: "/", YieldMS: 100, TraceMode: trace.ModeBestEffort})
	if err != nil {
		t.Fatal(err)
	}
	waitForTerminal(t, svc, view.SessionID)
	stored, err := store.LoadOperation(context.Background(), "e27-best-fail")
	if err != nil {
		t.Fatal(err)
	}
	if preparer.calls.Load() != 1 || owner.starts.Load() != 1 || stored.Trace != nil || len(owner.lastSpec().EnvironmentAdditions) != 0 {
		t.Fatalf("prepare=%d starts=%d trace=%#v spec=%#v", preparer.calls.Load(), owner.starts.Load(), stored.Trace, owner.lastSpec())
	}
	plainStore := openE27DaemonStore(t)
	plainSvc := app.NewService(plainStore, &daemonTraceOwner{}, app.Options{Incarnation: "e27-best-plain", Shell: "/bin/sh", MaxQueuedInputBytes: 100})
	plainView, err := plainSvc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "e27-best-plain", Command: "true", CWD: "/", YieldMS: 100})
	if err != nil {
		t.Fatal(err)
	}
	waitForTerminal(t, plainSvc, plainView.SessionID)
	plain, err := plainStore.LoadOperation(context.Background(), "e27-best-plain")
	if err != nil || stored.ExecutionFingerprint != plain.ExecutionFingerprint {
		t.Fatalf("untraced execution fp=%q want=%q err=%v", stored.ExecutionFingerprint, plain.ExecutionFingerprint, err)
	}
}

func TestE27InputTraceActiveFreezesBindingAndEnvironmentBeforeSpawn(t *testing.T) {
	store := openE27DaemonStore(t)
	prepared := e27DaemonPrepared()
	preparer := &daemonTracePreparer{prepared: prepared}
	owner := &daemonTraceOwner{}
	svc := app.NewService(store, owner, app.Options{Incarnation: "e27-active", Shell: "/bin/sh", MaxQueuedInputBytes: 100, InputTracePreparer: preparer})
	view, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "e27-active", Command: "true", CWD: "/", YieldMS: 100, TraceMode: trace.ModeBestEffort})
	if err != nil {
		t.Fatal(err)
	}
	waitForTerminal(t, svc, view.SessionID)
	stored, err := store.LoadOperation(context.Background(), "e27-active")
	if err != nil {
		t.Fatal(err)
	}
	spec := owner.lastSpec()
	if stored.Trace == nil || stored.Trace.TraceID != prepared.binding.TraceID || len(spec.EnvironmentAdditions) != 2 || spec.EnvironmentAdditions[0].Key != "SHELLBEAM_TRACE_ID" {
		t.Fatalf("stored=%#v spec=%#v", stored.Trace, spec)
	}
	plainStore := openE27DaemonStore(t)
	plainSvc := app.NewService(plainStore, &daemonTraceOwner{}, app.Options{Incarnation: "e27-active-plain", Shell: "/bin/sh", MaxQueuedInputBytes: 100})
	plainView, err := plainSvc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "e27-active-plain", Command: "true", CWD: "/", YieldMS: 100})
	if err != nil {
		t.Fatal(err)
	}
	waitForTerminal(t, plainSvc, plainView.SessionID)
	plain, err := plainStore.LoadOperation(context.Background(), "e27-active-plain")
	if err != nil {
		t.Fatal(err)
	}
	if stored.ExecutionFingerprint == plain.ExecutionFingerprint {
		t.Fatal("active instrumentation did not change execution identity")
	}
}

type e27FailReserveStore struct{ *storeadapter.Repository }

func (s *e27FailReserveStore) ReserveOperation(context.Context, operation.Reservation) (operation.Reservation, bool, app.StoreResult) {
	return operation.Reservation{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: failure.New(failure.PersistenceUnavailable, nil, nil)}
}

func TestE27InputTraceReservationFailureAbortsPreparedProvider(t *testing.T) {
	prepared := e27DaemonPrepared()
	preparer := &daemonTracePreparer{prepared: prepared}
	owner := &daemonTraceOwner{}
	store := &e27FailReserveStore{Repository: openE27DaemonStore(t)}
	svc := app.NewService(store, owner, app.Options{Incarnation: "e27-abort", Shell: "/bin/sh", MaxQueuedInputBytes: 100, InputTracePreparer: preparer})
	_, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "e27-abort", Command: "true", CWD: "/", TraceMode: trace.ModeBestEffort})
	if !errors.Is(err, failure.PersistenceUnavailable) {
		t.Fatalf("err=%v", err)
	}
	if prepared.aborts.Load() != 1 || owner.starts.Load() != 0 {
		t.Fatalf("aborts=%d starts=%d", prepared.aborts.Load(), owner.starts.Load())
	}
}

func TestE27InputTraceTTYAndPersistentAreExplicitUnsupportedButOffUnchanged(t *testing.T) {
	for _, req := range []app.StartRequest{
		{ProtocolVersion: 2, OperationID: "e27-tty", Command: "true", CWD: "/", TTY: true, TraceMode: trace.ModeBestEffort},
		{ProtocolVersion: 2, OperationID: "e27-persistent", Command: "true", CWD: "/", Persistent: true, SessionName: "dev", TraceMode: trace.ModeBestEffort},
	} {
		preparer := &daemonTracePreparer{panic: true}
		owner := &daemonTraceOwner{}
		svc := app.NewService(openE27DaemonStore(t), owner, app.Options{Incarnation: "e27-unsupported", Shell: "/bin/sh", MaxQueuedInputBytes: 100, InputTracePreparer: preparer})
		_, err := svc.Start(context.Background(), req)
		if !errors.Is(err, failure.InputTraceUnsupported) || owner.starts.Load() != 0 || preparer.calls.Load() != 0 {
			t.Fatalf("request=%#v err=%v starts=%d calls=%d", req, err, owner.starts.Load(), preparer.calls.Load())
		}
	}
}

func TestE27InputTraceNoTaxRawTypedPersistentOffNeverCallsPreparer(t *testing.T) {
	preparer := &daemonTracePreparer{panic: true}
	// Raw direct path.
	rawStore := openE27DaemonStore(t)
	rawOwner := &daemonTraceOwner{}
	rawSvc := app.NewService(rawStore, rawOwner, app.Options{Incarnation: "e27-notax-raw", Shell: "/bin/sh", MaxQueuedInputBytes: 100, InputTracePreparer: preparer})
	raw, err := rawSvc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "e27-notax-raw", Command: "true", CWD: "/", YieldMS: 100})
	if err != nil {
		t.Fatal(err)
	}
	waitForTerminal(t, rawSvc, raw.SessionID)

	// Typed path.
	sequence := &typedSequence{}
	typedStore := newTypedRecordingStore(t, sequence)
	binder := &typedBinder{sequence: sequence, binding: daemonProjectBinding(t, []string{"go", "test", "./internal/app"})}
	typedOwner := &typedOrderOwner{sequence: sequence}
	typedSvc := app.NewService(typedStore, typedOwner, app.Options{Incarnation: "e27-notax-typed", Shell: "/bin/sh", MaxQueuedInputBytes: 100, ProjectCommandBinder: binder, InputTracePreparer: preparer})
	typed, err := typedSvc.Start(context.Background(), typedStartRequest("e27-notax-typed", "./internal/app"))
	if err != nil {
		t.Fatal(err)
	}
	waitForTerminal(t, typedSvc, typed.SessionID)

	// Persistent path with tracing omitted.
	persistentStore := openPersistentLaunchStore(t)
	handle := &persistentFakeHandle{pid: 4242}
	runtime := &fakePersistentRuntime{launch: app.PersistentLaunch{Handle: handle, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}, PID: 4242}}
	persistentSvc := app.NewService(persistentStore, &fakeOwner{}, app.Options{Incarnation: "e27-notax-persistent", Shell: "/bin/sh", MaxQueuedInputBytes: 100, PersistentRuntime: runtime, InputTracePreparer: preparer})
	if _, err := persistentSvc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "e27-notax-persistent", Command: "sleep 10", CWD: "/", Persistent: true, SessionName: "dev"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := persistentSvc.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if preparer.calls.Load() != 0 {
		t.Fatalf("off/omitted paths called preparer %d times", preparer.calls.Load())
	}
}

func openE27DaemonStore(t *testing.T) *storeadapter.Repository {
	t.Helper()
	store, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 8, MaxSessionOutput: 1 << 20, MaxTotalState: 8 << 20, ControlReserve: 4096})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func e27DaemonPrepared() *daemonTracePrepared {
	return &daemonTracePrepared{
		binding: trace.InstrumentationBinding{
			SchemaVersion: trace.SchemaVersion, TraceID: "trace_01K00000000000000000000000", Mode: trace.ModeBestEffort, Status: trace.BindingActive,
			Provider: trace.ProviderIdentity{ID: "dyld-interpose", Version: 1, CapabilityVersion: 1}, Platform: "darwin",
			InstrumentationFingerprint: strings.Repeat("a", 64), InstrumentationEffect: trace.EffectEnvironmentAffecting,
			Coverage: trace.CoverageMatrix{FilesystemReads: trace.CoveragePartial, FilesystemMetadataQueries: trace.CoveragePartial, DirectoryEnumerations: trace.CoveragePartial, FilesystemWrites: trace.CoveragePartial, ExecutedBinaries: trace.CoveragePartial, LoadedLibraries: trace.CoveragePartial, EnvironmentNamesObserved: trace.CoverageUnsupported, NetworkAttempts: trace.CoverageUnsupported, ChildProcesses: trace.CoveragePartial},
		},
		env: []operation.EnvironmentEntry{{Key: "SHELLBEAM_TRACE_ID", Value: "trace_01K00000000000000000000000"}, {Key: "DYLD_INSERT_LIBRARIES", Value: "/private/provider/instrumentation.dylib"}},
	}
}

type e27LostRaceStore struct {
	*storeadapter.Repository
	winner operation.Reservation
	seen   atomic.Bool
}

func (s *e27LostRaceStore) FindOperation(ctx context.Context, id operation.ID) (operation.Reservation, bool, error) {
	if !s.seen.Load() {
		return operation.Reservation{}, false, nil
	}
	return s.Repository.FindOperation(ctx, id)
}

func (s *e27LostRaceStore) ReserveOperation(ctx context.Context, incoming operation.Reservation) (operation.Reservation, bool, app.StoreResult) {
	winner := incoming
	winner.SessionID = operation.SessionID("e27-race-winner-session")
	winnerTrace := e27DaemonPrepared().binding
	winnerTrace.TraceID = "trace_01K00000000000000000000001"
	winnerTrace.InstrumentationFingerprint = strings.Repeat("b", 64)
	winner.Trace = &winnerTrace
	winner.ExecutionFingerprint = strings.Repeat("c", 64)
	stored, created, result := s.Repository.ReserveOperation(ctx, winner)
	if result.Err != nil || !created {
		return stored, created, result
	}
	s.seen.Store(true)
	s.winner = stored
	return stored, false, app.StoreResult{Durability: app.DurableChange}
}

func TestE27InputTraceCommitLostRaceAbortsLoserAndUsesStoredAuthority(t *testing.T) {
	base := openE27DaemonStore(t)
	store := &e27LostRaceStore{Repository: base}
	prepared := e27DaemonPrepared()
	preparer := &daemonTracePreparer{prepared: prepared}
	owner := &daemonTraceOwner{}
	svc := app.NewService(store, owner, app.Options{Incarnation: "e27-race", Shell: "/bin/sh", MaxQueuedInputBytes: 100, InputTracePreparer: preparer})
	view, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "e27-race", Command: "true", CWD: "/", TraceMode: trace.ModeBestEffort})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.aborts.Load() != 1 || owner.starts.Load() != 0 {
		t.Fatalf("loser aborts=%d starts=%d", prepared.aborts.Load(), owner.starts.Load())
	}
	if view.SessionID != string(store.winner.SessionID) || store.winner.Trace == nil || store.winner.Trace.TraceID != "trace_01K00000000000000000000001" {
		t.Fatalf("view=%#v winner=%#v", view, store.winner.Trace)
	}
}
