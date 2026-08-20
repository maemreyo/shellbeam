package daemon_test

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	projectapp "github.com/maemreyo/shellbeam/internal/app/project"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	project "github.com/maemreyo/shellbeam/internal/core/project"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

type typedSequence struct {
	mu     sync.Mutex
	values []string
}

func (s *typedSequence) add(value string) {
	s.mu.Lock()
	s.values = append(s.values, value)
	s.mu.Unlock()
}

func (s *typedSequence) snapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.values...)
}

func (s *typedSequence) reset() {
	s.mu.Lock()
	s.values = nil
	s.mu.Unlock()
}

type typedRecordingStore struct {
	*storeadapter.Repository
	sequence    *typedSequence
	claimCalls  atomic.Int32
	commitCalls atomic.Int32
	findOpCalls atomic.Int32
}

func (s *typedRecordingStore) FindOperation(ctx context.Context, id operation.ID) (operation.Reservation, bool, error) {
	s.findOpCalls.Add(1)
	s.sequence.add("find_operation")
	return s.Repository.FindOperation(ctx, id)
}

func (s *typedRecordingStore) ReserveTypedIntent(ctx context.Context, claim operation.TypedIntentClaim) (operation.TypedIntentClaim, bool, app.StoreResult) {
	s.claimCalls.Add(1)
	s.sequence.add("reserve_typed_claim")
	return s.Repository.ReserveTypedIntent(ctx, claim)
}

func (s *typedRecordingStore) CommitTypedBinding(ctx context.Context, id operation.ID, reservation operation.Reservation) (operation.Reservation, bool, app.StoreResult) {
	s.commitCalls.Add(1)
	s.sequence.add("commit_typed_binding")
	return s.Repository.CommitTypedBinding(ctx, id, reservation)
}

type typedBinder struct {
	sequence *typedSequence
	mu       sync.Mutex
	binding  project.CommandBinding
	err      error
	calls    int
	last     projectapp.BindRequest
}

func (b *typedBinder) Bind(_ context.Context, request projectapp.BindRequest) (project.CommandBinding, error) {
	b.sequence.add("bind_project_command")
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls++
	b.last = request
	if b.err != nil {
		return project.CommandBinding{}, b.err
	}
	return cloneTestBinding(b.binding), nil
}

func (b *typedBinder) setFailure(err error) {
	b.mu.Lock()
	b.err = err
	b.mu.Unlock()
}

func (b *typedBinder) callCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls
}

type typedOrderOwner struct {
	sequence *typedSequence
	starts   atomic.Int32
	mu       sync.Mutex
	specs    []operation.ExecutionSpec
}

func (o *typedOrderOwner) BindExecution(spec operation.ExecutionSpec) operation.ExecutionSpec {
	if spec.Mode == operation.ExecutionModeArgv && len(spec.Argv) > 0 {
		spec.Executable = spec.Argv[0]
	}
	return spec
}

func (o *typedOrderOwner) Start(_ context.Context, spec operation.ExecutionSpec, _ app.OutputSink) (app.ProcessHandle, receipt.SpawnEvidence, error) {
	o.sequence.add("spawn")
	o.starts.Add(1)
	o.mu.Lock()
	o.specs = append(o.specs, spec)
	o.mu.Unlock()
	return fakeHandle{}, receipt.SpawnEvidence{Attempted: true, Succeeded: true}, nil
}

func TestProjectCommandFirstAdmissionOrdersClaimBindCommitBeforeSingleSpawn(t *testing.T) {
	sequence := &typedSequence{}
	store := newTypedRecordingStore(t, sequence)
	binding := daemonProjectBinding(t, []string{"go", "test", "-json", "./internal/app"})
	binder := &typedBinder{sequence: sequence, binding: binding}
	owner := &typedOrderOwner{sequence: sequence}
	svc := app.NewService(store, owner, app.Options{
		Incarnation: "typed-daemon", Shell: "/bin/sh", MaxQueuedInputBytes: 100,
		ProjectCommandBinder: binder,
	})
	req := typedStartRequest("typed-order", "./internal/app")
	started, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	terminal := waitForTerminal(t, svc, started.SessionID)
	wantOrder := []string{"find_operation", "reserve_typed_claim", "bind_project_command", "commit_typed_binding", "spawn"}
	if got := sequence.snapshot(); !reflect.DeepEqual(got[:min(len(got), len(wantOrder))], wantOrder) {
		t.Fatalf("order=%v want prefix=%v", got, wantOrder)
	}
	if owner.starts.Load() != 1 {
		t.Fatalf("starts=%d", owner.starts.Load())
	}
	owner.mu.Lock()
	specs := append([]operation.ExecutionSpec(nil), owner.specs...)
	owner.mu.Unlock()
	if len(specs) != 1 || specs[0].Mode != operation.ExecutionModeArgv || !reflect.DeepEqual(specs[0].Argv, binding.ResolvedArgv) || specs[0].CWD != binding.ResolvedCWD {
		t.Fatalf("specs=%#v", specs)
	}
	stored, err := store.LoadOperation(context.Background(), "typed-order")
	if err != nil {
		t.Fatal(err)
	}
	if stored.SchemaVersion != 3 || stored.ProjectCommand == nil || stored.ProjectCommand.ManifestDigest != binding.ManifestDigest || stored.ProjectCommand.SourceGeneration != binding.SourceGeneration || stored.ProjectCommand.ParameterFingerprint != binding.ParameterFingerprint {
		t.Fatalf("stored=%#v", stored)
	}
	if stored.StructuredAdapter != "go-test-json" {
		t.Fatalf("structured adapter=%q", stored.StructuredAdapter)
	}
	if terminal.Receipt == nil || terminal.Receipt.SchemaVersion != 3 || terminal.Receipt.ProjectCommand == nil || terminal.Receipt.ProjectCommand.ParameterFingerprint != binding.ParameterFingerprint {
		t.Fatalf("terminal receipt=%#v", terminal.Receipt)
	}
}

