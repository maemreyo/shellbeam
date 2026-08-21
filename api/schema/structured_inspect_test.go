package schema

import (
	"strings"
	"testing"
)

func TestStructuredInspectV2SchemasAreClosedAndBounded(t *testing.T) {
	inputValid := []struct {
		name  Name
		value map[string]any
	}{
		{MCPInputV2, map[string]any{"action": "inspect.structured", "operation_id": "op-1", "record_kind": "diagnostic", "severity": "error", "path": "internal/a.go", "max_records": 10.0}},
		{IPCV2, map[string]any{"ipc_version": 2.0, "kind": "request", "request_id": "s", "action": "inspect.structured", "operation_id": "op-1", "test_status": "fail", "max_records": 10.0}},
	}
	for _, tc := range inputValid {
		if err := resolvedSchema(t, tc.name).Validate(tc.value); err != nil {
			t.Errorf("%s rejected %#v: %v", tc.name, tc.value, err)
		}
	}

	_, structured, failureRecord := structuredInspectSchemaFixtures()
	structuredWithFailure := cloneMap(structured)
	structuredWithFailure["records"] = []any{failureRecord}
	v2Record := cloneMap(failureRecord)
	v2Record["schema_version"] = 2.0
	v2Case := cloneMap(failureRecord["test_case"].(map[string]any))
	delete(v2Case, "failure_excerpt")
	v2Record["test_case"] = v2Case
	structuredWithV2 := cloneMap(structured)
	structuredWithV2["records"] = []any{v2Record}
	validOutputs := []struct {
		name  Name
		value map[string]any
	}{
		{MCPOutputV2, map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.structured", "structured": structured}},
		{IPCV2, map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "s", "action": "inspect.structured", "ok": true, "structured": structured}},
		{MCPOutputV2, map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.structured", "structured": structuredWithFailure}},
		{IPCV2, map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "f", "action": "inspect.structured", "ok": true, "structured": structuredWithFailure}},
		{MCPOutputV2, map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.structured", "structured": structuredWithV2}},
		{MCPOutputV2, map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.structured", "structured": map[string]any{"schema_version": 1.0, "operation_id": "missing", "status": "not_found", "summary": map[string]any{"errors": 0.0, "warnings": 0.0, "files": 0.0, "test_passed": 0.0, "test_failed": 0.0, "test_skipped": 0.0, "mechanical_records": 0.0, "advisory_records": 0.0, "records_returned": 0.0, "records_total_or_lower_bound": 0.0, "records_total_exact": false, "truncated": false, "details_status": "unavailable"}}}},
	}
	for _, tc := range validOutputs {
		if err := resolvedSchema(t, tc.name).Validate(tc.value); err != nil {
			t.Errorf("%s rejected %#v: %v", tc.name, tc.value, err)
		}
	}

	invalid := []struct {
		name  Name
		value map[string]any
	}{
		{MCPInputV2, map[string]any{"action": "inspect.structured", "operation_id": "op-1", "record_kind": "unknown", "max_records": 10.0}},
		{MCPInputV2, map[string]any{"action": "inspect.structured", "operation_id": "op-1", "path": "../secret", "max_records": 10.0}},
		{MCPInputV2, map[string]any{"action": "inspect.structured", "operation_id": "op-1", "continuation": "bad-token", "max_records": 10.0}},
		{MCPInputV2, map[string]any{"action": "inspect.structured", "operation_id": "op-1", "max_records": 0.0}},
		{MCPInputV2, map[string]any{"action": "poll", "session_id": "s", "max_records": 10.0}},
		{IPCV2, map[string]any{"ipc_version": 2.0, "kind": "request", "request_id": "s", "action": "inspect.structured", "operation_id": "op-1", "max_records": 257.0}},
	}
	for _, tc := range invalid {
		if err := resolvedSchema(t, tc.name).Validate(tc.value); err == nil {
			t.Errorf("%s accepted invalid %#v", tc.name, tc.value)
		}
	}
}

func TestStructuredInspectV2OutputSchemasRejectInvalidMetadata(t *testing.T) {
	record, structured, _ := structuredInspectSchemaFixtures()
	badRecord := cloneMap(record)
	badRecord["extra"] = true
	badStructured := cloneMap(structured)
	badStructured["records"] = []any{badRecord}
	if err := resolvedSchema(t, MCPOutputV2).Validate(map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.structured", "structured": badStructured}); err == nil {
		t.Fatal("structured record accepted extra property")
	}
	tooMany := make([]any, 257)
	for i := range tooMany {
		tooMany[i] = record
	}
	tooManyStructured := cloneMap(structured)
	tooManyStructured["records"] = tooMany
	if err := resolvedSchema(t, MCPOutputV2).Validate(map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.structured", "structured": tooManyStructured}); err == nil {
		t.Fatal("257 structured records accepted")
	}
	badReason := cloneMap(structured)
	badReason["completeness_reason"] = "future_reason"
	if err := resolvedSchema(t, MCPOutputV2).Validate(map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.structured", "structured": badReason}); err == nil {
		t.Fatal("unknown completeness reason accepted")
	}
	badCounts := cloneMap(structured)
	badCounts["observed_entries"] = map[string]any{"namespace": "jest", "vocabulary_version": 1.0, "files": 1.0, "entries": 65537.0, "pass": 65537.0, "fail": 0.0, "skip": 0.0, "error": 0.0}
	if err := resolvedSchema(t, IPCV2).Validate(map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "s", "action": "inspect.structured", "ok": true, "structured": badCounts}); err == nil {
		t.Fatal("observed entry count above bound accepted")
	}
	for _, forbidden := range []string{"task_complete", "work_complete", "safe_to_finish"} {
		claim := cloneMap(structured)
		claim[forbidden] = true
		if err := resolvedSchema(t, MCPOutputV2).Validate(map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.structured", "structured": claim}); err == nil {
			t.Fatalf("structured schema accepted completion claim %q", forbidden)
		}
	}
}

func TestStructuredTestSuiteProducerDispositionSchemaIsV2OnlyAndClosed(t *testing.T) {
	_, structured, _ := structuredInspectSchemaFixtures()
	suiteRecord := map[string]any{
		"schema_version": 2.0, "record_kind": "test_suite", "authority": "mechanical", "derivation_method": "native_field_mapping",
		"producer": map[string]any{"adapter_id": "jest-json", "adapter_version": 1.0, "capability_version": 1.0}, "operation_id": "op-1",
		"source_ref": map[string]any{"kind": "raw_output", "raw_output": map[string]any{"session_id": "session-1", "start_byte": 0.0, "end_byte": 10.0, "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},
		"test_suite": map[string]any{"name": "src/a.test.js", "status": "pass", "producer_disposition": map[string]any{"namespace": "jest", "vocabulary_version": 1.0, "code": "jest:suite_focused"}},
	}
	withSuite := cloneMap(structured)
	withSuite["records"] = []any{suiteRecord}
	ipcRoot := map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "suite", "action": "inspect.structured", "ok": true, "structured": withSuite}
	if err := resolvedSchema(t, IPCV2).Validate(ipcRoot); err != nil {
		t.Fatalf("ipc rejected v2 suite disposition: %v", err)
	}
	mcpRoot := map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.structured", "structured": withSuite}
	if err := resolvedSchema(t, MCPOutputV2).Validate(mcpRoot); err != nil {
		t.Fatalf("mcp rejected v2 suite disposition: %v", err)
	}
	legacy := cloneMap(suiteRecord)
	legacy["schema_version"] = 1.0
	legacyStructured := cloneMap(structured)
	legacyStructured["records"] = []any{legacy}
	if err := resolvedSchema(t, IPCV2).Validate(map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "legacy", "action": "inspect.structured", "ok": true, "structured": legacyStructured}); err == nil {
		t.Fatal("schema v1 suite accepted producer_disposition")
	}
	future := cloneMap(suiteRecord)
	futureSuite := cloneMap(suiteRecord["test_suite"].(map[string]any))
	futureDisposition := cloneMap(futureSuite["producer_disposition"].(map[string]any))
	futureDisposition["future"] = true
	futureSuite["producer_disposition"] = futureDisposition
	future["test_suite"] = futureSuite
	futureStructured := cloneMap(structured)
	futureStructured["records"] = []any{future}
	if err := resolvedSchema(t, MCPOutputV2).Validate(map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.structured", "structured": futureStructured}); err == nil {
		t.Fatal("suite producer disposition accepted unknown member")
	}
}

func TestStructuredTestCaseAttemptCountSchemaIsV2PlusAndBounded(t *testing.T) {
	_, structured, _ := structuredInspectSchemaFixtures()
	record := map[string]any{
		"schema_version": 2.0, "record_kind": "test_case", "authority": "mechanical", "derivation_method": "native_field_mapping",
		"producer": map[string]any{"adapter_id": "jest-json", "adapter_version": 1.0, "capability_version": 1.0}, "operation_id": "op-1",
		"source_ref": map[string]any{"kind": "raw_output", "raw_output": map[string]any{"session_id": "session-1", "start_byte": 0.0, "end_byte": 10.0, "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},
		"test_case":  map[string]any{"name": "retried pass", "status": "pass", "attempt_count": 3.0},
	}
	withRecord := cloneMap(structured)
	withRecord["records"] = []any{record}
	if err := resolvedSchema(t, IPCV2).Validate(map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "attempts", "action": "inspect.structured", "ok": true, "structured": withRecord}); err != nil {
		t.Fatalf("ipc rejected attempt count: %v", err)
	}
	if err := resolvedSchema(t, MCPOutputV2).Validate(map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.structured", "structured": withRecord}); err != nil {
		t.Fatalf("mcp rejected attempt count: %v", err)
	}
	legacy := cloneMap(record)
	legacy["schema_version"] = 1.0
	legacyStructured := cloneMap(structured)
	legacyStructured["records"] = []any{legacy}
	if err := resolvedSchema(t, IPCV2).Validate(map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "legacy-attempts", "action": "inspect.structured", "ok": true, "structured": legacyStructured}); err == nil {
		t.Fatal("schema v1 test case accepted attempt_count")
	}
	bad := cloneMap(record)
	badCase := cloneMap(record["test_case"].(map[string]any))
	badCase["attempt_count"] = 1048577.0
	bad["test_case"] = badCase
	badStructured := cloneMap(structured)
	badStructured["records"] = []any{bad}
	if err := resolvedSchema(t, MCPOutputV2).Validate(map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.structured", "structured": badStructured}); err == nil {
		t.Fatal("attempt_count above bound accepted")
	}
}

func TestStructuredFailureExcerptSchemasAreClosedAndVersioned(t *testing.T) {
	_, structured, failureRecord := structuredInspectSchemaFixtures()
	v2WithExcerpt := cloneMap(failureRecord)
	v2WithExcerpt["schema_version"] = 2.0
	badV2Structured := cloneMap(structured)
	badV2Structured["records"] = []any{v2WithExcerpt}
	if err := resolvedSchema(t, MCPOutputV2).Validate(map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.structured", "structured": badV2Structured}); err == nil {
		t.Fatal("schema-v2 record accepted failure_excerpt")
	}
	passWithExcerpt := cloneMap(failureRecord)
	passCase := cloneMap(failureRecord["test_case"].(map[string]any))
	passCase["status"] = "pass"
	passWithExcerpt["test_case"] = passCase
	badPassStructured := cloneMap(structured)
	badPassStructured["records"] = []any{passWithExcerpt}
	if err := resolvedSchema(t, MCPOutputV2).Validate(map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.structured", "structured": badPassStructured}); err == nil {
		t.Fatal("pass record accepted failure_excerpt")
	}
	oversizedExcerpt := cloneMap(failureRecord)
	oversizedCase := cloneMap(failureRecord["test_case"].(map[string]any))
	oversizedFailure := cloneMap(oversizedCase["failure_excerpt"].(map[string]any))
	oversizedFailure["text"] = strings.Repeat("x", 2049)
	oversizedCase["failure_excerpt"] = oversizedFailure
	oversizedExcerpt["test_case"] = oversizedCase
	badExcerptStructured := cloneMap(structured)
	badExcerptStructured["records"] = []any{oversizedExcerpt}
	if err := resolvedSchema(t, IPCV2).Validate(map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "x", "action": "inspect.structured", "ok": true, "structured": badExcerptStructured}); err == nil {
		t.Fatal("failure excerpt above bound accepted")
	}
}

