package schema

import (
	"strings"
	"testing"
)

func TestTypedProjectCommandModernWireSchemasAreClosed(t *testing.T) {
	valid := []struct {
		name  Name
		value map[string]any
	}{
		{MCPInputV2, map[string]any{"action": "start", "operation_id": "typed-op", "workspace_id": "ws_01K00000000000000000000000", "project_command_id": "test_package", "params": map[string]any{"package": "./internal/app", "count": "3"}, "timeout_ms": 5000.0}},
		{IPCV2, map[string]any{"ipc_version": 2.0, "kind": "request", "request_id": "typed", "action": "start", "operation_id": "typed-op", "workspace_id": "ws_01K00000000000000000000000", "project_command_id": "test_package", "params": map[string]any{"package": "./internal/app"}, "timeout_ms": 5000.0}},
	}
	for _, tc := range valid {
		if err := resolvedSchema(t, tc.name).Validate(tc.value); err != nil {
			t.Errorf("%s rejected typed start %#v: %v", tc.name, tc.value, err)
		}
	}
	invalid := []struct {
		name  Name
		value map[string]any
	}{
		{MCPInputV2, map[string]any{"action": "start", "operation_id": "typed-op", "workspace_id": "ws_01K00000000000000000000000", "project_command_id": "test_package", "command": "true"}},
		{MCPInputV2, map[string]any{"action": "start", "operation_id": "typed-op", "workspace_id": "ws_01K00000000000000000000000", "params": map[string]any{"name": "x"}}},
		{MCPInputV2, map[string]any{"action": "start", "operation_id": "typed-op", "workspace_id": "ws_01K00000000000000000000000", "project_command_id": "test_package", "params": map[string]any{"BAD": "x"}}},
		{IPCV2, map[string]any{"ipc_version": 2.0, "kind": "request", "request_id": "typed", "action": "start", "operation_id": "typed-op", "workspace_id": "ws_01K00000000000000000000000", "project_command_id": "test_package", "argv": []any{"true"}}},
		{IPCV2, map[string]any{"ipc_version": 2.0, "kind": "request", "request_id": "typed", "action": "start", "operation_id": "typed-op", "workspace_id": "ws_01K00000000000000000000000", "project_command_id": "test_package", "params": map[string]any{"name": "x"}, "extra": true}},
	}
	for _, tc := range invalid {
		if err := resolvedSchema(t, tc.name).Validate(tc.value); err == nil {
			t.Errorf("%s accepted invalid typed start %#v", tc.name, tc.value)
		}
	}
}

func TestOperationAndReceiptV3RequireFrozenProjectCommandBinding(t *testing.T) {
	binding := projectCommandBindingSchemaValue()
	operation := map[string]any{
		"schema_version": 3.0, "operation_id": "typed-op", "workspace_id": "ws_01K00000000000000000000000", "logical_cwd": ".", "session_id": "typed-session",
		"request_fingerprint": strings.Repeat("a", 64), "execution_fingerprint": strings.Repeat("b", 64), "execution_mode": "argv", "executable": "go",
		"argv": []any{"go", "test", "./internal/app"}, "cwd": "/repo", "tty": false, "timeout_ms": 5000.0, "shell": "", "daemon_incarnation": "daemon", "control_reservation_bytes": 0.0,
		"project_command": binding, "created_at": "2026-08-15T01:00:00Z",
	}
	receipt := projectCommandReceiptV3SchemaValue()
	for _, tc := range []struct {
		name  Name
		value map[string]any
	}{{OperationV3, operation}, {ReceiptV3, receipt}} {
		if err := resolvedSchema(t, tc.name).Validate(tc.value); err != nil {
			t.Fatalf("%s rejected valid v3: %v", tc.name, err)
		}
		without := cloneProjectCommandMap(tc.value)
		delete(without, "project_command")
		if err := resolvedSchema(t, tc.name).Validate(without); err == nil {
			t.Fatalf("%s accepted missing project_command", tc.name)
		}
		bad := cloneProjectCommandMap(tc.value)
		badBinding := cloneProjectCommandMap(binding)
		badBinding["raw_secret"] = "nope"
		bad["project_command"] = badBinding
		if err := resolvedSchema(t, tc.name).Validate(bad); err == nil {
			t.Fatalf("%s accepted open project command binding", tc.name)
		}
	}
}

