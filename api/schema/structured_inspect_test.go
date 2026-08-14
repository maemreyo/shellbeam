package schema

import "testing"

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

	summary := map[string]any{"errors": 1.0, "warnings": 0.0, "files": 1.0, "test_passed": 0.0, "test_failed": 0.0, "test_skipped": 0.0, "mechanical_records": 1.0, "advisory_records": 0.0, "records_returned": 1.0, "records_total_or_lower_bound": 1.0, "records_total_exact": true, "truncated": false, "details_status": "available"}
	record := map[string]any{"schema_version": 1.0, "record_kind": "diagnostic", "authority": "mechanical", "derivation_method": "native_field_mapping", "producer": map[string]any{"adapter_id": "go-vet-json", "adapter_version": 1.0, "capability_version": 1.0}, "operation_id": "op-1", "source_ref": map[string]any{"session_id": "session-1", "start_byte": 0.0, "end_byte": 10.0, "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, "diagnostic": map[string]any{"severity": "error", "code": "printf", "message": "bad printf", "location": map[string]any{"kind": "provider_reported", "provider_reported": map[string]any{"origin": "repository", "sanitized_logical_path": "internal/a.go", "line": 5.0, "column": 2.0, "normalization_quality": "partial"}}}}
	structured := map[string]any{"schema_version": 1.0, "operation_id": "op-1", "status": "terminal", "derivation_key": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "producer": map[string]any{"adapter_id": "go-vet-json", "adapter_version": 1.0, "capability_version": 1.0}, "parse_outcome": "complete", "completeness": "complete", "summary": summary, "records": []any{record}}
	validOutputs := []struct {
		name  Name
		value map[string]any
	}{
		{MCPOutputV2, map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.structured", "structured": structured}},
		{IPCV2, map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "s", "action": "inspect.structured", "ok": true, "structured": structured}},
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