func structuredInspectSchemaFixtures() (map[string]any, map[string]any, map[string]any) {
	summary := map[string]any{"errors": 1.0, "warnings": 0.0, "files": 1.0, "test_passed": 0.0, "test_failed": 0.0, "test_skipped": 0.0, "mechanical_records": 1.0, "advisory_records": 0.0, "records_returned": 1.0, "records_total_or_lower_bound": 1.0, "records_total_exact": true, "truncated": false, "details_status": "available"}
	record := map[string]any{"schema_version": 1.0, "record_kind": "diagnostic", "authority": "mechanical", "derivation_method": "native_field_mapping", "producer": map[string]any{"adapter_id": "go-vet-json", "adapter_version": 1.0, "capability_version": 1.0}, "operation_id": "op-1", "source_ref": map[string]any{"session_id": "session-1", "start_byte": 0.0, "end_byte": 10.0, "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, "diagnostic": map[string]any{"severity": "error", "code": "printf", "message": "bad printf", "location": map[string]any{"kind": "provider_reported", "provider_reported": map[string]any{"origin": "repository", "sanitized_logical_path": "internal/a.go", "line": 5.0, "column": 2.0, "normalization_quality": "partial"}}}}
	failureRecord := map[string]any{"schema_version": 3.0, "record_kind": "test_case", "authority": "mechanical", "derivation_method": "native_field_mapping", "producer": map[string]any{"adapter_id": "jest-json", "adapter_version": 1.0, "capability_version": 1.0}, "operation_id": "op-1", "source_ref": map[string]any{"kind": "raw_output", "raw_output": map[string]any{"session_id": "session-1", "start_byte": 0.0, "end_byte": 10.0, "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}, "test_case": map[string]any{"name": "fails", "status": "fail", "failure_excerpt": map[string]any{"namespace": "jest", "vocabulary_version": 1.0, "text": "failure at src/a.ts:12", "truncated": false, "redacted": false}}}
	structured := map[string]any{"schema_version": 1.0, "operation_id": "op-1", "status": "terminal", "derivation_key": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "producer": map[string]any{"adapter_id": "go-vet-json", "adapter_version": 1.0, "capability_version": 1.0}, "parse_outcome": "partial", "completeness": "partial", "completeness_reason": "pass_records_elided", "observed_entries": map[string]any{"namespace": "jest", "vocabulary_version": 1.0, "files": 2.0, "entries": 2.0, "pass": 1.0, "fail": 1.0, "skip": 0.0, "error": 0.0}, "summary": summary, "records": []any{record}}
	return record, structured, failureRecord
}

func TestStructuredCapabilityCatalogSchemaIsOptionalAndClosed(t *testing.T) {
	catalog := map[string]any{"shellbeam_protocol_version": 2.0, "receipt_schema_versions": []any{1.0, 2.0}, "project_manifest_schema_versions": []any{1.0}, "event_cursor_schema_versions": []any{1.0}, "result_cursor_schema_versions": []any{1.0}, "structured_adapter_ids": []any{"go-test-json", "go-vet-json"}, "structured_result_kinds": []any{"diagnostic", "test_case", "test_suite", "artifact_result"}, "structured_lifecycle": true, "features": map[string]any{"structured_results": "available", "structured_lifecycle": "available"}, "limits": map[string]any{"command_bytes": 1.0, "response_bytes": 2.0, "session_output_bytes": 3.0, "runtime_ms": 4.0, "live_sessions": 5.0, "activity_history": 0.0, "event_journal_max_events": 256.0, "event_cursor_bytes": 2048.0, "event_snapshot_facts": 64.0, "structured_inspect_records": 128.0}}
	for _, tc := range []struct {
		name  Name
		value map[string]any
	}{
		{MCPOutputV2, map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.server", "server": catalog}},
		{IPCV2, map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "c", "action": "inspect.server", "ok": true, "server": catalog}},
	} {
		if err := resolvedSchema(t, tc.name).Validate(tc.value); err != nil {
			t.Errorf("%s rejected catalog: %v", tc.name, err)
		}
	}
	bad := cloneMap(catalog)
	bad["structured_result_kinds"] = []any{"mystery"}
	if err := resolvedSchema(t, MCPOutputV2).Validate(map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.server", "server": bad}); err == nil {
		t.Fatal("unknown structured result kind accepted")
	}
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
