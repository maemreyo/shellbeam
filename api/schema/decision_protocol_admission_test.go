package schema

import (
	"encoding/json"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

func TestDecisionProtocolExperimentIDIsDurableOnlyInOperationV2AndV3(t *testing.T) {
	cases := []struct {
		name   string
		schema Name
		value  map[string]any
		want   bool
	}{
		{
			name:   "v2 accepts optional experiment",
			schema: Name("operation-v2.json"),
			value:  map[string]any{"schema_version": 2.0, "operation_id": "op", "experiment_id": "exp-a", "session_id": "s", "request_fingerprint": "req", "execution_fingerprint": "exec", "observation_binding_fingerprint": "obs", "command": "true", "cwd": "/tmp", "shell": "/bin/sh", "daemon_incarnation": "d"},
			want:   true,
		},
		{
			name:   "v3 accepts optional experiment",
			schema: Name("operation-v3.json"),
			value:  map[string]any{"schema_version": 3.0, "operation_id": "op", "experiment_id": "exp-a", "workspace_id": "ws_01K00000000000000000000000", "logical_cwd": ".", "session_id": "s", "request_fingerprint": "req", "execution_fingerprint": "exec", "observation_binding_fingerprint": "obs", "execution_mode": "argv", "executable": "go", "argv": []any{"go", "test"}, "cwd": "/tmp", "tty": false, "timeout_ms": 0.0, "daemon_incarnation": "d", "control_reservation_bytes": 0.0, "created_at": "2026-08-19T00:00:00Z", "project_command": validDecisionProtocolProjectBinding()},
			want:   true,
		},
		{
			name:   "v1 remains unchanged and rejects experiment",
			schema: Name("operation-v1.json"),
			value:  map[string]any{"schema_version": 1.0, "operation_id": "op", "experiment_id": "exp-a", "session_id": "s", "fingerprint": "fp", "command": "true", "cwd": "/tmp", "tty": false, "timeout_ms": 0.0, "shell": "/bin/sh", "daemon_incarnation": "d"},
			want:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := Load(tc.schema)
			if err != nil {
				t.Fatal(err)
			}
			var doc jsonschema.Schema
			if err := json.Unmarshal(data, &doc); err != nil {
				t.Fatal(err)
			}
			resolved, err := doc.Resolve(nil)
			if err != nil {
				t.Fatal(err)
			}
			err = resolved.Validate(tc.value)
			if tc.want && err != nil {
				t.Fatal(err)
			}
			if !tc.want && err == nil {
				t.Fatal("schema unexpectedly accepted experiment_id")
			}
		})
	}
}

func validDecisionProtocolProjectBinding() map[string]any {
	return map[string]any{
		"schema_version":           1.0,
		"manifest_digest":          "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"manifest_schema_version":  2.0,
		"command_id":               "test_package",
		"parameter_fingerprint":    "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"parameters":               []any{},
		"resolved_argv":            []any{"go", "test"},
		"logical_cwd":              ".",
		"resolved_cwd":             "/tmp",
		"source_generation":        "gen_cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		"path_observation_quality": "exact_at_bind",
	}
}
