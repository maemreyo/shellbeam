package repro

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	inputtrace "github.com/maemreyo/shellbeam/internal/core/inputtrace"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	project "github.com/maemreyo/shellbeam/internal/core/project"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	core "github.com/maemreyo/shellbeam/internal/core/repro"
	"github.com/maemreyo/shellbeam/internal/core/session"
	structured "github.com/maemreyo/shellbeam/internal/core/structuredresult"
	telemetry "github.com/maemreyo/shellbeam/internal/core/telemetry"
)

type memoryReproRepository struct {
	mu              sync.Mutex
	reservation     operation.Reservation
	receipt         receipt.Receipt
	structured      structured.Derivation
	structuredFound bool
	telemetry       telemetry.PerformanceRecord
	telemetryFound  bool
	inputTrace      inputtrace.Record
	inputTraceFound bool
	creates         map[string]memoryReproCreate
}

type memoryReproCreate struct {
	fingerprint string
	capsule     core.Capsule
}

func (r *memoryReproRepository) LoadOperation(ctx context.Context, id operation.ID) (operation.Reservation, error) {
	if err := ctx.Err(); err != nil {
		return operation.Reservation{}, err
	}
	if id != r.reservation.OperationID {
		return operation.Reservation{}, errors.New("not found")
	}
	return r.reservation, nil
}
func (r *memoryReproRepository) LoadReceipt(ctx context.Context, id operation.SessionID) (receipt.Receipt, error) {
	if err := ctx.Err(); err != nil {
		return receipt.Receipt{}, err
	}
	if id != operation.SessionID(r.receipt.SessionID) {
		return receipt.Receipt{}, errors.New("not found")
	}
	return r.receipt, nil
}
func (r *memoryReproRepository) FindOperationDerivation(ctx context.Context, operationID string) (structured.Derivation, bool, error) {
	if err := ctx.Err(); err != nil {
		return structured.Derivation{}, false, err
	}
	if operationID != string(r.reservation.OperationID) || !r.structuredFound {
		return structured.Derivation{}, false, nil
	}
	return r.structured, true, nil
}
func (r *memoryReproRepository) FindPerformanceByOperation(ctx context.Context, operationID string) (telemetry.PerformanceRecord, bool, error) {
	if err := ctx.Err(); err != nil {
		return telemetry.PerformanceRecord{}, false, err
	}
	if operationID != string(r.reservation.OperationID) || !r.telemetryFound {
		return telemetry.PerformanceRecord{}, false, nil
	}
	return r.telemetry, true, nil
}
func (r *memoryReproRepository) LoadInputTraceByOperation(ctx context.Context, operationID string) (inputtrace.Record, bool, error) {
	if err := ctx.Err(); err != nil {
		return inputtrace.Record{}, false, err
	}
	if operationID != string(r.reservation.OperationID) || !r.inputTraceFound {
		return inputtrace.Record{}, false, nil
	}
	return r.inputTrace, true, nil
}
func (r *memoryReproRepository) CreateRepro(ctx context.Context, fingerprint string, capsule core.Capsule) (core.Capsule, bool, error) {
	if err := ctx.Err(); err != nil {
		return core.Capsule{}, false, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.creates == nil {
		r.creates = map[string]memoryReproCreate{}
	}
	if current, ok := r.creates[capsule.CreateID]; ok {
		if current.fingerprint != fingerprint {
			return core.Capsule{}, false, errors.New("operation_metadata_conflict")
		}
		return current.capsule, false, nil
	}
	r.creates[capsule.CreateID] = memoryReproCreate{fingerprint: fingerprint, capsule: capsule}
	return capsule, true, nil
}
func (r *memoryReproRepository) GetReproByCreateID(ctx context.Context, createID string) (core.Capsule, bool, error) {
	if err := ctx.Err(); err != nil {
		return core.Capsule{}, false, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.creates[createID]
	return current.capsule, ok, nil
}
func (r *memoryReproRepository) GetRepro(ctx context.Context, reproID string) (core.Capsule, bool, error) {
	if err := ctx.Err(); err != nil {
		return core.Capsule{}, false, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, current := range r.creates {
		if current.capsule.ReproID == reproID {
			return current.capsule, true, nil
		}
	}
	return core.Capsule{}, false, nil
}

func TestCreateFreezesCurrentDerivationCutAndRetryReturnsOriginal(t *testing.T) {
	repo := reproFixture(t)
	repo.structured, repo.structuredFound = structuredFixture(t, structured.LifecyclePending, structured.CompletenessPartial), true
	svc := New(repo)
	request := core.CreateRequest{CreateID: "repro-create-1", OperationID: "op-repro-1", Policy: core.CapturePolicy{DependentDerivations: core.CaptureCurrent}}
	first, err := svc.Create(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first.CaptureCutDigest == "" || len(first.Results) != 2 {
		t.Fatalf("initial capsule=%#v", first)
	}
	if got := referenceAvailability(first.Results, "structured_result"); got != core.AvailabilityPending {
		t.Fatalf("structured availability=%q", got)
	}
	if got := referenceAvailability(first.Results, "execution_telemetry"); got != core.AvailabilityAbsent {
		t.Fatalf("telemetry availability=%q", got)
	}

	repo.structured = structuredFixture(t, structured.LifecycleTerminal, structured.CompletenessComplete)
	repo.telemetry, repo.telemetryFound = telemetryFixtureForRepro(t, repo.receipt), true
	retry, err := svc.Create(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, retry) {
		t.Fatalf("retry mutated frozen cut\nfirst=%#v\nretry=%#v", first, retry)
	}

	laterReq := request
	laterReq.CreateID = "repro-create-2"
	later, err := svc.Create(context.Background(), laterReq)
	if err != nil {
		t.Fatal(err)
	}
	if later.ReproID == first.ReproID || later.CaptureCutDigest == first.CaptureCutDigest {
		t.Fatalf("later cut did not change first=%#v later=%#v", first, later)
	}
	if got := referenceAvailability(later.Results, "structured_result"); got != core.AvailabilityTerminal {
		t.Fatalf("later structured availability=%q", got)
	}
	if got := referenceAvailability(later.Results, "execution_telemetry"); got != core.AvailabilityTerminal {
		t.Fatalf("later telemetry availability=%q", got)
	}
}

func referenceAvailability(refs []core.ReferenceDescriptor, kind string) core.AvailabilityState {
	for _, ref := range refs {
		if ref.RecordKind == kind {
			return ref.OriginalAvailability
		}
	}
	return ""
}

func reproFixture(t *testing.T) *memoryReproRepository {
	t.Helper()
	execution := strings.Repeat("e", 64)
	now := time.Date(2026, 8, 15, 1, 0, 0, 0, time.UTC)
	zero := 0
	return &memoryReproRepository{
		reservation: operation.Reservation{SchemaVersion: 2, OperationID: "op-repro-1", SessionID: "session-repro-1", RequestFingerprint: strings.Repeat("r", 64), ExecutionFingerprint: execution, ExecutionMode: operation.ExecutionModeArgv, Executable: "go", Argv: []string{"go", "test", "./..."}, CWD: "/tmp", Shell: "/bin/sh", CreatedAt: now},
		receipt:     receipt.Receipt{SchemaVersion: 2, OperationID: "op-repro-1", SessionID: "session-repro-1", RequestFingerprint: strings.Repeat("a", 64), ExecutionFingerprint: execution, DaemonIncarnation: "daemon-1", ExecutionMode: "argv", Executable: "go", State: session.Completed, Outcome: session.Success, CWD: "/tmp", OutputComplete: true, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}, Exit: receipt.ExitEvidence{Reaped: true, Code: &zero}},
		creates:     map[string]memoryReproCreate{},
	}
}

func structuredFixture(t *testing.T, lifecycle structured.Lifecycle, completeness structured.Completeness) structured.Derivation {
	t.Helper()
	ref := structured.RawOutputRef{SessionID: "session-repro-1", StartByte: 0, EndByte: 4, SHA256: strings.Repeat("1", 64)}
	producer := structured.Producer{AdapterID: "go-test-json", AdapterVersion: 1, CapabilityVersion: 1}
	key, err := structured.DerivationKey([]structured.RawOutputRef{ref}, producer, 1, strings.Repeat("2", 64))
	if err != nil {
		t.Fatal(err)
	}
	d := structured.Derivation{SchemaVersion: 1, DerivationKey: key, SourceAuthorityRefs: []structured.StructuredInputRef{structured.RawInputRef(ref)}, Producer: producer, DerivationSchemaVersion: 1, DerivationConfigDigest: strings.Repeat("2", 64), Lifecycle: lifecycle, Completeness: completeness}
	if lifecycle == structured.LifecycleTerminal {
		d.ParseOutcome = structured.ParseComplete
	}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	return d
}

func telemetryFixtureForRepro(t *testing.T, rec receipt.Receipt) telemetry.PerformanceRecord {
	t.Helper()
	receiptDigest, err := receipt.Digest(rec)
	if err != nil {
		t.Fatal(err)
	}
	producer := telemetry.Producer{ProducerID: "shellbeam.telemetry", ProducerVersion: 1, CapabilityVersion: 1}
	config := strings.Repeat("3", 64)
	key, err := telemetry.DerivationKey(receiptDigest, producer, 1, config)
	if err != nil {
		t.Fatal(err)
	}
	unavailable := telemetry.IntMetric{Quality: telemetry.MetricUnavailable}
	record := telemetry.PerformanceRecord{SchemaVersion: 1, DerivationKey: key, DerivationSchemaVersion: 1, DerivationConfigDigest: config, Producer: producer, OperationID: rec.OperationID, SessionID: rec.SessionID, ReceiptDigest: receiptDigest, CommandSemanticsFingerprint: rec.ExecutionFingerprint, ScopeClass: telemetry.ScopeArgv, Platform: "darwin", Architecture: "arm64", TerminalOutcome: rec.Outcome, CapturedAt: time.Date(2026, 8, 15, 1, 0, 1, 0, time.UTC), Lifecycle: telemetry.LifecycleTerminal, Completeness: telemetry.CompletenessPartial, Resources: telemetry.ResourceMetrics{CPUUserMS: unavailable, CPUSystemMS: unavailable, MaxRSSBytes: unavailable, ReadBytes: unavailable, WriteBytes: unavailable, ProcessCountPeak: unavailable}}
	if err := record.Validate(); err != nil {
		t.Fatal(err)
	}
	return record
}

func TestCreateFreezesTypedProjectCommandFromReceiptWithoutTelemetry(t *testing.T) {
	repo := reproFixture(t)
	binding := reproProjectCommandBinding(t)
	repo.receipt.SchemaVersion = 3
	repo.receipt.ProjectCommand = &binding
	repo.receipt.CWD = binding.ResolvedCWD
	repo.reservation.SchemaVersion = 3
	repo.reservation.ProjectCommand = &binding
	repo.reservation.Argv = append([]string(nil), binding.ResolvedArgv...)
	repo.reservation.LogicalCWD = binding.LogicalCWD
	repo.reservation.CWD = binding.ResolvedCWD
	repo.telemetryFound = false

	capsule, err := New(repo).Create(context.Background(), core.CreateRequest{CreateID: "repro-typed-1", OperationID: "op-repro-1", Policy: core.CapturePolicy{DependentDerivations: core.CaptureCurrent}})
	if err != nil {
		t.Fatal(err)
	}
	bindingDigest, err := binding.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if capsule.Execution.ProjectCommandBindingDigest != bindingDigest || capsule.Execution.ProjectManifestDigest != binding.ManifestDigest || capsule.Execution.ProjectCommandID != binding.CommandID || capsule.Execution.ParameterBindingFingerprint != binding.ParameterFingerprint {
		t.Fatalf("typed execution provenance=%#v", capsule.Execution)
	}
	if !reflect.DeepEqual(capsule.Execution.ResolvedArgv, binding.ResolvedArgv) || capsule.Execution.CommandDetails != core.CaptureExact {
		t.Fatalf("frozen argv=%#v", capsule.Execution)
	}
	wantProviders := []core.ParameterProviderDescriptor{{ParameterID: "package", ProviderID: "go-repo-package", ProviderVersion: 1}}
	if !reflect.DeepEqual(capsule.Execution.ParameterProviders, wantProviders) {
		t.Fatalf("providers=%#v want=%#v", capsule.Execution.ParameterProviders, wantProviders)
	}
	if capsule.Project.ManifestDigest != binding.ManifestDigest || capsule.Project.Quality != core.CaptureExact {
		t.Fatalf("project descriptor=%#v", capsule.Project)
	}
}

func TestCreateOrdinaryOperationKeepsTypedProjectCommandDescriptorAbsent(t *testing.T) {
	repo := reproFixture(t)
	capsule, err := New(repo).Create(context.Background(), core.CreateRequest{CreateID: "repro-ordinary-1", OperationID: "op-repro-1", Policy: core.CapturePolicy{DependentDerivations: core.CaptureCurrent}})
	if err != nil {
		t.Fatal(err)
	}
	if capsule.Execution.ProjectCommandBindingDigest != "" || capsule.Execution.ProjectManifestDigest != "" || capsule.Execution.ParameterBindingFingerprint != "" || len(capsule.Execution.ParameterProviders) != 0 {
		t.Fatalf("ordinary operation gained typed descriptor: %#v", capsule.Execution)
	}
}

func reproProjectCommandBinding(t *testing.T) project.CommandBinding {
	t.Helper()
	params := []project.ParameterBinding{{ID: "package", Kind: project.ParameterRepoPackage, Value: "./internal/app", Source: project.BindingSourceCaller, ProviderID: "go-repo-package", ProviderVersion: 1}}
	fingerprint, err := project.ParameterFingerprint(params)
	if err != nil {
		t.Fatal(err)
	}
	binding := project.CommandBinding{
		SchemaVersion:  project.BindingSchemaVersion,
		ManifestDigest: strings.Repeat("4", 64), ManifestSchemaVersion: project.ManifestSchemaV2,
		CommandID: "test_package", ParameterFingerprint: fingerprint, Parameters: params,
		ResolvedArgv: []string{"go", "test", "./internal/app"}, LogicalCWD: ".", ResolvedCWD: "/tmp",
		SourceGeneration: "gen_" + strings.Repeat("5", 64), PathObservationQuality: project.PathObservationExactAtBind,
	}
	if err := binding.Validate(); err != nil {
		t.Fatal(err)
	}
	return binding
}

func TestCreateFreezesInputTraceAsOpaqueDerivedReferenceWithoutResourcePaths(t *testing.T) {
	repo := reproFixture(t)
	repo.inputTrace, repo.inputTraceFound = inputTraceFixtureForRepro(t, repo.receipt), true
	capsule, err := New(repo).Create(context.Background(), core.CreateRequest{CreateID: "repro-trace-1", OperationID: "op-repro-1", Policy: core.CapturePolicy{DependentDerivations: core.CaptureCurrent}})
	if err != nil {
		t.Fatal(err)
	}
	var found *core.ReferenceDescriptor
	for i := range capsule.Results {
		if capsule.Results[i].RecordKind == "input_trace" {
			found = &capsule.Results[i]
			break
		}
	}
	if found == nil || found.RefID != "input-trace:"+repo.inputTrace.DerivationKey || found.Digest != repo.inputTrace.DerivationKey || found.ProducerID != repo.inputTrace.Provider.ID || found.ProducerVersion != repo.inputTrace.Provider.Version || found.OriginalAvailability != core.AvailabilityTerminal {
		t.Fatalf("input trace reference=%#v", found)
	}
	encoded, err := json.Marshal(capsule)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"internal/private.go", "/Users/private", "DYLD_INSERT_LIBRARIES"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("capsule leaked input trace resource %q: %s", forbidden, encoded)
		}
	}
}

func inputTraceFixtureForRepro(t *testing.T, rec receipt.Receipt) inputtrace.Record {
	t.Helper()
	digest, err := receipt.Digest(rec)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 15, 1, 0, 0, 0, time.UTC)
	record := inputtrace.Record{
		SchemaVersion: inputtrace.SchemaVersion, DerivationKey: strings.Repeat("6", 64), TraceID: "trace_01K00000000000000000000000",
		OperationID: rec.OperationID, SessionID: rec.SessionID, ReceiptDigest: digest, Mode: inputtrace.ModeBestEffort,
		Provider: inputtrace.ProviderIdentity{ID: "dyld-interpose", Version: 1, CapabilityVersion: 1}, Platform: "darwin",
		InstrumentationFingerprint: strings.Repeat("7", 64), InstrumentationEffect: inputtrace.EffectEnvironmentAffecting,
		Authority: inputtrace.AuthorityAdvisory, ScopeKind: inputtrace.ScopeObservedInput, MayHaveUnobservedDependencies: true,
		CaptureStart: now, CaptureEnd: now.Add(time.Second),
		Coverage: inputtrace.CoverageMatrix{
			FilesystemReads: inputtrace.CoveragePartial, FilesystemMetadataQueries: inputtrace.CoveragePartial, DirectoryEnumerations: inputtrace.CoveragePartial,
			FilesystemWrites: inputtrace.CoveragePartial, ExecutedBinaries: inputtrace.CoveragePartial, LoadedLibraries: inputtrace.CoveragePartial,
			EnvironmentNamesObserved: inputtrace.CoverageUnsupported, NetworkAttempts: inputtrace.CoverageUnsupported, ChildProcesses: inputtrace.CoveragePartial,
		},
		Outcome:   inputtrace.OutcomePartial,
		Resources: []inputtrace.Resource{{ObservationClass: inputtrace.ClassFilesystemReads, PathClass: inputtrace.PathRepoRelative, Identity: "internal/private.go"}},
		Summary:   inputtrace.Summary{ResourcesReturned: 1, ResourcesObserved: 1},
	}
	if err := record.Validate(); err != nil {
		t.Fatal(err)
	}
	return record
}
