package telemetry

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
	core "github.com/maemreyo/shellbeam/internal/core/telemetry"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type memoryTelemetryRepository struct {
	mu          sync.Mutex
	reservation operation.Reservation
	snapshot    session.Snapshot
	receipt     receipt.Receipt
	records     map[string]core.PerformanceRecord
	putCalls    int
	putErr      error
	putCh       chan core.PerformanceRecord
	loadBlock   <-chan struct{}
	loadStarted chan struct{}
	loadOnce    sync.Once
}

func (r *memoryTelemetryRepository) LoadOperation(ctx context.Context, id operation.ID) (operation.Reservation, error) {
	if err := ctx.Err(); err != nil {
		return operation.Reservation{}, err
	}
	if id != r.reservation.OperationID {
		return operation.Reservation{}, errors.New("not found")
	}
	return r.reservation, nil
}

func (r *memoryTelemetryRepository) LoadSession(ctx context.Context, id operation.SessionID) (session.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return session.Snapshot{}, err
	}
	if id != operation.SessionID(r.snapshot.SessionID) {
		return session.Snapshot{}, errors.New("not found")
	}
	return r.snapshot, nil
}

func (r *memoryTelemetryRepository) LoadReceipt(ctx context.Context, id operation.SessionID) (receipt.Receipt, error) {
	if r.loadStarted != nil {
		r.loadOnce.Do(func() { close(r.loadStarted) })
	}
	if r.loadBlock != nil {
		select {
		case <-r.loadBlock:
		case <-ctx.Done():
			return receipt.Receipt{}, ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return receipt.Receipt{}, err
	}
	if id != operation.SessionID(r.receipt.SessionID) {
		return receipt.Receipt{}, errors.New("not found")
	}
	return r.receipt, nil
}

func (r *memoryTelemetryRepository) PutPerformanceRecord(ctx context.Context, record core.PerformanceRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.putErr != nil {
		return r.putErr
	}
	if err := record.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.putCalls++
	if r.records == nil {
		r.records = map[string]core.PerformanceRecord{}
	}
	if current, ok := r.records[record.DerivationKey]; ok && !reflect.DeepEqual(current, record) {
		return errors.New("telemetry_record_conflict")
	}
	r.records[record.DerivationKey] = record
	if r.putCh != nil {
		select {
		case r.putCh <- record:
		default:
		}
	}
	return nil
}

func TestServiceDerivesTerminalTelemetryFromDurableAuthorities(t *testing.T) {
	created := time.Date(2026, 8, 15, 1, 0, 0, 0, time.UTC)
	terminal := created.Add(1500 * time.Millisecond)
	repo := telemetryFixture(created, terminal, session.Completed, session.Success)
	service, err := newService(repo, "darwin", "arm64")
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DeriveTerminal(context.Background(), repo.receipt)
	if err != nil {
		t.Fatal(err)
	}
	if got.WallMS != 1500 || got.OutputBytes != repo.receipt.OutputBytes || got.InputAcceptedBytes != repo.receipt.InputAcceptedBytes || got.InputDeliveredBytes != repo.receipt.InputDeliveredBytes {
		t.Fatalf("execution counters=%#v", got)
	}
	if got.TerminalOutcome != session.Success || got.TimedOut || !got.CapturedAt.Equal(terminal) || got.Lifecycle != core.LifecycleTerminal || got.Completeness != core.CompletenessPartial {
		t.Fatalf("terminal facts=%#v", got)
	}
	if got.CommandSemanticsFingerprint != repo.receipt.ExecutionFingerprint || got.ScopeClass != core.ScopeArgv || got.Platform != "darwin" || got.Architecture != "arm64" {
		t.Fatalf("compatibility facts=%#v", got)
	}
	if got.RepositoryID != "repo_01K00000000000000000000000" || got.WorkspaceID != "ws_01K00000000000000000000000" || got.ActivityID != "activity-1" {
		t.Fatalf("correlation=%#v", got)
	}
	for name, metric := range map[string]core.IntMetric{
		"cpu_user": got.Resources.CPUUserMS, "cpu_system": got.Resources.CPUSystemMS, "rss": got.Resources.MaxRSSBytes,
		"read": got.Resources.ReadBytes, "write": got.Resources.WriteBytes, "process_peak": got.Resources.ProcessCountPeak,
	} {
		if metric.Quality != core.MetricUnavailable || metric.Value != nil {
			t.Fatalf("resource %s overclaimed: %#v", name, metric)
		}
	}
	digest, err := receipt.Digest(repo.receipt)
	if err != nil || got.ReceiptDigest != digest {
		t.Fatalf("receipt digest got=%q want=%q err=%v", got.ReceiptDigest, digest, err)
	}
	if repo.putCalls != 1 || len(repo.records) != 1 {
		t.Fatalf("puts=%d records=%d", repo.putCalls, len(repo.records))
	}
}

func TestServiceDerivationIsDeterministicAndClampsNegativeWallDuration(t *testing.T) {
	created := time.Date(2026, 8, 15, 1, 0, 1, 0, time.UTC)
	terminal := created.Add(-time.Second)
	repo := telemetryFixture(created, terminal, session.TimedOut, session.Timeout)
	service, err := newService(repo, "darwin", "arm64")
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.DeriveTerminal(context.Background(), repo.receipt)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.DeriveTerminal(context.Background(), repo.receipt)
	if err != nil {
		t.Fatal(err)
	}
	if first.DerivationKey != second.DerivationKey || first.ReceiptDigest != second.ReceiptDigest || first.WallMS != 0 || !first.TimedOut {
		t.Fatalf("first=%#v second=%#v", first, second)
	}
	if len(repo.records) != 1 || repo.putCalls != 2 {
		t.Fatalf("logical samples=%d put calls=%d", len(repo.records), repo.putCalls)
	}
}

func TestServiceUsesReceiptWorkspaceAuthorityAndOmitsIncompleteBinding(t *testing.T) {
	repo := telemetryFixture(time.Now().UTC().Add(-time.Second), time.Now().UTC(), session.Completed, session.Success)
	repo.reservation.WorkspaceID = "ws_01K00000000000000000000099"
	service, err := newService(repo, "darwin", "arm64")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DeriveTerminal(context.Background(), repo.receipt); err == nil {
		t.Fatal("reservation/receipt workspace authority mismatch accepted")
	}

	repo = telemetryFixture(time.Now().UTC().Add(-time.Second), time.Now().UTC(), session.Completed, session.Success)
	repo.reservation.WorkspaceID = ""
	repo.receipt.WorkspaceProvenance = &receipt.WorkspaceProvenance{
		SchemaVersion: 2,
		Binding:       receipt.WorkspaceBinding{WorkspaceID: workspace.WorkspaceID("ws_01K00000000000000000000000")},
		Pre:           receipt.WorkspaceObservationRef{Kind: receipt.WorkspaceUnreconciled},
		Post:          receipt.WorkspaceObservationRef{Kind: receipt.WorkspaceUnreconciled},
	}
	// An incomplete but valid receipt binding cannot become grouping authority.
	service, _ = newService(repo, "darwin", "arm64")
	got, err := service.DeriveTerminal(context.Background(), repo.receipt)
	if err != nil {
		t.Fatal(err)
	}
	if got.RepositoryID != "" || got.WorkspaceID != "" {
		t.Fatalf("incomplete binding became grouping authority: %#v", got)
	}
}

func TestServiceRejectsNonAuthoritativeOrChangedTerminalInputs(t *testing.T) {
	repo := telemetryFixture(time.Now().UTC().Add(-time.Second), time.Now().UTC(), session.Completed, session.Success)
	service, _ := newService(repo, "darwin", "arm64")
	for name, mutate := range map[string]func(*memoryTelemetryRepository, *receipt.Receipt){
		"scheduled receipt differs": func(r *memoryTelemetryRepository, scheduled *receipt.Receipt) { scheduled.OutputBytes++ },
		"reservation execution differs": func(r *memoryTelemetryRepository, _ *receipt.Receipt) {
			r.reservation.ExecutionFingerprint = strings.Repeat("d", 64)
		},
		"session not terminal": func(r *memoryTelemetryRepository, _ *receipt.Receipt) {
			r.snapshot.State, r.snapshot.Outcome = session.Running, session.NoOutcome
		},
	} {
		t.Run(name, func(t *testing.T) {
			copyRepo := *repo
			copyRepo.records = map[string]core.PerformanceRecord{}
			scheduled := copyRepo.receipt
			mutate(&copyRepo, &scheduled)
			local, _ := newService(&copyRepo, "darwin", "arm64")
			if _, err := local.DeriveTerminal(context.Background(), scheduled); err == nil {
				t.Fatal("non-authoritative terminal input accepted")
			}
		})
	}
	_ = service
}

func TestServiceStoreFailureDoesNotMutateReceipt(t *testing.T) {
	repo := telemetryFixture(time.Now().UTC().Add(-time.Second), time.Now().UTC(), session.Completed, session.Success)
	original := repo.receipt
	repo.putErr = errors.New("telemetry unavailable")
	service, _ := newService(repo, "darwin", "arm64")
	if _, err := service.DeriveTerminal(context.Background(), repo.receipt); err == nil {
		t.Fatal("store failure hidden")
	}
	if !reflect.DeepEqual(repo.receipt, original) {
		t.Fatalf("receipt mutated: got=%#v want=%#v", repo.receipt, original)
	}
}

func telemetryFixture(created, terminal time.Time, state session.State, outcome session.Outcome) *memoryTelemetryRepository {
	code := 0
	execution := strings.Repeat("c", 64)
	workspaceID := "ws_01K00000000000000000000000"
	provenance := &receipt.WorkspaceProvenance{
		SchemaVersion: 2,
		Binding:       receipt.WorkspaceBinding{RepositoryID: workspace.RepositoryID("repo_01K00000000000000000000000"), WorkspaceID: workspace.WorkspaceID(workspaceID)},
		Pre:           receipt.WorkspaceObservationRef{Kind: receipt.WorkspaceUnreconciled},
		Post:          receipt.WorkspaceObservationRef{Kind: receipt.WorkspaceUnreconciled},
	}
	rec := receipt.Receipt{
		SchemaVersion: 2, OperationID: "op-telemetry", SessionID: "session-telemetry",
		RequestFingerprint: "request", ExecutionFingerprint: execution, ObservationBindingFingerprint: "observation",
		DaemonIncarnation: "daemon", ExecutionMode: string(operation.ExecutionModeArgv), Executable: "/usr/bin/true",
		State: state, Outcome: outcome, CWD: "/tmp", OutputBytes: 123, OutputComplete: true,
		InputAcceptedBytes: 4, InputDeliveredBytes: 4, WorkspaceProvenance: provenance,
		Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}, Exit: receipt.ExitEvidence{Reaped: true, Code: &code},
	}
	if state != session.Completed || outcome != session.Success {
		rec.Exit.Code = nil
		rec.Exit.Signal = "TERM"
	}
	return &memoryTelemetryRepository{
		reservation: operation.Reservation{
			SchemaVersion: 2, OperationID: operation.ID(rec.OperationID), ActivityID: "activity-1", WorkspaceID: workspaceID,
			SessionID: operation.SessionID(rec.SessionID), RequestFingerprint: rec.RequestFingerprint, ExecutionFingerprint: execution,
			ObservationBindingFingerprint: rec.ObservationBindingFingerprint, ExecutionMode: operation.ExecutionModeArgv,
			Executable: rec.Executable, CWD: rec.CWD, DaemonIncarnation: rec.DaemonIncarnation, CreatedAt: created,
		},
		snapshot: session.Snapshot{SchemaVersion: 1, OperationID: rec.OperationID, SessionID: rec.SessionID, DaemonIncarnation: rec.DaemonIncarnation, State: state, Outcome: outcome, OutputBytes: rec.OutputBytes, OutputAvailable: true, UpdatedAt: terminal},
		receipt:  rec, records: map[string]core.PerformanceRecord{},
	}
}
