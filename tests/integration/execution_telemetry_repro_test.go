//go:build linux || darwin

package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	reproapp "github.com/maemreyo/shellbeam/internal/app/repro"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	reprocore "github.com/maemreyo/shellbeam/internal/core/repro"
	"github.com/maemreyo/shellbeam/internal/core/session"
	structured "github.com/maemreyo/shellbeam/internal/core/structuredresult"
	telemetry "github.com/maemreyo/shellbeam/internal/core/telemetry"
)

type a4IntegrationFixture struct {
	ctx                  context.Context
	root                 string
	limits               storeadapter.Limits
	store                *storeadapter.Repository
	forbidden            []string
	now                  time.Time
	reservation          operation.Reservation
	derivation           structured.Derivation
	receiptDigest        string
	executionFingerprint string
}

func newA4IntegrationFixture(t *testing.T) a4IntegrationFixture {
	t.Helper()
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "state")
	limits := storeadapter.Limits{
		MaxSessions: 8, MaxSessionOutput: 1 << 20, MaxTotalState: 16 << 20, ControlReserve: 1024,
		MaxTelemetrySamples: 1, MaxTelemetryBytes: 2 << 20, MaxTelemetryKeys: 4,
		MaxTelemetryKeysPerRepository: 4, MaxTelemetrySamplesPerKey: 1, MaxTelemetryAge: 7 * 24 * time.Hour,
		MaxReproCapsules: 4, MaxReproBytes: 2 << 20, MaxReproAge: 7 * 24 * time.Hour,
	}
	store := openA4IntegrationStore(t, root, limits)
	forbidden := []string{
		"--token=super-secret", "password=hunter2", "-----BEGIN PRIVATE KEY-----", "/Users/alice/.ssh/id_work",
		"STDIN-CONTENT-SECRET", "RAW_ENV_SECRET_VALUE", "SOURCE_CONTENT_SECRET",
	}
	rawFixture := strings.Join(forbidden, "\n")
	now := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	executionFingerprint := strings.Repeat("e", 64)
	requestFingerprint := strings.Repeat("a", 64)
	observationFingerprint := strings.Repeat("o", 64)
	reservation := operation.Reservation{
		SchemaVersion: 2, OperationID: "a4-int-op", SessionID: "a4-int-session",
		RequestFingerprint: requestFingerprint, ExecutionFingerprint: executionFingerprint,
		ObservationBindingFingerprint: observationFingerprint,
		ExecutionMode:                 operation.ExecutionModeArgv, Executable: "/usr/bin/tool",
		Argv: []string{"tool", forbidden[0], forbidden[1], forbidden[2], forbidden[3]}, CWD: "/tmp",
		DaemonIncarnation: "a4-integration", CreatedAt: now,
	}
	if _, created, result := store.ReserveOperation(ctx, reservation); result.Err != nil || !created {
		t.Fatalf("reserve created=%v result=%#v", created, result)
	}
	if _, result := store.AppendOutput(ctx, reservation.SessionID, []byte(rawFixture)); result.Err != nil {
		t.Fatal(result.Err)
	}
	zero := 0
	rec := receipt.Receipt{
		SchemaVersion: 2, OperationID: string(reservation.OperationID), SessionID: string(reservation.SessionID),
		RequestFingerprint: requestFingerprint, ExecutionFingerprint: executionFingerprint,
		ObservationBindingFingerprint: observationFingerprint, DaemonIncarnation: reservation.DaemonIncarnation,
		ExecutionMode: string(operation.ExecutionModeArgv), Executable: reservation.Executable, CWD: reservation.CWD,
		State: session.Completed, Outcome: session.Success, OutputBytes: int64(len(rawFixture)), OutputComplete: true,
		InputAcceptedBytes: int64(len(forbidden[4])), InputDeliveredBytes: int64(len(forbidden[4])), StdinClosed: true,
		Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}, Exit: receipt.ExitEvidence{Reaped: true, Code: &zero},
	}
	if result := store.PublishTerminal(ctx, rec); result.Err != nil {
		t.Fatal(result.Err)
	}
	storedRaw, _, err := store.ReadOutput(ctx, reservation.SessionID, 0, 1<<20)
	if err != nil || string(storedRaw) != rawFixture {
		t.Fatalf("canonical raw fixture missing err=%v output=%q", err, storedRaw)
	}
	derivation := putA4StructuredTerminal(t, store, reservation.OperationID, reservation.SessionID, rawFixture)
	receiptDigest, err := receipt.Digest(rec)
	if err != nil {
		t.Fatal(err)
	}
	firstTelemetry := a4TelemetryRecord(t, string(reservation.OperationID), string(reservation.SessionID), receiptDigest, executionFingerprint, now.Add(time.Second))
	if err := store.PutPerformanceRecord(ctx, firstTelemetry); err != nil {
		t.Fatal(err)
	}
	return a4IntegrationFixture{ctx: ctx, root: root, limits: limits, store: store, forbidden: forbidden, now: now, reservation: reservation, derivation: derivation, receiptDigest: receiptDigest, executionFingerprint: executionFingerprint}
}