func TestProjectCommandBindingFailureLeavesClaimButNoOperationOrSpawn(t *testing.T) {
	sequence := &typedSequence{}
	store := newTypedRecordingStore(t, sequence)
	binder := &typedBinder{sequence: sequence, err: errors.New("provider unavailable")}
	owner := &typedOrderOwner{sequence: sequence}
	svc := app.NewService(store, owner, app.Options{Incarnation: "typed-daemon", Shell: "/bin/sh", MaxQueuedInputBytes: 100, ProjectCommandBinder: binder})
	req := typedStartRequest("typed-bind-fail", "./internal/app")
	if _, err := svc.Start(context.Background(), req); err == nil {
		t.Fatal("binding failure accepted")
	}
	if got := sequence.snapshot(); !reflect.DeepEqual(got, []string{"find_operation", "reserve_typed_claim", "bind_project_command"}) {
		t.Fatalf("order=%v", got)
	}
	if owner.starts.Load() != 0 || store.commitCalls.Load() != 0 {
		t.Fatalf("starts=%d commits=%d", owner.starts.Load(), store.commitCalls.Load())
	}
	claim, found, err := store.FindTypedIntent(context.Background(), "typed-bind-fail")
	if err != nil || !found || claim.Intent.ProjectCommandID != req.ProjectCommandID {
		t.Fatalf("claim=%#v found=%v err=%v", claim, found, err)
	}
	if _, found, err := store.FindOperation(context.Background(), "typed-bind-fail"); err != nil || found {
		t.Fatalf("operation found=%v err=%v", found, err)
	}
}

func TestProjectCommandAdmittedRetryBypassesBinderAndPreservesFrozenBinding(t *testing.T) {
	sequence := &typedSequence{}
	store := newTypedRecordingStore(t, sequence)
	binding := daemonProjectBinding(t, []string{"go", "test", "-json", "./internal/app"})
	binder := &typedBinder{sequence: sequence, binding: binding}
	owner := &typedOrderOwner{sequence: sequence}
	svc := app.NewService(store, owner, app.Options{Incarnation: "typed-daemon", Shell: "/bin/sh", MaxQueuedInputBytes: 100, ProjectCommandBinder: binder})
	req := typedStartRequest("typed-retry", "./internal/app")
	first, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	_ = waitForTerminal(t, svc, first.SessionID)
	if binder.callCount() != 1 || owner.starts.Load() != 1 {
		t.Fatalf("after first bind calls=%d starts=%d", binder.callCount(), owner.starts.Load())
	}
	binder.setFailure(errors.New("binder must not be called on admitted retry"))
	sequence.reset()
	retry := req
	retry.YieldMS = 0
	replayed, err := svc.Start(context.Background(), retry)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.SessionID != first.SessionID || binder.callCount() != 1 || owner.starts.Load() != 1 {
		t.Fatalf("replay=%#v first=%#v bind calls=%d starts=%d", replayed, first, binder.callCount(), owner.starts.Load())
	}
	if got := sequence.snapshot(); !reflect.DeepEqual(got, []string{"find_operation"}) {
		t.Fatalf("retry touched current binding state: %v", got)
	}
	stored, err := store.LoadOperation(context.Background(), "typed-retry")
	if err != nil || stored.ProjectCommand == nil || !reflect.DeepEqual(stored.ProjectCommand.ResolvedArgv, binding.ResolvedArgv) {
		t.Fatalf("stored=%#v err=%v", stored, err)
	}
}