func TestV2PersistedSchemasRejectProjectCommandProvenance(t *testing.T) {
	operation := map[string]any{
		"schema_version": 2.0, "operation_id": "op", "session_id": "s", "request_fingerprint": "req", "execution_fingerprint": "exec",
		"execution_mode": "argv", "executable": "true", "argv": []any{"true"}, "cwd": "/tmp", "tty": false, "timeout_ms": 0.0, "shell": "", "daemon_incarnation": "d", "project_command": projectCommandBindingSchemaValue(),
	}
	receipt := map[string]any{
		"schema_version": 2.0, "operation_id": "op", "session_id": "s", "request_fingerprint": "req", "execution_fingerprint": "exec", "daemon_incarnation": "d", "state": "failed", "outcome": "failure", "tty": false, "timeout_ms": 0.0,
		"output_bytes": 0.0, "output_complete": true, "input_accepted_bytes": 0.0, "input_delivered_bytes": 0.0, "stdin_closed": false, "project_command": projectCommandBindingSchemaValue(),
		"spawn_evidence": map[string]any{"attempted": true, "succeeded": true}, "exit_evidence": map[string]any{"reaped": true, "code": 1.0}, "signal_evidence": map[string]any{"attempted": false, "succeeded": false},
	}
	for _, tc := range []struct {
		name  Name
		value map[string]any
	}{{OperationV2, operation}, {ReceiptV2, receipt}} {
		if err := resolvedSchema(t, tc.name).Validate(tc.value); err == nil {
			t.Fatalf("%s accepted v3 provenance", tc.name)
		}
	}
}

func projectCommandBindingSchemaValue() map[string]any {
	return map[string]any{
		"schema_version": 1.0, "manifest_digest": strings.Repeat("c", 64), "manifest_schema_version": 2.0, "command_id": "test_package",
		"parameter_fingerprint": strings.Repeat("d", 64),
		"parameters":            []any{map[string]any{"id": "package", "kind": "repo_package", "value": "./internal/app", "source": "caller", "provider_id": "go-repo-package", "provider_version": 1.0}},
		"resolved_argv":         []any{"go", "test", "./internal/app"}, "logical_cwd": ".", "resolved_cwd": "/repo", "source_generation": "gen_" + strings.Repeat("e", 64), "path_observation_quality": "exact_at_bind",
	}
}

func cloneProjectCommandMap(input map[string]any) map[string]any {
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func TestTypedProjectCommandV3ReceiptIsAcceptedInModernStartResult(t *testing.T) {
	result := map[string]any{
		"schema_version": 2.0,
		"operation":      map[string]any{"operation_id": "typed-op", "workspace_id": "ws_01K00000000000000000000000", "session_id": "typed-session", "state": "terminal"},
		"child":          map[string]any{"state": "exited", "outcome": "success", "exit_code": 0.0, "timed_out": false},
		"output":         map[string]any{"canonical_stream": "combined", "raw_bytes": 0.0, "returned_bytes": 0.0, "cursor": 0.0, "next_cursor": 0.0, "truncated": false, "output_complete": true},
		"receipt":        projectCommandReceiptV3SchemaValue(),
	}
	for _, tc := range []struct {
		name  Name
		value map[string]any
	}{
		{MCPOutputV2, map[string]any{"schema_version": 2.0, "ok": true, "action": "start", "result": result}},
		{IPCV2, map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "typed", "action": "start", "ok": true, "result": result}},
	} {
		if err := resolvedSchema(t, tc.name).Validate(tc.value); err != nil {
			t.Fatalf("%s rejected typed terminal receipt v3: %v", tc.name, err)
		}
	}
}

func projectCommandReceiptV3SchemaValue() map[string]any {
	return map[string]any{
		"schema_version": 3.0, "operation_id": "typed-op", "session_id": "typed-session", "request_fingerprint": strings.Repeat("a", 64), "execution_fingerprint": strings.Repeat("b", 64),
		"daemon_incarnation": "daemon", "execution_mode": "argv", "executable": "go", "state": "completed", "outcome": "success", "cwd": "/repo", "tty": false, "timeout_ms": 5000.0,
		"output_bytes": 0.0, "output_complete": true, "input_accepted_bytes": 0.0, "input_delivered_bytes": 0.0, "stdin_closed": false, "project_command": projectCommandBindingSchemaValue(),
		"spawn_evidence": map[string]any{"attempted": true, "succeeded": true}, "exit_evidence": map[string]any{"reaped": true, "code": 0.0}, "signal_evidence": map[string]any{"attempted": false, "succeeded": false},
	}
}

