package structuredresult

import (
	"context"
	"strings"
	"testing"

	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

type resultRepoFake struct {
	derivations map[string]core.Derivation
	records     map[string][]core.Record
}

func newResultRepoFake() *resultRepoFake {
	return &resultRepoFake{derivations: map[string]core.Derivation{}, records: map[string][]core.Record{}}
}
func (f *resultRepoFake) PutDerivation(_ context.Context, d core.Derivation) error {
	f.derivations[d.DerivationKey] = d
	return nil
}
func (f *resultRepoFake) GetDerivation(_ context.Context, key string) (core.Derivation, error) {
	return f.derivations[key], nil
}
func (f *resultRepoFake) PutRecords(_ context.Context, key string, records []core.Record) error {
	f.records[key] = append([]core.Record(nil), records...)
	return nil
}
func (f *resultRepoFake) ListRecords(_ context.Context, key string, q RecordQuery) ([]core.Record, error) {
	return append([]core.Record(nil), f.records[key]...), nil
}
func (f *resultRepoFake) CompactRecords(context.Context, string) error { return nil }

func TestStructuredServiceBuildsDeterministicMonotonicDerivation(t *testing.T) {
	repo := newResultRepoFake()
	svc := New(repo)
	ref := core.RawOutputRef{SessionID: "session-1", StartByte: 0, EndByte: 3, SHA256: strings.Repeat("a", 64)}
	producer := core.Producer{AdapterID: "go-test-json", AdapterVersion: 1, CapabilityVersion: 1}
	pending, err := svc.Begin(context.Background(), []core.StructuredInputRef{core.RawInputRef(ref)}, producer, 1, strings.Repeat("b", 64))
	if err != nil || pending.Lifecycle != core.LifecyclePending {
		t.Fatalf("pending=%#v err=%v", pending, err)
	}
	processing, err := svc.MarkProcessing(context.Background(), pending.DerivationKey)
	if err != nil || processing.Lifecycle != core.LifecycleProcessing {
		t.Fatalf("processing=%#v err=%v", processing, err)
	}
	terminal, err := svc.Complete(context.Background(), pending.DerivationKey, core.ParseComplete, core.CompletenessComplete, nil)
	if err != nil || terminal.Lifecycle != core.LifecycleTerminal || terminal.ParseOutcome != core.ParseComplete {
		t.Fatalf("terminal=%#v err=%v", terminal, err)
	}
	changed, err := svc.Begin(context.Background(), []core.StructuredInputRef{core.RawInputRef(ref)}, core.Producer{AdapterID: "go-test-json", AdapterVersion: 2, CapabilityVersion: 1}, 1, strings.Repeat("b", 64))
	if err != nil || changed.DerivationKey == pending.DerivationKey {
		t.Fatalf("producer version reused key changed=%#v err=%v", changed, err)
	}
}

func TestStructuredServicePersistsTerminalMetadataAsDerivationV3AndCopiesInputs(t *testing.T) {
	repo := newResultRepoFake()
	svc := New(repo)
	ref := core.RawOutputRef{SessionID: "session-metadata", StartByte: 0, EndByte: 3, SHA256: strings.Repeat("c", 64)}
	producer := core.Producer{AdapterID: "jest-json", AdapterVersion: 1, CapabilityVersion: 1}
	pending, err := svc.Begin(context.Background(), []core.StructuredInputRef{core.RawInputRef(ref)}, producer, 1, strings.Repeat("d", 64))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.MarkProcessing(context.Background(), pending.DerivationKey); err != nil {
		t.Fatal(err)
	}
	coverage := &core.ProducerSemanticsCoverage{Namespace: "jest", VocabularyVersion: 1, Format: "json", Family: "v30", MechanicallyObservable: []string{"coarse:fail"}, Unavailable: []string{"jest:error_status"}}
	counts := &core.ObservedEntryCounts{Namespace: "jest", VocabularyVersion: 1, Files: 2, Entries: 2, Pass: 1, Fail: 1}
	terminal, err := svc.CompleteWithMetadata(context.Background(), pending.DerivationKey, core.ParsePartial, core.CompletenessPartial, nil, TerminalMetadata{
		CompletenessReason: core.CompletenessReasonPassRecordsElided,
		ObservedEntries:    counts,
		SemanticsCoverage:  coverage,
	})
	if err != nil {
		t.Fatal(err)
	}
	if terminal.SchemaVersion != core.DerivationSchemaVersionV3 || terminal.CompletenessReason != core.CompletenessReasonPassRecordsElided || terminal.ObservedEntries == nil || terminal.SemanticsCoverage == nil {
		t.Fatalf("terminal metadata=%#v", terminal)
	}
	counts.Fail = 0
	coverage.MechanicallyObservable[0] = "changed"
	stored := repo.derivations[pending.DerivationKey]
	if stored.ObservedEntries == nil || stored.ObservedEntries.Fail != 1 || stored.SemanticsCoverage == nil || stored.SemanticsCoverage.MechanicallyObservable[0] != "coarse:fail" {
		t.Fatalf("terminal metadata aliases caller inputs: %#v", stored)
	}
}

func TestStructuredServiceExistingCompletionWithoutV3MetadataStaysV2(t *testing.T) {
	repo := newResultRepoFake()
	svc := New(repo)
	ref := core.RawOutputRef{SessionID: "session-v2-complete", StartByte: 0, EndByte: 3, SHA256: strings.Repeat("e", 64)}
	producer := core.Producer{AdapterID: "go-test-json", AdapterVersion: 1, CapabilityVersion: 1}
	pending, err := svc.Begin(context.Background(), []core.StructuredInputRef{core.RawInputRef(ref)}, producer, 1, strings.Repeat("f", 64))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.MarkProcessing(context.Background(), pending.DerivationKey); err != nil {
		t.Fatal(err)
	}
	terminal, err := svc.Complete(context.Background(), pending.DerivationKey, core.ParseComplete, core.CompletenessComplete, nil)
	if err != nil {
		t.Fatal(err)
	}
	if terminal.SchemaVersion != core.SchemaVersion || terminal.CompletenessReason != "" || terminal.ObservedEntries != nil {
		t.Fatalf("existing completion changed persisted schema: %#v", terminal)
	}
}