func TestProjectCommandPytestReplayPreservesCaptureDigestBinding(t *testing.T) {
	sequence := &typedSequence{}
	store := newTypedRecordingStore(t, sequence)
	binding := daemonProjectBinding(t, []string{"pytest", "test_example.py", "--junitxml=reports/junit.xml", "-o", "junit_family=xunit2", "-o", "addopts="})
	binder := &typedBinder{sequence: sequence, binding: binding}
	owner := &typedOrderOwner{sequence: sequence}
	digest := strings.Repeat("c", 64)
	preparer := &pytestCapturePreparerStub{prepare: app.StructuredCapturePreparation{AdapterID: "pytest-junit-xml", CaptureDigest: digest, Owned: true}}
	svc := app.NewService(store, owner, app.Options{
		Incarnation: "typed-daemon", Shell: "/bin/sh", MaxQueuedInputBytes: 100,
		ProjectCommandBinder: binder, StructuredCapturePreparer: preparer,
	})
	req := typedStartRequest("typed-pytest-replay", "./internal/app")
	req.StructuredAdapter = "pytest-junit-xml"
	first, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	_ = waitForTerminal(t, svc, first.SessionID)
	stored, err := store.LoadOperation(context.Background(), operation.ID(req.OperationID))
	if err != nil {
		t.Fatal(err)
	}
	if stored.StructuredCaptureDigest != digest {
		t.Fatalf("stored capture digest=%q want=%q", stored.StructuredCaptureDigest, digest)
	}
	wantObservation, err := (operation.ObservationBinding{StructuredAdapter: "pytest-junit-xml", StructuredCaptureDigest: digest}).Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if stored.ObservationBindingFingerprint != wantObservation {
		t.Fatalf("stored observation fingerprint=%q want digest-bound=%q", stored.ObservationBindingFingerprint, wantObservation)
	}

	binder.setFailure(errors.New("binder must not run on admitted pytest replay"))
	sequence.reset()
	replayed, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.SessionID != first.SessionID || binder.callCount() != 1 || owner.starts.Load() != 1 || preparer.calls.Load() != 1 {
		t.Fatalf("replay=%#v first=%#v binds=%d starts=%d prepares=%d", replayed, first, binder.callCount(), owner.starts.Load(), preparer.calls.Load())
	}
	if got := sequence.snapshot(); !reflect.DeepEqual(got, []string{"find_operation"}) {
		t.Fatalf("pytest replay touched current binding state: %v", got)
	}
}

func TestProjectCommandConflictingCallerFingerprintFailsBeforeClaimOrBinder(t *testing.T) {
	sequence := &typedSequence{}
	store := newTypedRecordingStore(t, sequence)
	binding := daemonProjectBinding(t, []string{"go", "test", "-json", "./internal/app"})
	binder := &typedBinder{sequence: sequence, binding: binding}
	owner := &typedOrderOwner{sequence: sequence}
	svc := app.NewService(store, owner, app.Options{Incarnation: "typed-daemon", Shell: "/bin/sh", MaxQueuedInputBytes: 100, ProjectCommandBinder: binder})
	req := typedStartRequest("typed-conflict", "./internal/app")
	first, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	_ = waitForTerminal(t, svc, first.SessionID)
	claimsBefore := store.claimCalls.Load()
	bindsBefore := binder.callCount()
	sequence.reset()
	conflict := req
	conflict.Params = map[string]string{"package": "./other"}
	if _, err := svc.Start(context.Background(), conflict); !errors.Is(err, failure.OperationConflict) {
		t.Fatalf("conflict err=%v", err)
	}
	if store.claimCalls.Load() != claimsBefore || binder.callCount() != bindsBefore || owner.starts.Load() != 1 {
		t.Fatalf("claims=%d/%d binds=%d/%d starts=%d", store.claimCalls.Load(), claimsBefore, binder.callCount(), bindsBefore, owner.starts.Load())
	}
	if got := sequence.snapshot(); !reflect.DeepEqual(got, []string{"find_operation"}) {
		t.Fatalf("conflict touched current state: %v", got)
	}
}

func TestOrdinaryStartPaysZeroTypedCommandTax(t *testing.T) {
	sequence := &typedSequence{}
	store := newTypedRecordingStore(t, sequence)
	binder := &typedBinder{sequence: sequence, err: errors.New("must not be called")}
	owner := &typedOrderOwner{sequence: sequence}
	svc := app.NewService(store, owner, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100, ProjectCommandBinder: binder})
	started, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "raw-zero-tax", Argv: []string{"true"}, CWD: "/", YieldMS: 0})
	if err != nil {
		t.Fatal(err)
	}
	_ = waitForTerminal(t, svc, started.SessionID)
	if binder.callCount() != 0 || store.claimCalls.Load() != 0 || store.commitCalls.Load() != 0 || owner.starts.Load() != 1 {
		t.Fatalf("binder=%d claims=%d commits=%d starts=%d", binder.callCount(), store.claimCalls.Load(), store.commitCalls.Load(), owner.starts.Load())
	}
}

