package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	trace "github.com/maemreyo/shellbeam/internal/core/inputtrace"
	"github.com/maemreyo/shellbeam/internal/core/observation"
)

func TestE27InputTraceStoreIdempotentConflictClosedInspectAndEventOnce(t *testing.T) {
	r := openInputTraceRepository(t)
	record := inputTraceRecord("op-a", strings.Repeat("a", 64), time.Now().UTC(), false)
	if err := r.PutInputTraceRecord(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	if err := r.PutInputTraceRecord(context.Background(), record); err != nil {
		t.Fatalf("replay: %v", err)
	}
	got, ok, err := r.LoadInputTraceByOperation(context.Background(), record.OperationID)
	if err != nil || !ok || got.DerivationKey != record.DerivationKey {
		t.Fatalf("got=%#v ok=%v err=%v", got, ok, err)
	}
	inspection, err := r.InspectInputTrace(context.Background(), record.OperationID)
	if err != nil || inspection.Status != "available" || inspection.Record == nil {
		t.Fatalf("inspection=%#v err=%v", inspection, err)
	}
	high, _ := r.ObservationHighWatermark(context.Background())
	if high != 1 {
		t.Fatalf("high=%d", high)
	}
	obs, err := r.ListObservationObligations(context.Background(), 0, 10)
	if err != nil || len(obs) != 1 || obs[0].Kind != observation.EventInputTraceRecorded || obs[0].State != observation.ObligationCommitted {
		t.Fatalf("obs=%#v err=%v", obs, err)
	}
	if strings.Contains(obs[0].Summary, "internal/") || strings.Contains(obs[0].Summary, "dep.go") {
		t.Fatalf("event leaked resource path: %#v", obs[0])
	}
	conflict := record
	conflict.Outcome = trace.OutcomeUnavailable
	if err := r.PutInputTraceRecord(context.Background(), conflict); err == nil || !strings.Contains(err.Error(), "input_trace_record_conflict") {
		t.Fatalf("conflict err=%v", err)
	}
	if next, _ := r.ObservationHighWatermark(context.Background()); next != high {
		t.Fatalf("conflict allocated event %d->%d", high, next)
	}
}

func TestE27InputTraceTruncatedUsesDistinctSmallEvent(t *testing.T) {
	r := openInputTraceRepository(t)
	record := inputTraceRecord("op-trunc", strings.Repeat("b", 64), time.Now().UTC(), true)
	if err := r.PutInputTraceRecord(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	obs, _ := r.ListObservationObligations(context.Background(), 0, 10)
	if len(obs) != 1 || obs[0].Kind != observation.EventInputTraceTruncated {
		t.Fatalf("obs=%#v", obs)
	}
}

func TestE27InputTraceEventCommitFailureDoesNotRollBackTruthAndReconciles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openInputTraceRepositoryAt(t, root)
	failed := false
	r.writer.fail = func(point string) error {
		if point == "replace.rename" && !failed {
			failed = true
			return errors.New("event commit fault")
		}
		return nil
	}
	record := inputTraceRecord("op-fault", strings.Repeat("c", 64), time.Now().UTC(), false)
	if err := r.PutInputTraceRecord(context.Background(), record); err != nil {
		t.Fatalf("canonical trace changed by event failure: %v", err)
	}
	got, ok, err := r.LoadInputTraceByOperation(context.Background(), record.OperationID)
	if err != nil || !ok || got.DerivationKey != record.DerivationKey {
		t.Fatalf("truth=%#v ok=%v err=%v", got, ok, err)
	}
	r.writer.fail = nil
	reopened := openInputTraceRepositoryAt(t, root)
	if err := reopened.reconcilePreparedExecutionObservations(context.Background()); err != nil {
		t.Fatal(err)
	}
	obs, err := reopened.ListObservationObligations(context.Background(), 0, 10)
	if err != nil || len(obs) != 1 || obs[0].State != observation.ObligationCommitted {
		t.Fatalf("reconciled=%#v err=%v", obs, err)
	}
}

func TestE27InputTraceRetentionEvictsOldestPast128(t *testing.T) {
	r := openInputTraceRepository(t)
	base := time.Now().Add(-time.Hour).UTC()
	var first trace.Record
	for i := 0; i < trace.MaxRetainedTraceRecords+1; i++ {
		key := fmt.Sprintf("%064x", i+1)
		record := inputTraceRecord(fmt.Sprintf("op-%d", i), key, base.Add(time.Duration(i)*time.Second), false)
		if i == 0 {
			first = record
		}
		if err := r.PutInputTraceRecord(context.Background(), record); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.GetInputTraceRecord(context.Background(), first.DerivationKey); !errors.Is(err, ErrNotFound) {
		t.Fatalf("oldest retained err=%v", err)
	}
}

func openInputTraceRepository(t *testing.T) *Repository {
	return openInputTraceRepositoryAt(t, filepath.Join(t.TempDir(), "state"))
}
func openInputTraceRepositoryAt(t *testing.T, root string) *Repository {
	t.Helper()
	r, err := Open(root, Limits{MaxSessions: 4, MaxSessionOutput: 1 << 20, MaxTotalState: 64 << 20, ControlReserve: 4096})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func inputTraceRecord(op, key string, at time.Time, truncated bool) trace.Record {
	outcome := trace.OutcomePartial
	return trace.Record{SchemaVersion: trace.SchemaVersion, DerivationKey: key, TraceID: "trace_01K00000000000000000000000", OperationID: op, SessionID: op + "-session", ReceiptDigest: strings.Repeat("d", 64), Mode: trace.ModeBestEffort, Provider: trace.ProviderIdentity{ID: "dyld-interpose", Version: 1, CapabilityVersion: 1}, Platform: "darwin", InstrumentationFingerprint: strings.Repeat("e", 64), InstrumentationEffect: trace.EffectEnvironmentAffecting, Authority: trace.AuthorityAdvisory, ScopeKind: trace.ScopeObservedInput, MayHaveUnobservedDependencies: true, CaptureStart: at.Add(-time.Second), CaptureEnd: at, Coverage: trace.CoverageMatrix{FilesystemReads: trace.CoveragePartial, FilesystemMetadataQueries: trace.CoveragePartial, DirectoryEnumerations: trace.CoveragePartial, FilesystemWrites: trace.CoveragePartial, ExecutedBinaries: trace.CoveragePartial, LoadedLibraries: trace.CoveragePartial, EnvironmentNamesObserved: trace.CoverageUnsupported, NetworkAttempts: trace.CoverageUnsupported, ChildProcesses: trace.CoveragePartial}, Outcome: outcome, Truncated: truncated, Resources: []trace.Resource{{ObservationClass: trace.ClassFilesystemReads, PathClass: trace.PathRepoRelative, Identity: "internal/dep.go"}}, Summary: trace.Summary{ResourcesReturned: 1, ResourcesObserved: 1}}
}
