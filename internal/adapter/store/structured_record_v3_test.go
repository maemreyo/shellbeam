package store

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

func TestStructuredRecordSetV3AllowsMixedV2AndV3RecordsAndExactReplay(t *testing.T) {
	r := openStructuredRepository(t)
	seedStructuredExcerptSourceSession(t, r)
	pending := structuredDerivation(t, 1, core.LifecyclePending, "", core.CompletenessUnavailable)
	processing := pending
	processing.Lifecycle = core.LifecycleProcessing
	if err := r.PutDerivation(context.Background(), pending); err != nil {
		t.Fatal(err)
	}
	if err := r.PutDerivation(context.Background(), processing); err != nil {
		t.Fatal(err)
	}

	pass := structuredTestCaseRecord(processing, core.SchemaVersion, "pass", core.TestPassed, nil)
	fail := structuredTestCaseRecord(processing, core.RecordSchemaVersionV3, "fail", core.TestFailed, &core.FailureExcerpt{Namespace: "jest", VocabularyVersion: 1, Text: "failure at src/a.ts:12"})
	records := []core.Record{pass, fail}
	if err := r.PutRecords(context.Background(), processing.DerivationKey, records); err != nil {
		t.Fatalf("mixed v2/v3 record set: %v", err)
	}
	if err := r.PutRecords(context.Background(), processing.DerivationKey, records); err != nil {
		t.Fatalf("exact mixed v3 replay: %v", err)
	}

	raw, err := os.ReadFile(r.recordPath(processing.DerivationKey))
	if err != nil {
		t.Fatal(err)
	}
	var set structuredRecordSet
	if err := json.Unmarshal(raw, &set); err != nil {
		t.Fatal(err)
	}
	if set.SchemaVersion != core.RecordSchemaVersionV3 || len(set.Records) != 2 || set.Records[0].SchemaVersion != core.SchemaVersion || set.Records[1].SchemaVersion != core.RecordSchemaVersionV3 {
		t.Fatalf("persisted mixed set=%#v", set)
	}
	listed, err := r.ListRecords(context.Background(), processing.DerivationKey, structuredapp.RecordQuery{Offset: 0, Limit: 10})
	if err != nil || !reflect.DeepEqual(listed, records) {
		t.Fatalf("listed=%#v err=%v want=%#v", listed, err, records)
	}
}

func TestStructuredRecordSetV3StrictDecodeAndClosedMixedVersionRules(t *testing.T) {
	d := structuredDerivation(t, 1, core.LifecycleProcessing, "", core.CompletenessUnavailable)
	pass := structuredTestCaseRecord(d, core.SchemaVersion, "pass", core.TestPassed, nil)
	fail := structuredTestCaseRecord(d, core.RecordSchemaVersionV3, "fail", core.TestFailed, &core.FailureExcerpt{Namespace: "jest", VocabularyVersion: 1, Text: "failure"})
	valid := structuredRecordSet{SchemaVersion: core.RecordSchemaVersionV3, DerivationKey: d.DerivationKey, Records: []core.Record{pass, fail}}
	raw, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeStructuredRecordSet(raw, d)
	if err != nil || !reflect.DeepEqual(decoded, valid) {
		t.Fatalf("v3 decode=%#v err=%v", decoded, err)
	}
	withUnknown := append([]byte(nil), raw[:len(raw)-1]...)
	withUnknown = append(withUnknown, []byte(`,"future":true}`)...)
	if _, err := decodeStructuredRecordSet(withUnknown, d); err == nil {
		t.Fatal("v3 record set accepted unknown member")
	}
	if err := validateStructuredRecordSet(structuredRecordSet{SchemaVersion: core.RecordSchemaVersionV3, DerivationKey: d.DerivationKey, Records: []core.Record{pass}}, d); err == nil {
		t.Fatal("v3 record set without any v3 record accepted")
	}
	legacy := pass
	legacy.SchemaVersion = core.SchemaVersionV1
	if err := validateStructuredRecordSet(structuredRecordSet{SchemaVersion: core.RecordSchemaVersionV3, DerivationKey: d.DerivationKey, Records: []core.Record{legacy, fail}}, d); err == nil {
		t.Fatal("v3 record set accepted schema v1 member")
	}
}

func TestStructuredV2RecordSetReadDoesNotRewritePersistedBytes(t *testing.T) {
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
	record := structuredTestCaseRecord(processing, core.SchemaVersion, "pass", core.TestPassed, nil)
	if err := r.PutRecords(context.Background(), processing.DerivationKey, []core.Record{record}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(r.recordPath(processing.DerivationKey))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(before), `"schema_version":2`) {
		t.Fatalf("expected v2 persisted bytes, got %s", before)
	}
	if _, err := r.ListRecords(context.Background(), processing.DerivationKey, structuredapp.RecordQuery{Offset: 0, Limit: 10}); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(r.recordPath(processing.DerivationKey))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("reading v2 record set rewrote persisted bytes")
	}
}

func seedStructuredExcerptSourceSession(t *testing.T, r *Repository) {
	t.Helper()
	res := reservationN(1)
	res.OperationID = "op-1"
	res.SessionID = "session-1"
	reserveOK(t, r, res)
}

func structuredTestCaseRecord(d core.Derivation, schema int, name string, status core.TestStatus, excerpt *core.FailureExcerpt) core.Record {
	return core.Record{
		SchemaVersion: schema, RecordKind: core.RecordTestCase, Authority: core.AuthorityMechanical,
		DerivationMethod: core.DerivationNativeFieldMapping, Producer: d.Producer, OperationID: "op-1", SourceRef: d.SourceAuthorityRefs[0],
		TestCase: &core.TestCase{Name: name, Status: status, FailureExcerpt: excerpt},
	}
}
