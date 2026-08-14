package schema

import (
	"encoding/json"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

func TestV2PersistedSchemasAreClosedAndSeparateFingerprints(t *testing.T) {
	tests := []struct {
		name    Name
		valid   []map[string]any
		invalid []map[string]any
	}{
		{
			name: Name("operation-v2.json"),
			valid: []map[string]any{{
				"schema_version": 2.0, "operation_id": "op", "session_id": "s",
				"request_fingerprint": "req", "execution_fingerprint": "exec",
				"observation_binding_fingerprint": "obs", "workspace_id": "ws_01K00000000000000000000000", "logical_cwd": "src", "command": "true", "cwd": "/tmp/src",
				"tty": false, "timeout_ms": 0.0, "shell": "/bin/sh", "daemon_incarnation": "d",
				"control_reservation_bytes": 0.0, "created_at": "2026-08-13T00:00:00Z",
			}, {
				"schema_version": 2.0, "operation_id": "op-argv", "session_id": "s-argv",
				"request_fingerprint": "req-a", "execution_fingerprint": "exec-a", "execution_mode": "argv", "executable": "/bin/echo",
				"argv": []any{"/bin/echo", "hi"}, "cwd": "/tmp", "tty": false, "timeout_ms": 0.0, "daemon_incarnation": "d",
				"control_reservation_bytes": 0.0, "created_at": "2026-08-13T00:00:00Z",
			},
			},
			invalid: []map[string]any{
				{"schema_version": 2.0, "operation_id": "op", "session_id": "s", "execution_fingerprint": "exec", "command": "true", "cwd": "/tmp", "shell": "/bin/sh", "daemon_incarnation": "d"},
				{"schema_version": 2.0, "operation_id": "op", "session_id": "s", "request_fingerprint": "req", "execution_fingerprint": "exec", "fingerprint": "legacy", "command": "true", "cwd": "/tmp", "shell": "/bin/sh", "daemon_incarnation": "d"},
			},
		},
		{
			name: Name("receipt-v2.json"),
			valid: []map[string]any{{
				"schema_version": 2.0, "operation_id": "op", "session_id": "s",
				"request_fingerprint": "req", "execution_fingerprint": "exec",
				"observation_binding_fingerprint": "obs", "daemon_incarnation": "d",
				"state": "failed", "outcome": "failure", "tty": false, "timeout_ms": 0.0,
				"output_bytes": 9.0, "output_complete": true, "input_accepted_bytes": 0.0,
				"input_delivered_bytes": 0.0, "stdin_closed": false,
				"spawn_evidence":  map[string]any{"attempted": true, "succeeded": true},
				"exit_evidence":   map[string]any{"reaped": true, "code": 1.0},
				"signal_evidence": map[string]any{"attempted": false, "succeeded": false},
			}},
			invalid: []map[string]any{
				{"schema_version": 2.0, "operation_id": "op", "session_id": "s", "execution_fingerprint": "exec", "daemon_incarnation": "d", "state": "failed", "outcome": "failure", "output_bytes": 0.0, "output_complete": true, "input_accepted_bytes": 0.0, "input_delivered_bytes": 0.0},
				{"schema_version": 2.0, "operation_id": "op", "session_id": "s", "request_fingerprint": "req", "execution_fingerprint": "exec", "fingerprint": "legacy", "daemon_incarnation": "d", "state": "failed", "outcome": "failure", "output_bytes": 0.0, "output_complete": true, "input_accepted_bytes": 0.0, "input_delivered_bytes": 0.0},
			},
		},
	}
	for _, tt := range tests {
		t.Run(string(tt.name), func(t *testing.T) {
			data, err := Load(tt.name)
			if err != nil {
				t.Fatal(err)
			}
			var schemaDoc jsonschema.Schema
			if err := json.Unmarshal(data, &schemaDoc); err != nil {
				t.Fatal(err)
			}
			resolved, err := schemaDoc.Resolve(nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, payload := range tt.valid {
				if err := resolved.Validate(payload); err != nil {
					t.Errorf("valid payload rejected: %v", err)
				}
			}
			for _, payload := range tt.invalid {
				if err := resolved.Validate(payload); err == nil {
					t.Errorf("invalid payload accepted: %#v", payload)
				}
			}
		})
	}
}
