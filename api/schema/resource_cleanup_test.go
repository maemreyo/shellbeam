package schema

import (
	"strings"
	"testing"
)

func TestResourceCleanupMetadataIsClosedAndReceiptV2V3Only(t *testing.T) {
	cleanup := map[string]any{"status": "incomplete", "reason": "cleanup_remove_failed"}
	v2 := map[string]any{
		"schema_version": 2.0, "operation_id": "op", "session_id": "s", "request_fingerprint": "req", "execution_fingerprint": "exec",
		"daemon_incarnation": "d", "state": "completed", "outcome": "success", "tty": false, "timeout_ms": 0.0,
		"output_bytes": 0.0, "output_complete": true, "input_accepted_bytes": 0.0, "input_delivered_bytes": 0.0, "stdin_closed": true,
		"spawn_evidence": map[string]any{"attempted": true, "succeeded": true}, "exit_evidence": map[string]any{"reaped": true, "code": 0.0}, "signal_evidence": map[string]any{"attempted": false, "succeeded": false},
		"resource_cleanup": cleanup,
	}
	if err := resolvedSchema(t, ReceiptV2).Validate(v2); err != nil {
		t.Fatalf("receipt v2 rejected cleanup metadata: %v", err)
	}
	bad := cloneMapResourceCleanup(v2)
	bad["resource_cleanup"] = map[string]any{"status": "incomplete", "reason": "cleanup_remove_failed", "path": "/private/cgroup"}
	if err := resolvedSchema(t, ReceiptV2).Validate(bad); err == nil {
		t.Fatal("receipt v2 cleanup accepted private/unknown path field")
	}
	badReason := cloneMapResourceCleanup(v2)
	badReason["resource_cleanup"] = map[string]any{"status": "incomplete", "reason": "/private/cgroup"}
	if err := resolvedSchema(t, ReceiptV2).Validate(badReason); err == nil {
		t.Fatal("receipt v2 cleanup accepted unbounded reason")
	}
	v3 := map[string]any{
		"schema_version": 3.0, "operation_id": "typed", "session_id": "s3", "request_fingerprint": "req", "execution_fingerprint": "exec",
		"daemon_incarnation": "d", "execution_mode": "argv", "executable": "/bin/true", "state": "completed", "outcome": "success",
		"tty": false, "timeout_ms": 0.0, "output_bytes": 0.0, "output_complete": true, "input_accepted_bytes": 0.0, "input_delivered_bytes": 0.0, "stdin_closed": true,
		"spawn_evidence": map[string]any{"attempted": true, "succeeded": true}, "exit_evidence": map[string]any{"reaped": true, "code": 0.0}, "signal_evidence": map[string]any{"attempted": false, "succeeded": false},
		"resource_cleanup": cleanup,
		"project_command": map[string]any{
			"schema_version": 1.0, "manifest_digest": strings.Repeat("a", 64), "manifest_schema_version": 2.0, "command_id": "test",
			"parameter_fingerprint": strings.Repeat("b", 64), "parameters": []any{}, "resolved_argv": []any{"/bin/true"}, "logical_cwd": ".", "resolved_cwd": "/tmp",
		},
	}
	if err := resolvedSchema(t, ReceiptV3).Validate(v3); err != nil {
		t.Fatalf("receipt v3 rejected cleanup metadata: %v", err)
	}
}

func cloneMapResourceCleanup(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
