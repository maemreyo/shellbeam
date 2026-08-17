package schema

import (
	"strings"
	"testing"
)

func TestE27InputTraceModernOperationSchemasCarryOnlyClosedSafeTraceBinding(t *testing.T) {
	trace := e27TraceSchemaValue()
	v2 := map[string]any{
		"schema_version": 2.0, "operation_id": "e27-op", "session_id": "e27-session", "request_fingerprint": strings.Repeat("a", 64), "execution_fingerprint": strings.Repeat("b", 64),
		"execution_mode": "shell", "executable": "/bin/sh", "command": "true", "cwd": "/tmp", "tty": false, "timeout_ms": 0.0, "shell": "/bin/sh", "daemon_incarnation": "d", "input_trace": trace,
	}
	v3 := map[string]any{
		"schema_version": 3.0, "operation_id": "typed-op", "workspace_id": "ws_01K00000000000000000000000", "logical_cwd": ".", "session_id": "typed-session",
		"request_fingerprint": strings.Repeat("c", 64), "execution_fingerprint": strings.Repeat("d", 64), "execution_mode": "argv", "executable": "go", "argv": []any{"go", "test", "./internal/app"},
		"cwd": "/repo", "tty": false, "timeout_ms": 5000.0, "shell": "", "daemon_incarnation": "daemon", "control_reservation_bytes": 0.0, "project_command": projectCommandBindingSchemaValue(), "created_at": "2026-08-17T01:00:00Z", "input_trace": trace,
	}
	for _, tc := range []struct {
		name Name
		v    map[string]any
	}{{OperationV2, v2}, {OperationV3, v3}} {
		if err := resolvedSchema(t, tc.name).Validate(tc.v); err != nil {
			t.Fatalf("%s rejected safe E27 binding: %v", tc.name, err)
		}
		leaky := cloneProjectCommandMap(tc.v)
		badTrace := cloneProjectCommandMap(trace)
		badTrace["socket_path"] = "/tmp/private.sock"
		leaky["input_trace"] = badTrace
		if err := resolvedSchema(t, tc.name).Validate(leaky); err == nil {
			t.Fatalf("%s accepted private trace field", tc.name)
		}
		privateSpawn := cloneProjectCommandMap(tc.v)
		privateSpawn["environment_additions"] = []any{map[string]any{"name": "DYLD_INSERT_LIBRARIES", "value": "/tmp/private.dylib"}}
		if err := resolvedSchema(t, tc.name).Validate(privateSpawn); err == nil {
			t.Fatalf("%s accepted ephemeral spawn control", tc.name)
		}
	}
}

func TestE27InputTraceOperationV1RejectsTraceBinding(t *testing.T) {
	v1 := map[string]any{"schema_version": 1.0, "operation_id": "legacy", "session_id": "legacy-s", "fingerprint": "fp", "command": "true", "cwd": "/tmp", "tty": false, "timeout_ms": 0.0, "shell": "/bin/sh", "daemon_incarnation": "d", "input_trace": e27TraceSchemaValue()}
	if err := resolvedSchema(t, OperationV1).Validate(v1); err == nil {
		t.Fatal("operation v1 accepted E27 binding")
	}
}

func e27TraceSchemaValue() map[string]any {
	return map[string]any{
		"schema_version": 1.0, "trace_id": "trace_01K00000000000000000000000", "mode": "best_effort", "status": "active",
		"provider": map[string]any{"provider_id": "dyld-interpose", "provider_version": 1.0, "capability_version": 1.0}, "platform": "darwin",
		"instrumentation_fingerprint": strings.Repeat("e", 64), "instrumentation_effect": "environment_affecting", "pre_exec_coverage_established": false,
		"coverage": map[string]any{"filesystem_reads": "partial", "filesystem_metadata_queries": "partial", "directory_enumerations": "partial", "filesystem_writes": "partial", "executed_binaries": "partial", "loaded_libraries": "partial", "environment_names_observed": "unsupported", "network_attempts": "unsupported", "child_processes": "partial"},
	}
}
