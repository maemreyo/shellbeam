package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

func TestTerminalRetentionStripsFailureExcerptBeforeCollectingRawOutput(t *testing.T) {
	r, expired, key := seedRetainedFailureExcerpt(t)
	if markers := failureExcerptMarkersForSession(t, r, expired.SessionID); len(markers) != 1 {
		t.Fatalf("retention markers before sweep=%v, want 1", markers)
	}

	report, err := r.CollectExpiredTerminals(context.Background(), RetentionPolicy{TerminalRetention: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if report.Collected != 1 {
		t.Fatalf("collected=%d want 1", report.Collected)
	}
	if sessionExists(t, r, expired.SessionID) {
		t.Fatal("expired raw-output session survived retention")
	}
	if markers := failureExcerptMarkersForSession(t, r, expired.SessionID); len(markers) != 0 {
		t.Fatalf("retention markers survived sweep: %v", markers)
	}

	records, err := r.ListRecords(context.Background(), key, structuredapp.RecordQuery{Offset: 0, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].SchemaVersion != core.SchemaVersion || records[0].TestCase == nil || records[0].TestCase.FailureExcerpt != nil {
		t.Fatalf("records after retention=%#v", records)
	}
	out, _, err := r.ReadOutput(context.Background(), expired.SessionID, 0, 1024)
	if err != nil || len(out) != 0 {
		t.Fatalf("raw output still readable after collection: %q err=%v", out, err)
	}
}

func TestFailureExcerptWriteIsRejectedAfterSourceSessionRetention(t *testing.T) {
	r := retentionRepository(t)
	expired := seedTerminal(t, r, 0, 48*time.Hour)
	ref := core.RawOutputRef{SessionID: string(expired.SessionID), StartByte: 0, EndByte: int64(len("output\n")), SHA256: strings.Repeat("a", 64)}
	producer := core.Producer{AdapterID: "jest-json", AdapterVersion: 1, CapabilityVersion: 1}
	config := strings.Repeat("c", 64)
	key, err := core.DerivationKey([]core.RawOutputRef{ref}, producer, 1, config)
	if err != nil {
		t.Fatal(err)
	}
	pending := core.Derivation{SchemaVersion: core.SchemaVersion, DerivationKey: key, SourceAuthorityRefs: []core.StructuredInputRef{core.RawInputRef(ref)}, Producer: producer, DerivationSchemaVersion: 1, DerivationConfigDigest: config, Lifecycle: core.LifecyclePending, Completeness: core.CompletenessUnavailable}
	if err := r.PutDerivation(context.Background(), pending); err != nil {
		t.Fatal(err)
	}
	processing := pending
	processing.Lifecycle = core.LifecycleProcessing
	if err := r.PutDerivation(context.Background(), processing); err != nil {
		t.Fatal(err)
	}
	if report, err := r.CollectExpiredTerminals(context.Background(), RetentionPolicy{TerminalRetention: time.Hour}); err != nil || report.Collected != 1 {
		t.Fatalf("retention report=%#v err=%v", report, err)
	}
	record := core.Record{
		SchemaVersion: core.RecordSchemaVersionV3, RecordKind: core.RecordTestCase,
		Authority: core.AuthorityMechanical, DerivationMethod: core.DerivationNativeFieldMapping,
		Producer: producer, OperationID: string(expired.OperationID), SourceRef: core.RawInputRef(ref),
		TestCase: &core.TestCase{Name: "late failure", Status: core.TestFailed, FailureExcerpt: &core.FailureExcerpt{Namespace: "jest", VocabularyVersion: 1, Text: "failure"}},
	}
	if err := r.PutRecords(context.Background(), key, []core.Record{record}); err == nil {
		t.Fatal("failure excerpt became visible after source session retention")
	}
	if markers := failureExcerptMarkersForSession(t, r, expired.SessionID); len(markers) != 0 {
		t.Fatalf("late failure excerpt marker became durable: %v", markers)
	}
	if _, err := r.ListRecords(context.Background(), key, structuredapp.RecordQuery{Offset: 0, Limit: 10}); err != nil {
		t.Fatal(err)
	} else if _, statErr := os.Stat(r.recordPath(key)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("late failure excerpt record became durable: %v", statErr)
	}
}

func TestTerminalRetentionBlocksCollectionWhenFailureExcerptStripFails(t *testing.T) {
	r, expired, key := seedRetainedFailureExcerpt(t)
	r.writer.fail = func(point string) error {
		if point == "replace.rename" {
			return errors.New("injected excerpt strip failure")
		}
		return nil
	}

	if _, err := r.CollectExpiredTerminals(context.Background(), RetentionPolicy{TerminalRetention: time.Hour}); err == nil {
		t.Fatal("retention collected session despite excerpt strip failure")
	}
	if !sessionExists(t, r, expired.SessionID) {
		t.Fatal("session was collected after excerpt strip failure")
	}
	out, _, err := r.ReadOutput(context.Background(), expired.SessionID, 0, 1024)
	if err != nil || string(out) != "output\n" {
		t.Fatalf("raw output after failed sweep=%q err=%v", out, err)
	}
	records, err := r.ListRecords(context.Background(), key, structuredapp.RecordQuery{Offset: 0, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].TestCase == nil || records[0].TestCase.FailureExcerpt == nil || records[0].SchemaVersion != core.RecordSchemaVersionV3 {
		t.Fatalf("excerpt was partially retired on failed sweep: %#v", records)
	}
	if markers := failureExcerptMarkersForSession(t, r, expired.SessionID); len(markers) != 1 {
		t.Fatalf("retention marker lost after failed strip: %v", markers)
	}
}

func seedRetainedFailureExcerpt(t *testing.T) (*Repository, operation.Reservation, string) {
	t.Helper()
	r := retentionRepository(t)
	expired := seedTerminal(t, r, 0, 48*time.Hour)
	ref := core.RawOutputRef{SessionID: string(expired.SessionID), StartByte: 0, EndByte: int64(len("output\n")), SHA256: strings.Repeat("a", 64)}
	producer := core.Producer{AdapterID: "jest-json", AdapterVersion: 1, CapabilityVersion: 1}
	config := strings.Repeat("b", 64)
	key, err := core.DerivationKey([]core.RawOutputRef{ref}, producer, 1, config)
	if err != nil {
		t.Fatal(err)
	}
	pending := core.Derivation{
		SchemaVersion: core.SchemaVersion, DerivationKey: key,
		SourceAuthorityRefs: []core.StructuredInputRef{core.RawInputRef(ref)}, Producer: producer,
		DerivationSchemaVersion: 1, DerivationConfigDigest: config,
		Lifecycle: core.LifecyclePending, Completeness: core.CompletenessUnavailable,
	}
	if err := r.PutDerivation(context.Background(), pending); err != nil {
		t.Fatal(err)
	}
	processing := pending
	processing.Lifecycle = core.LifecycleProcessing
	if err := r.PutDerivation(context.Background(), processing); err != nil {
		t.Fatal(err)
	}
	record := core.Record{
		SchemaVersion: core.RecordSchemaVersionV3, RecordKind: core.RecordTestCase,
		Authority: core.AuthorityMechanical, DerivationMethod: core.DerivationNativeFieldMapping,
		Producer: producer, OperationID: string(expired.OperationID), SourceRef: core.RawInputRef(ref),
		TestCase: &core.TestCase{Name: "fails", Status: core.TestFailed, FailureExcerpt: &core.FailureExcerpt{Namespace: "jest", VocabularyVersion: 1, Text: "failure"}},
	}
	if err := r.PutRecords(context.Background(), key, []core.Record{record}); err != nil {
		t.Fatal(err)
	}
	terminal := processing
	terminal.Lifecycle = core.LifecycleTerminal
	terminal.ParseOutcome = core.ParseComplete
	terminal.Completeness = core.CompletenessComplete
	if err := r.PutDerivation(context.Background(), terminal); err != nil {
		t.Fatal(err)
	}
	return r, expired, key
}

func failureExcerptMarkersForSession(t *testing.T, r *Repository, sessionID operation.SessionID) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(r.structuredRoot(), "failure-excerpt-retention", string(sessionID), "*", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	return matches
}
