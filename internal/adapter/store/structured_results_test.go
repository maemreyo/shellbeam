package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	"github.com/maemreyo/shellbeam/internal/core/observation"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

func TestStructuredRawOutputRefStoreIsIdempotentConflictClosedAndNoFollow(t *testing.T) {
	r := openStructuredRepository(t)
	ref := core.RawOutputRef{SessionID: "session-1", StartByte: 0, EndByte: 3, SHA256: strings.Repeat("a", 64)}
	if err := r.PutRawOutputRef(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	if err := r.PutRawOutputRef(context.Background(), ref); err != nil {
		t.Fatalf("idempotent ref replay: %v", err)
	}
	conflict := ref
	conflict.SHA256 = strings.Repeat("b", 64)
	if err := r.PutRawOutputRef(context.Background(), conflict); err == nil {
		t.Fatal("conflicting raw output ref accepted")
	}
	path := r.rawOutputRefPath(ref.SessionID)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "ref.json")
	if err := os.WriteFile(target, []byte(`{"session_id":"session-1","start_byte":0,"end_byte":3,"sha256":"`+strings.Repeat("a", 64)+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := r.GetRawOutputRef(context.Background(), ref.SessionID); err == nil {
		t.Fatal("symlink raw output ref accepted")
	}
}

func TestStructuredRecordCountIsBounded(t *testing.T) {
	r := openStructuredRepository(t)
	pending := structuredDerivation(t, 1, core.LifecyclePending, "", core.CompletenessUnavailable)
	processing := pending
	processing.Lifecycle = core.LifecycleProcessing
	if err := r.PutDerivation(context.Background(), pending); err != nil {
		t.Fatal(err)
	}
	if err := r.PutDerivation(context.Background(), processing); err != nil {
		t.Fatal(err)
	}
	record := structuredArtifactRecord(pending, core.AuthorityMechanical, "bounded")
	records := make([]core.Record, MaxStructuredRecords+1)
	for i := range records {
		records[i] = record
	}
	if err := r.PutRecords(context.Background(), pending.DerivationKey, records); err == nil {
		t.Fatal("unbounded structured records accepted")
	}
}

func TestStructuredDerivationLifecycleIsMonotonicAndTerminalReplayIsIdempotent(t *testing.T) {
	r := openStructuredRepository(t)
	pending := structuredDerivation(t, 1, core.LifecyclePending, "", core.CompletenessUnavailable)
	if err := r.PutDerivation(context.Background(), pending); err != nil {
		t.Fatal(err)
	}
	terminal := pending
	terminal.Lifecycle, terminal.ParseOutcome, terminal.Completeness = core.LifecycleTerminal, core.ParseComplete, core.CompletenessComplete
	if err := r.PutDerivation(context.Background(), terminal); err == nil {
		t.Fatal("pending skipped processing")
	}
	processing := pending
	processing.Lifecycle = core.LifecycleProcessing
	if err := r.PutDerivation(context.Background(), processing); err != nil {
		t.Fatal(err)
	}
	if err := r.PutDerivation(context.Background(), pending); err == nil {
		t.Fatal("processing regressed to pending")
	}
	if err := r.PutDerivation(context.Background(), terminal); err != nil {
		t.Fatal(err)
	}
	high, err := r.ObservationHighWatermark(context.Background())
	if err != nil || high != 1 {
		t.Fatalf("high=%d err=%v", high, err)
	}
	if err := r.PutDerivation(context.Background(), terminal); err != nil {
		t.Fatal(err)
	}
	high, _ = r.ObservationHighWatermark(context.Background())
	if high != 1 {
		t.Fatalf("terminal replay allocated event sequence: %d", high)
	}
	conflict := terminal
	conflict.ParseOutcome = core.ParsePartial
	if err := r.PutDerivation(context.Background(), conflict); err == nil {
		t.Fatal("terminal conflict accepted")
	}
	listed, err := r.ListObservationObligations(context.Background(), 0, 10)
	if err != nil || len(listed) != 1 || listed[0].Kind != observation.EventStructuredChanged || listed[0].State != observation.ObligationCommitted {
		t.Fatalf("obligations=%#v err=%v", listed, err)
	}
}

func TestStructuredRecordsPersistBoundedSummaryAndCompactToTombstone(t *testing.T) {
	r := openStructuredRepository(t)
	pending := structuredDerivation(t, 1, core.LifecyclePending, "", core.CompletenessUnavailable)
	processing := pending
	processing.Lifecycle = core.LifecycleProcessing
	if err := r.PutDerivation(context.Background(), pending); err != nil {
		t.Fatal(err)
	}
	if err := r.PutDerivation(context.Background(), processing); err != nil {
		t.Fatal(err)
	}
	records := []core.Record{structuredArtifactRecord(pending, core.AuthorityMechanical, "one"), structuredArtifactRecord(pending, core.AuthorityAdvisory, "two")}
	if err := r.PutRecords(context.Background(), pending.DerivationKey, records); err != nil {
		t.Fatal(err)
	}
	got, err := r.ListRecords(context.Background(), pending.DerivationKey, structuredapp.RecordQuery{Offset: 0, Limit: 10})
	if err != nil || len(got) != 2 {
		t.Fatalf("records=%#v err=%v", got, err)
	}
	terminal := processing
	terminal.Lifecycle, terminal.ParseOutcome, terminal.Completeness = core.LifecycleTerminal, core.ParseComplete, core.CompletenessComplete
	if err := r.PutDerivation(context.Background(), terminal); err != nil {
		t.Fatal(err)
	}
	if err := r.PutRecords(context.Background(), pending.DerivationKey, records); err != nil {
		t.Fatalf("terminal exact record replay: %v", err)
	}
	conflicting := append([]core.Record(nil), records...)
	conflicting[0] = structuredArtifactRecord(pending, core.AuthorityMechanical, "changed")
	if err := r.PutRecords(context.Background(), pending.DerivationKey, conflicting); err == nil {
		t.Fatal("terminal conflicting record replay accepted")
	}
	if err := r.CompactRecords(context.Background(), pending.DerivationKey); err != nil {
		t.Fatal(err)
	}
	stored, err := r.GetDerivation(context.Background(), pending.DerivationKey)
	if err != nil || stored.Completeness != core.CompletenessCompacted {
		t.Fatalf("stored=%#v err=%v", stored, err)
	}
	if _, err := r.ListRecords(context.Background(), pending.DerivationKey, structuredapp.RecordQuery{Offset: 0, Limit: 10}); !errors.Is(err, ErrStructuredRecordsCompacted) {
		t.Fatalf("compacted records err=%v", err)
	}
	summary, err := r.readStructuredSummary(pending.DerivationKey)
	if err != nil || !summary.Compacted || summary.RecordCount != 2 || summary.MechanicalCount != 1 || summary.AdvisoryCount != 1 {
		t.Fatalf("summary=%#v err=%v", summary, err)
	}
	high, err := r.ObservationHighWatermark(context.Background())
	if err != nil || high != 2 {
		t.Fatalf("high after terminal+compaction=%d err=%v", high, err)
	}
	if err := r.CompactRecords(context.Background(), pending.DerivationKey); err != nil {
		t.Fatal(err)
	}
	if replayHigh, _ := r.ObservationHighWatermark(context.Background()); replayHigh != high {
		t.Fatalf("compaction replay allocated event sequence: %d -> %d", high, replayHigh)
	}
}

func TestStructuredTerminalObservationPreparedGapReconcilesAfterRestart(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openStructuredRepositoryAt(t, root)
	pending := structuredDerivation(t, 1, core.LifecyclePending, "", core.CompletenessUnavailable)
	processing := pending
	processing.Lifecycle = core.LifecycleProcessing
	if err := r.PutDerivation(context.Background(), pending); err != nil {
		t.Fatal(err)
	}
	if err := r.PutDerivation(context.Background(), processing); err != nil {
		t.Fatal(err)
	}
	terminal := processing
	terminal.Lifecycle, terminal.ParseOutcome, terminal.Completeness = core.LifecycleTerminal, core.ParseComplete, core.CompletenessComplete
	r.writer = failNthAtomicWriter("replace.rename", 2)
	if err := r.PutDerivation(context.Background(), terminal); err != nil {
		t.Fatalf("terminal canonical write failed: %v", err)
	}
	listed, err := r.ListObservationObligations(context.Background(), 0, 10)
	if err != nil || len(listed) != 1 || listed[0].State != observation.ObligationPrepared {
		t.Fatalf("before restart=%#v err=%v", listed, err)
	}
	r = openStructuredRepositoryAt(t, root)
	if err := r.AbandonUnresolved(context.Background(), "restart"); err != nil {
		t.Fatal(err)
	}
	listed, err = r.ListObservationObligations(context.Background(), 0, 10)
	if err != nil || len(listed) != 1 || listed[0].State != observation.ObligationCommitted {
		t.Fatalf("after restart=%#v err=%v", listed, err)
	}
}

func openStructuredRepository(t *testing.T) *Repository {
	return openStructuredRepositoryAt(t, filepath.Join(t.TempDir(), "state"))
}
func openStructuredRepositoryAt(t *testing.T, root string) *Repository {
	t.Helper()
	r, err := Open(root, Limits{MaxSessions: 8, MaxSessionOutput: 1 << 20, MaxTotalState: 16 << 20, ControlReserve: 1024})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func structuredDerivation(t *testing.T, version int, lifecycle core.Lifecycle, outcome core.ParseOutcome, completeness core.Completeness) core.Derivation {
	t.Helper()
	ref := core.RawOutputRef{SessionID: "session-1", StartByte: 0, EndByte: 3, SHA256: strings.Repeat("a", 64)}
	producer := core.Producer{AdapterID: "go-test-json", AdapterVersion: version, CapabilityVersion: 1}
	key, err := core.DerivationKey([]core.RawOutputRef{ref}, producer, 1, strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	return core.Derivation{SchemaVersion: 1, DerivationKey: key, SourceAuthorityRefs: []core.RawOutputRef{ref}, Producer: producer, DerivationSchemaVersion: 1, DerivationConfigDigest: strings.Repeat("b", 64), Lifecycle: lifecycle, ParseOutcome: outcome, Completeness: completeness}
}
func structuredArtifactRecord(d core.Derivation, authority core.Authority, name string) core.Record {
	method := core.DerivationNativeFieldMapping
	if authority == core.AuthorityAdvisory {
		method = core.DerivationHeuristicExtraction
	}
	return core.Record{SchemaVersion: 1, RecordKind: core.RecordArtifactResult, Authority: authority, DerivationMethod: method, Producer: d.Producer, OperationID: "op-1", SourceRef: d.SourceAuthorityRefs[0], ArtifactResult: &core.ArtifactResult{Name: name, Status: "ok"}}
}
func failNthAtomicWriter(point string, nth int) atomicWriter {
	seen := 0
	return atomicWriter{fail: func(got string) error {
		if got == point {
			seen++
			if seen == nth {
				return errors.New("injected structured persistence fault")
			}
		}
		return nil
	}}
}
