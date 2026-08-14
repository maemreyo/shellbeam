package schema

import (
	"strings"
	"testing"
)

func TestTelemetryAndReproV2SchemasAreClosedBoundedAndHonest(t *testing.T) {
	validInputs := []struct {
		name  Name
		value map[string]any
	}{
		{MCPInputV2, map[string]any{"action": "inspect.telemetry", "operation_id": "op-1", "max_samples": 16.0}},
		{MCPInputV2, map[string]any{"action": "repro.create", "repro_create_id": "repro-create-1", "operation_id": "op-1"}},
		{IPCV2, map[string]any{"ipc_version": 2.0, "kind": "request", "request_id": "r", "action": "repro.create", "repro_create_id": "repro-create-1", "operation_id": "op-1", "capture_policy": map[string]any{"dependent_derivations": "current"}}},
		{IPCV2, map[string]any{"ipc_version": 2.0, "kind": "request", "request_id": "i", "action": "inspect.repro", "repro_id": "repro_01K00000000000000000000000"}},
	}
	for _, tc := range validInputs {
		if err := resolvedSchema(t, tc.name).Validate(tc.value); err != nil {
			t.Errorf("%s rejected %#v: %v", tc.name, tc.value, err)
		}
	}
	invalid := []struct {
		name  Name
		value map[string]any
	}{
		{MCPInputV2, map[string]any{"action": "inspect.telemetry", "operation_id": "op-1", "max_samples": 0.0}},
		{MCPInputV2, map[string]any{"action": "inspect.telemetry", "operation_id": "op-1", "max_samples": 129.0}},
		{MCPInputV2, map[string]any{"action": "inspect.telemetry", "operation_id": "op-1", "max_samples": 1.0, "command": "env"}},
		{MCPInputV2, map[string]any{"action": "repro.create", "repro_create_id": "repro-create-1", "operation_id": "op-1", "capture_policy": map[string]any{"dependent_derivations": "future"}}},
		{MCPInputV2, map[string]any{"action": "repro.create", "repro_create_id": "repro-create-1", "operation_id": "op-1", "stdin": "secret"}},
		{MCPInputV2, map[string]any{"action": "inspect.repro", "repro_id": "bad"}},
	}
	for _, tc := range invalid {
		if err := resolvedSchema(t, tc.name).Validate(tc.value); err == nil {
			t.Errorf("%s accepted invalid %#v", tc.name, tc.value)
		}
	}

	telemetry := map[string]any{"schema_version": 1.0, "status": "unavailable", "operation_id": "op-1", "samples_returned": 0.0, "samples_available": 0.0}
	capsule := reproSchemaCapsule()
	inspection := map[string]any{"schema_version": 1.0, "capsule": capsule, "references": []any{map[string]any{"ref_id": "telemetry:operation:op-1", "record_kind": "execution_telemetry", "resolution_state": "unavailable"}}}
	outputs := []struct {
		name  Name
		value map[string]any
	}{
		{MCPOutputV2, map[string]any{"schema_version": 2.0, "ok": true, "action": "inspect.telemetry", "telemetry": telemetry}},
		{MCPOutputV2, map[string]any{"schema_version": 2.0, "ok": true, "action": "repro.create", "capsule": capsule}},
		{IPCV2, map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "i", "action": "inspect.repro", "ok": true, "repro": inspection}},
	}
	for _, tc := range outputs {
		if err := resolvedSchema(t, tc.name).Validate(tc.value); err != nil {
			t.Errorf("%s rejected output %#v: %v", tc.name, tc.value, err)
		}
	}
	for _, name := range []Name{IPCV2, MCPOutputV2} {
		data, err := Load(name)
		if err != nil {
			t.Fatal(err)
		}
		lower := strings.ToLower(string(data))
		for _, forbidden := range []string{"reproducible", "performance_regression", "root_cause"} {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("%s exposes forbidden verdict %q", name, forbidden)
			}
		}
	}
}

func reproSchemaCapsule() map[string]any {
	d := strings.Repeat("a", 64)
	return map[string]any{
		"schema_version": 1.0, "repro_create_id": "repro-create-1", "repro_id": "repro_01K00000000000000000000000", "created_at": "2026-08-15T00:00:00Z", "capture_cut_digest": d,
		"execution":   map[string]any{"operation_id": "op-1", "session_id": "session-1", "receipt_digest": d, "command_semantics_fingerprint": d, "execution_mode": "argv", "executable": "go", "resolved_argv": []any{"go", "test", "./..."}, "command_details": "exact"},
		"source":      map[string]any{"quality": "unavailable"},
		"project":     map[string]any{"quality": "unavailable"},
		"environment": map[string]any{"environment_quality": "unavailable", "toolchain_quality": "unavailable"},
		"input":       map[string]any{"accepted_bytes": 0.0, "delivered_bytes": 0.0, "complete": true, "content_identity": "unavailable"},
		"results":     []any{map[string]any{"ref_id": "telemetry:operation:op-1", "record_kind": "execution_telemetry", "producer_id": "shellbeam.telemetry", "producer_version": 1.0, "schema_version": 1.0, "original_availability": "absent"}},
		"capture":     map[string]any{"source": "unavailable", "command": "exact", "toolchain": "unavailable", "environment": "unavailable", "filesystem_external": "unknown", "network_dependencies": "unknown", "external_services": "unknown", "time_randomness": "unknown", "input": "partial", "results": "complete"},
	}
}
