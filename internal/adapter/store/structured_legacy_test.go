package store

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

func TestStructuredLegacyV1NormalizesRawSourcesWithoutRewritingPersistedBytes(t *testing.T) {
	r := openStructuredRepository(t)
	raw := core.RawOutputRef{SessionID: "legacy-session", StartByte: 2, EndByte: 9, SHA256: strings.Repeat("a", 64)}
	producer := core.Producer{AdapterID: "go-test-json", AdapterVersion: 1, CapabilityVersion: 1}
	config := strings.Repeat("b", 64)
	key, err := core.DerivationKey([]core.RawOutputRef{raw}, producer, 1, config)
	if err != nil {
		t.Fatal(err)
	}
	derivationJSON := fmt.Sprintf(`{"schema_version":1,"derivation_key":%q,"source_authority_refs":[{"session_id":%q,"start_byte":2,"end_byte":9,"sha256":%q}],"producer":{"adapter_id":"go-test-json","adapter_version":1,"capability_version":1},"derivation_schema_version":1,"derivation_config_digest":%q,"lifecycle":"terminal","parse_outcome":"complete","completeness":"complete"}`+"\n", key, raw.SessionID, raw.SHA256, config)
	recordJSON := fmt.Sprintf(`{"schema_version":1,"derivation_key":%q,"records":[{"schema_version":1,"record_kind":"artifact_result","authority":"mechanical","derivation_method":"native_field_mapping","producer":{"adapter_id":"go-test-json","adapter_version":1,"capability_version":1},"operation_id":"legacy-op","source_ref":{"session_id":%q,"start_byte":2,"end_byte":9,"sha256":%q},"artifact_result":{"name":"legacy","status":"ok"}}]}`+"\n", key, raw.SessionID, raw.SHA256)
	if err := os.WriteFile(r.derivationPath(key), []byte(derivationJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.recordPath(key), []byte(recordJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	beforeDerivation, _ := os.ReadFile(r.derivationPath(key))
	beforeRecord, _ := os.ReadFile(r.recordPath(key))

	got, err := r.GetDerivation(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != core.SchemaVersionV1 || len(got.SourceAuthorityRefs) != 1 || got.SourceAuthorityRefs[0].Kind != core.StructuredInputRawOutput || got.SourceAuthorityRefs[0].RawOutput == nil || *got.SourceAuthorityRefs[0].RawOutput != raw {
		t.Fatalf("legacy derivation normalization=%#v", got)
	}
	records, err := r.ListRecords(context.Background(), key, structuredapp.RecordQuery{Offset: 0, Limit: 10})
	if err != nil || len(records) != 1 || records[0].SchemaVersion != core.SchemaVersionV1 || records[0].SourceRef.Kind != core.StructuredInputRawOutput || records[0].SourceRef.RawOutput == nil || *records[0].SourceRef.RawOutput != raw {
		t.Fatalf("legacy records=%#v err=%v", records, err)
	}
	afterDerivation, _ := os.ReadFile(r.derivationPath(key))
	afterRecord, _ := os.ReadFile(r.recordPath(key))
	if string(afterDerivation) != string(beforeDerivation) || string(afterRecord) != string(beforeRecord) {
		t.Fatal("legacy structured bytes were rewritten during read normalization")
	}
}

func TestStructuredLegacyV1AcceptsV2RawReplayWithoutRewrite(t *testing.T) {
	r := openStructuredRepository(t)
	raw := core.RawOutputRef{SessionID: "legacy-replay", StartByte: 0, EndByte: 4, SHA256: strings.Repeat("c", 64)}
	producer := core.Producer{AdapterID: "go-test-json", AdapterVersion: 1, CapabilityVersion: 1}
	config := strings.Repeat("d", 64)
	key, err := core.DerivationKey([]core.RawOutputRef{raw}, producer, 1, config)
	if err != nil {
		t.Fatal(err)
	}
	legacy := fmt.Sprintf(`{"schema_version":1,"derivation_key":%q,"source_authority_refs":[{"session_id":%q,"start_byte":0,"end_byte":4,"sha256":%q}],"producer":{"adapter_id":"go-test-json","adapter_version":1,"capability_version":1},"derivation_schema_version":1,"derivation_config_digest":%q,"lifecycle":"pending","completeness":"unavailable"}`+"\n", key, raw.SessionID, raw.SHA256, config)
	if err := os.WriteFile(r.derivationPath(key), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	modern := core.Derivation{
		SchemaVersion: core.SchemaVersion, DerivationKey: key,
		SourceAuthorityRefs: []core.StructuredInputRef{{Kind: core.StructuredInputRawOutput, RawOutput: &raw}},
		Producer:            producer, DerivationSchemaVersion: 1, DerivationConfigDigest: config,
		Lifecycle: core.LifecyclePending, Completeness: core.CompletenessUnavailable,
	}
	if err := r.PutDerivation(context.Background(), modern); err != nil {
		t.Fatalf("v2 raw replay of v1 identity: %v", err)
	}
	bytes, err := os.ReadFile(r.derivationPath(key))
	if err != nil {
		t.Fatal(err)
	}
	if string(bytes) != legacy {
		t.Fatal("v2 replay rewrote historical v1 derivation")
	}
}

func TestStructuredV1NewDerivationWriteIsRejected(t *testing.T) {
	r := openStructuredRepository(t)
	legacyWrite := structuredDerivation(t, 1, core.LifecyclePending, "", core.CompletenessUnavailable)
	legacyWrite.SchemaVersion = core.SchemaVersionV1
	if err := r.PutDerivation(context.Background(), legacyWrite); err == nil {
		t.Fatal("new schema-v1 derivation write accepted")
	}
	if _, err := os.Stat(r.derivationPath(legacyWrite.DerivationKey)); !os.IsNotExist(err) {
		t.Fatalf("rejected schema-v1 derivation created durable state: %v", err)
	}
}

func TestStructuredV1NewRecordWriteIsRejected(t *testing.T) {
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
	record := structuredArtifactRecord(processing, core.AuthorityMechanical, "legacy-write")
	record.SchemaVersion = core.SchemaVersionV1
	if err := r.PutRecords(context.Background(), processing.DerivationKey, []core.Record{record}); err == nil {
		t.Fatal("new schema-v1 record write accepted")
	}
	if _, err := os.Stat(r.recordPath(processing.DerivationKey)); !os.IsNotExist(err) {
		t.Fatalf("rejected schema-v1 record created durable state: %v", err)
	}
}

func TestStructuredLegacyV1LifecycleTransitionWritesV2(t *testing.T) {
	r := openStructuredRepository(t)
	raw := core.RawOutputRef{SessionID: "legacy-transition", StartByte: 0, EndByte: 4, SHA256: strings.Repeat("e", 64)}
	producer := core.Producer{AdapterID: "go-test-json", AdapterVersion: 1, CapabilityVersion: 1}
	config := strings.Repeat("f", 64)
	key, err := core.DerivationKey([]core.RawOutputRef{raw}, producer, 1, config)
	if err != nil {
		t.Fatal(err)
	}
	legacy := fmt.Sprintf(`{"schema_version":1,"derivation_key":%q,"source_authority_refs":[{"session_id":%q,"start_byte":0,"end_byte":4,"sha256":%q}],"producer":{"adapter_id":"go-test-json","adapter_version":1,"capability_version":1},"derivation_schema_version":1,"derivation_config_digest":%q,"lifecycle":"pending","completeness":"unavailable"}`+"\n", key, raw.SessionID, raw.SHA256, config)
	if err := os.WriteFile(r.derivationPath(key), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	processing, err := structuredapp.New(r).MarkProcessing(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if processing.SchemaVersion != core.SchemaVersion || processing.Lifecycle != core.LifecycleProcessing {
		t.Fatalf("processing=%#v", processing)
	}
	persisted, err := r.GetDerivation(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.SchemaVersion != core.SchemaVersion || persisted.Lifecycle != core.LifecycleProcessing {
		t.Fatalf("persisted transition=%#v", persisted)
	}
}

func TestStructuredLegacyV1RecordReplayIsIdempotentAcrossV2Resume(t *testing.T) {
	r := openStructuredRepository(t)
	raw := core.RawOutputRef{SessionID: "legacy-record-replay", StartByte: 0, EndByte: 4, SHA256: strings.Repeat("7", 64)}
	producer := core.Producer{AdapterID: "go-test-json", AdapterVersion: 1, CapabilityVersion: 1}
	config := strings.Repeat("8", 64)
	key, err := core.DerivationKey([]core.RawOutputRef{raw}, producer, 1, config)
	if err != nil {
		t.Fatal(err)
	}
	legacyDerivation := fmt.Sprintf(`{"schema_version":1,"derivation_key":%q,"source_authority_refs":[{"session_id":%q,"start_byte":0,"end_byte":4,"sha256":%q}],"producer":{"adapter_id":"go-test-json","adapter_version":1,"capability_version":1},"derivation_schema_version":1,"derivation_config_digest":%q,"lifecycle":"processing","completeness":"unavailable"}`+"\n", key, raw.SessionID, raw.SHA256, config)
	legacyRecords := fmt.Sprintf(`{"schema_version":1,"derivation_key":%q,"records":[{"schema_version":1,"record_kind":"artifact_result","authority":"mechanical","derivation_method":"native_field_mapping","producer":{"adapter_id":"go-test-json","adapter_version":1,"capability_version":1},"operation_id":"op-1","source_ref":{"session_id":%q,"start_byte":0,"end_byte":4,"sha256":%q},"artifact_result":{"name":"legacy-record","status":"ok"}}]}`+"\n", key, raw.SessionID, raw.SHA256)
	if err := os.WriteFile(r.derivationPath(key), []byte(legacyDerivation), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.recordPath(key), []byte(legacyRecords), 0o600); err != nil {
		t.Fatal(err)
	}
	beforeRecords, err := os.ReadFile(r.recordPath(key))
	if err != nil {
		t.Fatal(err)
	}

	modernDerivation := core.Derivation{
		SchemaVersion: core.SchemaVersion, DerivationKey: key,
		SourceAuthorityRefs: []core.StructuredInputRef{core.RawInputRef(raw)},
		Producer:            producer, DerivationSchemaVersion: 1, DerivationConfigDigest: config,
		Lifecycle: core.LifecycleProcessing, Completeness: core.CompletenessUnavailable,
	}
	modernRecord := structuredArtifactRecord(modernDerivation, core.AuthorityMechanical, "legacy-record")
	conflicting := modernRecord
	conflicting.RecordID = strings.Repeat("9", 64)
	if err := r.PutRecords(context.Background(), key, []core.Record{conflicting}); err == nil {
		t.Fatal("v2 replay with new record metadata matched historical v1 records")
	}

	terminal, err := structuredapp.New(r).Complete(context.Background(), key, core.ParseComplete, core.CompletenessComplete, []core.Record{modernRecord})
	if err != nil {
		t.Fatalf("v2 resume of historical v1 records: %v", err)
	}
	if terminal.SchemaVersion != core.SchemaVersion || terminal.Lifecycle != core.LifecycleTerminal || terminal.ParseOutcome != core.ParseComplete {
		t.Fatalf("terminal=%#v", terminal)
	}
	afterRecords, err := os.ReadFile(r.recordPath(key))
	if err != nil {
		t.Fatal(err)
	}
	if string(afterRecords) != string(beforeRecords) {
		t.Fatal("v2 replay rewrote historical v1 record bytes")
	}
}

func TestStructuredLegacyStrictDerivationDecodeFailsClosed(t *testing.T) {
	raw := core.RawOutputRef{SessionID: "legacy-strict", StartByte: 0, EndByte: 1, SHA256: strings.Repeat("1", 64)}
	producer := core.Producer{AdapterID: "go-test-json", AdapterVersion: 1, CapabilityVersion: 1}
	config := strings.Repeat("2", 64)
	key, err := core.DerivationKey([]core.RawOutputRef{raw}, producer, 1, config)
	if err != nil {
		t.Fatal(err)
	}
	valid := []byte(fmt.Sprintf(`{"schema_version":1,"derivation_key":%q,"source_authority_refs":[{"session_id":%q,"start_byte":0,"end_byte":1,"sha256":%q}],"producer":{"adapter_id":"go-test-json","adapter_version":1,"capability_version":1},"derivation_schema_version":1,"derivation_config_digest":%q,"lifecycle":"pending","completeness":"unavailable"}`, key, raw.SessionID, raw.SHA256, config))
	got, err := decodeStructuredDerivation(valid)
	if err != nil || got.SchemaVersion != core.SchemaVersionV1 || len(got.SourceAuthorityRefs) != 1 || got.SourceAuthorityRefs[0].Kind != core.StructuredInputRawOutput {
		t.Fatalf("valid legacy decode=%#v err=%v", got, err)
	}
	if _, err := decodeStructuredDerivation(append(append([]byte(nil), valid...), []byte(` {}`)...)); err == nil {
		t.Fatal("trailing structured derivation JSON accepted")
	}
	unknown := []byte(strings.Replace(string(valid), `"schema_version":1`, `"schema_version":99`, 1))
	if _, err := decodeStructuredDerivation(unknown); err == nil {
		t.Fatal("unknown structured derivation schema accepted")
	}
}