func TestTelemetryReproRestartRetentionCompactionAndPrivacy(t *testing.T) {
	fixture := newA4IntegrationFixture(t)
	service := reproapp.New(fixture.store)
	createRequest := reprocore.CreateRequest{CreateID: "a4-integration-create", OperationID: string(fixture.reservation.OperationID), Policy: reprocore.CapturePolicy{DependentDerivations: reprocore.CaptureCurrent}}
	capsule, err := service.Create(fixture.ctx, createRequest)
	if err != nil {
		t.Fatal(err)
	}
	if capsule.Execution.CommandDetails != reprocore.CapturePartial || len(capsule.Execution.ResolvedArgv) != 0 {
		t.Fatalf("unsafe argv survived privacy projection: %#v", capsule.Execution)
	}
	if got := a4Availability(capsule.Results, "structured_result"); got != reprocore.AvailabilityTerminal {
		t.Fatalf("structured availability=%q", got)
	}
	if got := a4Availability(capsule.Results, "execution_telemetry"); got != reprocore.AvailabilityTerminal {
		t.Fatalf("telemetry availability=%q", got)
	}
	encoded, err := json.Marshal(capsule)
	if err != nil {
		t.Fatal(err)
	}
	assertA4SecretsAbsent(t, encoded, fixture.forbidden...)

	// Simulate a durable response being lost: reopen the state root and retry the same create ID.
	fixture.store = openA4IntegrationStore(t, fixture.root, fixture.limits)
	service = reproapp.New(fixture.store)
	beforeRetry, err := fixture.store.ObservationHighWatermark(fixture.ctx)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := service.Create(fixture.ctx, createRequest)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(capsule, retry) {
		t.Fatalf("restart retry changed capsule\nfirst=%#v\nretry=%#v", capsule, retry)
	}
	afterRetry, err := fixture.store.ObservationHighWatermark(fixture.ctx)
	if err != nil || afterRetry != beforeRetry {
		t.Fatalf("retry allocated observation before=%d after=%d err=%v", beforeRetry, afterRetry, err)
	}

	if err := fixture.store.CompactRecords(fixture.ctx, fixture.derivation.DerivationKey); err != nil {
		t.Fatal(err)
	}
	secondTelemetry := a4TelemetryRecord(t, "a4-other-op", "a4-other-session", strings.Repeat("f", 64), strings.Repeat("c", 64), fixture.now.Add(2*time.Second))
	if err := fixture.store.PutPerformanceRecord(fixture.ctx, secondTelemetry); err != nil {
		t.Fatal(err)
	}
	inspected, err := service.Inspect(fixture.ctx, capsule.ReproID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(inspected.Capsule.Results, capsule.Results) {
		t.Fatalf("creation descriptors mutated after retention: %#v", inspected.Capsule.Results)
	}
	states := map[string]reprocore.ResolutionState{}
	for _, ref := range inspected.References {
		states[ref.RecordKind] = ref.ResolutionState
	}
	if states["structured_result"] != reprocore.ResolutionCompacted || states["execution_telemetry"] != reprocore.ResolutionPurged {
		t.Fatalf("dynamic resolution=%#v", states)
	}
	for _, dir := range []string{filepath.Join(fixture.root, "derived", "telemetry"), filepath.Join(fixture.root, "derived", "repro")} {
		assertA4SecretsAbsent(t, readA4DerivedTree(t, dir), fixture.forbidden...)
	}
}

func openA4IntegrationStore(t *testing.T, root string, limits storeadapter.Limits) *storeadapter.Repository {
	t.Helper()
	store, err := storeadapter.Open(root, limits)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func putA4StructuredTerminal(t *testing.T, store *storeadapter.Repository, operationID operation.ID, sessionID operation.SessionID, raw string) structured.Derivation {
	t.Helper()
	ctx := context.Background()
	ref := structured.RawOutputRef{SessionID: string(sessionID), StartByte: 0, EndByte: int64(len(raw)), SHA256: strings.Repeat("1", 64)}
	inputRef := structured.RawInputRef(ref)
	producer := structured.Producer{AdapterID: "go-test-json", AdapterVersion: 1, CapabilityVersion: 1}
	key, err := structured.DerivationKey([]structured.RawOutputRef{ref}, producer, 1, strings.Repeat("2", 64))
	if err != nil {
		t.Fatal(err)
	}
	pending := structured.Derivation{SchemaVersion: structured.SchemaVersion, DerivationKey: key, SourceAuthorityRefs: []structured.StructuredInputRef{inputRef}, Producer: producer, DerivationSchemaVersion: 1, DerivationConfigDigest: strings.Repeat("2", 64), Lifecycle: structured.LifecyclePending, Completeness: structured.CompletenessUnavailable}
	processing := pending
	processing.Lifecycle = structured.LifecycleProcessing
	terminal := processing
	terminal.Lifecycle, terminal.ParseOutcome, terminal.Completeness = structured.LifecycleTerminal, structured.ParseComplete, structured.CompletenessComplete
	if err := store.PutDerivation(ctx, pending); err != nil {
		t.Fatal(err)
	}
	if err := store.PutDerivation(ctx, processing); err != nil {
		t.Fatal(err)
	}
	record := structured.Record{SchemaVersion: structured.SchemaVersion, RecordKind: structured.RecordArtifactResult, Authority: structured.AuthorityMechanical, DerivationMethod: structured.DerivationNativeFieldMapping, Producer: producer, OperationID: string(operationID), SourceRef: inputRef, ArtifactResult: &structured.ArtifactResult{Name: "integration", Status: "ok"}}
	if err := store.PutRecords(ctx, key, []structured.Record{record}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutDerivation(ctx, terminal); err != nil {
		t.Fatal(err)
	}
	if err := store.BindOperationDerivation(ctx, operationID, key); err != nil {
		t.Fatal(err)
	}
	return terminal
}

func a4TelemetryRecord(t *testing.T, operationID, sessionID, receiptDigest, executionFingerprint string, capturedAt time.Time) telemetry.PerformanceRecord {
	t.Helper()
	producer := telemetry.Producer{ProducerID: "shellbeam.telemetry", ProducerVersion: 1, CapabilityVersion: 1}
	config := strings.Repeat("3", 64)
	key, err := telemetry.DerivationKey(receiptDigest, producer, 1, config)
	if err != nil {
		t.Fatal(err)
	}
	unavailable := telemetry.IntMetric{Quality: telemetry.MetricUnavailable}
	record := telemetry.PerformanceRecord{
		SchemaVersion: 1, DerivationKey: key, DerivationSchemaVersion: 1, DerivationConfigDigest: config, Producer: producer,
		OperationID: operationID, SessionID: sessionID, ReceiptDigest: receiptDigest, CommandSemanticsFingerprint: executionFingerprint,
		EnvironmentFingerprint: strings.Repeat("7", 64), EnvironmentSchemaVersion: 1, ToolchainFingerprint: strings.Repeat("8", 64), ToolchainSchemaVersion: 1,
		ScopeClass: telemetry.ScopeArgv, Platform: "darwin", Architecture: "arm64", WallMS: 1, OutputBytes: 1,
		TerminalOutcome: session.Success, CapturedAt: capturedAt, Lifecycle: telemetry.LifecycleTerminal, Completeness: telemetry.CompletenessPartial,
		Resources: telemetry.ResourceMetrics{CPUUserMS: unavailable, CPUSystemMS: unavailable, MaxRSSBytes: unavailable, ReadBytes: unavailable, WriteBytes: unavailable, ProcessCountPeak: unavailable},
	}
	if err := record.Validate(); err != nil {
		t.Fatal(err)
	}
	return record
}

func a4Availability(refs []reprocore.ReferenceDescriptor, kind string) reprocore.AvailabilityState {
	for _, ref := range refs {
		if ref.RecordKind == kind {
			return ref.OriginalAvailability
		}
	}
	return ""
}

func readA4DerivedTree(t *testing.T, root string) []byte {
	t.Helper()
	var out strings.Builder
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out.Write(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return []byte(out.String())
}

func assertA4SecretsAbsent(t *testing.T, data []byte, forbidden ...string) {
	t.Helper()
	for _, secret := range forbidden {
		if strings.Contains(string(data), secret) {
			t.Fatalf("A4 derived/output leaked %q: %s", secret, data)
		}
	}
}