func TestProjectCommandRequestShapeRejectsRawExecutionFields(t *testing.T) {
	sequence := &typedSequence{}
	store := newTypedRecordingStore(t, sequence)
	binder := &typedBinder{sequence: sequence, binding: daemonProjectBinding(t, []string{"true"})}
	owner := &typedOrderOwner{sequence: sequence}
	svc := app.NewService(store, owner, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100, ProjectCommandBinder: binder})
	base := typedStartRequest("typed-shape", "./internal/app")
	cases := []app.StartRequest{
		func() app.StartRequest { v := base; v.ProtocolVersion = 1; return v }(),
		func() app.StartRequest { v := base; v.WorkspaceID = ""; return v }(),
		func() app.StartRequest { v := base; v.Command = "true"; return v }(),
		func() app.StartRequest { v := base; v.Argv = []string{"true"}; return v }(),
		func() app.StartRequest { v := base; v.CWD = "/repo"; return v }(),
		{ProtocolVersion: 2, OperationID: "typed-shape-params-only", WorkspaceID: base.WorkspaceID, Params: map[string]string{"package": "./internal/app"}},
	}
	for index, request := range cases {
		if _, err := svc.Start(context.Background(), request); err == nil {
			t.Fatalf("case %d accepted: %#v", index, request)
		}
	}
	if binder.callCount() != 0 || store.claimCalls.Load() != 0 || owner.starts.Load() != 0 {
		t.Fatalf("invalid shapes touched execution: binder=%d claims=%d starts=%d", binder.callCount(), store.claimCalls.Load(), owner.starts.Load())
	}
}

func newTypedRecordingStore(t *testing.T, sequence *typedSequence) *typedRecordingStore {
	t.Helper()
	repository, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 8, MaxSessionOutput: 1 << 20, MaxTotalState: 8 << 20, ControlReserve: 1 << 10})
	if err != nil {
		t.Fatal(err)
	}
	return &typedRecordingStore{Repository: repository, sequence: sequence}
}

func typedStartRequest(operationID, pkg string) app.StartRequest {
	return app.StartRequest{
		ProtocolVersion: 2, OperationID: operationID,
		WorkspaceID: "ws_01K00000000000000000000000", ProjectCommandID: "test_package",
		Params: map[string]string{"package": pkg}, TimeoutMS: 5000, YieldMS: 0, MaxOutputBytes: 20000,
	}
}

func daemonProjectBinding(t *testing.T, argv []string) project.CommandBinding {
	t.Helper()
	params := []project.ParameterBinding{{ID: "package", Kind: project.ParameterRepoPackage, Value: "./internal/app", Source: project.BindingSourceCaller, ProviderID: "go-repo-package", ProviderVersion: 1}}
	fingerprint, err := project.ParameterFingerprint(params)
	if err != nil {
		t.Fatal(err)
	}
	binding := project.CommandBinding{
		SchemaVersion: project.BindingSchemaVersion, ManifestDigest: strings.Repeat("a", 64), ManifestSchemaVersion: project.ManifestSchemaV2,
		CommandID: "test_package", ParameterFingerprint: fingerprint, Parameters: params,
		ResolvedArgv: append([]string(nil), argv...), LogicalCWD: ".", ResolvedCWD: "/repo",
		SourceGeneration: "gen_" + strings.Repeat("b", 64), PathObservationQuality: project.PathObservationExactAtBind,
	}
	if err := binding.Validate(); err != nil {
		t.Fatal(err)
	}
	return binding
}

func cloneTestBinding(value project.CommandBinding) project.CommandBinding {
	value.Parameters = append([]project.ParameterBinding(nil), value.Parameters...)
	value.ResolvedArgv = append([]string(nil), value.ResolvedArgv...)
	return value
}

func TestProjectCommandBinderMismatchReturnsStableBindingConflict(t *testing.T) {
	sequence := &typedSequence{}
	store := newTypedRecordingStore(t, sequence)
	binding := daemonProjectBinding(t, []string{"go", "test", "./internal/app"})
	binding.CommandID = "different_command"
	binder := &typedBinder{sequence: sequence, binding: binding}
	owner := &typedOrderOwner{sequence: sequence}
	svc := app.NewService(store, owner, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100, ProjectCommandBinder: binder})
	_, err := svc.Start(context.Background(), typedStartRequest("typed-binder-mismatch", "./internal/app"))
	if got := failure.Public(err).Code; got != failure.ProjectCommandBindingConflict {
		t.Fatalf("code=%q err=%v", got, err)
	}
	if owner.starts.Load() != 0 {
		t.Fatalf("binder mismatch spawned child: %d", owner.starts.Load())
	}
}
