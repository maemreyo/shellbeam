package repro

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
	d := structured.Derivation{SchemaVersion: 1, DerivationKey: key, SourceAuthorityRefs: []structured.RawOutputRef{ref}, Producer: producer, DerivationSchemaVersion: 1, DerivationConfigDigest: strings.Repeat("2", 64), Lifecycle: lifecycle, Completeness: completeness}
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