func TestTypedProjectCommandReproSchemaPreservesFrozenBindingFacts(t *testing.T) {
	capsule := reproSchemaCapsule()
	execution := capsule["execution"].(map[string]any)
	execution["project_command_binding_digest"] = strings.Repeat("b", 64)
	execution["project_manifest_digest"] = strings.Repeat("c", 64)
	execution["project_command_id"] = "test_package"
	execution["parameter_binding_fingerprint"] = strings.Repeat("d", 64)
	execution["parameter_providers"] = []any{map[string]any{"parameter_id": "package", "provider_id": "go-repo-package", "provider_version": 1.0}}
	capsule["project"] = map[string]any{"manifest_digest": strings.Repeat("c", 64), "quality": "exact"}
	for _, tc := range []struct {
		name  Name
		value map[string]any
	}{
		{MCPOutputV2, map[string]any{"schema_version": 2.0, "ok": true, "action": "repro.create", "capsule": capsule}},
		{IPCV2, map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "typed-repro", "action": "repro.create", "ok": true, "capsule": capsule}},
	} {
		if err := resolvedSchema(t, tc.name).Validate(tc.value); err != nil {
			t.Errorf("%s rejected typed repro capsule: %v", tc.name, err)
		}
	}
	bad := reproSchemaCapsule()
	badExecution := bad["execution"].(map[string]any)
	badExecution["parameter_providers"] = []any{map[string]any{"parameter_id": "package", "provider_id": "go-repo-package", "provider_version": 1.0, "raw_value": "secret"}}
	if err := resolvedSchema(t, MCPOutputV2).Validate(map[string]any{"schema_version": 2.0, "ok": true, "action": "repro.create", "capsule": bad}); err == nil {
		t.Fatal("repro schema accepted open provider descriptor")
	}
}

func TestTypedProjectCommandTelemetrySchemaCarriesBindingDigest(t *testing.T) {
	d := strings.Repeat("a", 64)
	metric := map[string]any{"quality": "unavailable"}
	record := map[string]any{
		"schema_version": 1.0, "derivation_key": d, "derivation_schema_version": 1.0, "derivation_config_digest": d,
		"producer":     map[string]any{"producer_id": "shellbeam.telemetry", "producer_version": 1.0, "capability_version": 1.0},
		"operation_id": "typed-op", "session_id": "typed-session", "receipt_digest": d,
		"project_command_id": "test_package", "parameter_binding_fingerprint": strings.Repeat("b", 64), "project_command_binding_digest": strings.Repeat("c", 64),
		"command_semantics_fingerprint": strings.Repeat("d", 64), "scope_class": "project_command", "platform": "darwin", "architecture": "arm64",
		"wall_ms": 1.0, "output_bytes": 0.0, "input_accepted_bytes": 0.0, "input_delivered_bytes": 0.0,
		"terminal_outcome": "success", "timed_out": false, "captured_at": "2026-08-15T01:00:00Z", "lifecycle": "terminal", "completeness": "complete",
		"resources": map[string]any{"cpu_user_ms": metric, "cpu_system_ms": metric, "max_rss_bytes": metric, "read_bytes": metric, "write_bytes": metric, "process_count_peak": metric},
	}
	inspection := map[string]any{"schema_version": 1.0, "status": "available", "operation_id": "typed-op", "compatibility_key": d, "latest": record, "samples_returned": 1.0, "samples_available": 1.0}
	for _, tc := range []struct {
		name  Name
		value map[string]any
	}{
		{MCPOutputV2, map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.telemetry", "telemetry": inspection}},
		{IPCV2, map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "typed-telemetry", "action": "inspect.telemetry", "ok": true, "telemetry": inspection}},
	} {
		if err := resolvedSchema(t, tc.name).Validate(tc.value); err != nil {
			t.Errorf("%s rejected typed telemetry binding digest: %v", tc.name, err)
		}
	}
	badRecord := cloneProjectCommandMap(record)
	badRecord["project_command_binding_digest"] = "bad"
	badInspection := cloneProjectCommandMap(inspection)
	badInspection["latest"] = badRecord
	if err := resolvedSchema(t, MCPOutputV2).Validate(map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.telemetry", "telemetry": badInspection}); err == nil {
		t.Fatal("telemetry schema accepted invalid binding digest")
	}
}
