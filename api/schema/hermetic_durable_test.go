package schema

import (
	"strings"
	"testing"
)

func TestHermeticDurableBindingIsClosedOnOperationV2V3(t *testing.T) {
	binding := durableHermeticBindingSchemaValue()
	v2 := map[string]any{
		"schema_version": 2.0, "operation_id": "h-op", "session_id": "h-session", "request_fingerprint": "req", "execution_fingerprint": "exec",
		"execution_mode": "shell", "executable": "/bin/sh", "command": "true", "cwd": "/repo", "tty": false, "timeout_ms": 0.0, "shell": "/bin/sh", "daemon_incarnation": "daemon", "hermetic_boundary": binding,
	}
	if err := resolvedSchema(t, OperationV2).Validate(v2); err != nil {
		t.Fatalf("operation v2 rejected hermetic binding: %v", err)
	}
	v3 := map[string]any{
		"schema_version": 3.0, "operation_id": "h-typed", "session_id": "h-session", "request_fingerprint": "req", "execution_fingerprint": "exec", "execution_mode": "argv", "executable": "go",
		"argv": []any{"go", "test", "./internal/app"}, "workspace_id": "ws_01K00000000000000000000000", "logical_cwd": ".", "cwd": "/repo", "tty": false, "timeout_ms": 0.0, "shell": "", "daemon_incarnation": "daemon", "control_reservation_bytes": 0.0, "created_at": "2026-08-18T00:00:00Z",
		"project_command": projectCommandBindingSchemaValue(), "hermetic_boundary": binding,
	}
	if err := resolvedSchema(t, OperationV3).Validate(v3); err != nil {
		t.Fatalf("operation v3 rejected hermetic binding: %v", err)
	}
	missingContent := cloneProjectCommandMap(v2)
	missingBindingContent := durableHermeticBindingSchemaValue()
	delete(missingBindingContent, "capture_content_sha256")
	missingContent["hermetic_boundary"] = missingBindingContent
	if err := resolvedSchema(t, OperationV2).Validate(missingContent); err == nil {
		t.Fatal("operation v2 accepted hermetic binding without capture content digest")
	}

	bad := cloneProjectCommandMap(v2)
	leaky := durableHermeticBindingSchemaValue()
	leaky["private_state_root"] = "/private/hb"
	bad["hermetic_boundary"] = leaky
	if err := resolvedSchema(t, OperationV2).Validate(bad); err == nil {
		t.Fatal("operation v2 accepted private hermetic path")
	}
}

func TestHermeticDurableReceiptRequiresBindingAndResultV2V3(t *testing.T) {
	binding := durableHermeticBindingSchemaValue()
	result := durableHermeticResultSchemaValue()
	v2 := map[string]any{
		"schema_version": 2.0, "operation_id": "h-op", "session_id": "h-session", "request_fingerprint": "req", "execution_fingerprint": "exec", "daemon_incarnation": "daemon",
		"state": "completed", "outcome": "success", "tty": false, "timeout_ms": 0.0, "output_bytes": 0.0, "output_complete": true, "input_accepted_bytes": 0.0, "input_delivered_bytes": 0.0, "stdin_closed": true,
		"spawn_evidence": map[string]any{"attempted": true, "succeeded": true}, "exit_evidence": map[string]any{"reaped": true, "code": 0.0}, "signal_evidence": map[string]any{"attempted": false, "succeeded": false},
		"hermetic_boundary": binding, "hermetic_result": result,
	}
	if err := resolvedSchema(t, ReceiptV2).Validate(v2); err != nil {
		t.Fatalf("receipt v2 rejected hermetic truth: %v", err)
	}
	v3 := projectCommandReceiptV3SchemaValue()
	v3["hermetic_boundary"] = binding
	v3["hermetic_result"] = result
	if err := resolvedSchema(t, ReceiptV3).Validate(v3); err != nil {
		t.Fatalf("receipt v3 rejected hermetic truth: %v", err)
	}
	for _, name := range []Name{ReceiptV2, ReceiptV3} {
		base := v2
		if name == ReceiptV3 {
			base = v3
		}
		missingResult := cloneProjectCommandMap(base)
		delete(missingResult, "hermetic_result")
		if err := resolvedSchema(t, name).Validate(missingResult); err == nil {
			t.Fatalf("%s accepted hermetic binding without result", name)
		}
		missingBinding := cloneProjectCommandMap(base)
		delete(missingBinding, "hermetic_boundary")
		if err := resolvedSchema(t, name).Validate(missingBinding); err == nil {
			t.Fatalf("%s accepted hermetic result without binding", name)
		}
	}
	bad := cloneProjectCommandMap(v2)
	leaky := durableHermeticResultSchemaValue()
	leaky["status_fd"] = 3.0
	bad["hermetic_result"] = leaky
	if err := resolvedSchema(t, ReceiptV2).Validate(bad); err == nil {
		t.Fatal("receipt accepted private provider status fd")
	}
}

func TestHermeticStartSchemaRequiresRegisteredWorkspaceAuthority(t *testing.T) {
	hermetic := validHermeticSchemaValue()
	withoutWorkspace := map[string]any{"action": "start", "operation_id": "h-no-workspace", "command": "true", "cwd": "/repo", "hermetic": hermetic}
	if err := resolvedSchema(t, MCPInputV2).Validate(withoutWorkspace); err == nil {
		t.Fatal("MCP accepted hermetic raw start without workspace authority")
	}
	withoutWorkspaceIPC := map[string]any{"ipc_version": 2.0, "kind": "request", "request_id": "h-no-workspace", "action": "start", "operation_id": "h-no-workspace", "command": "true", "cwd": "/repo", "hermetic": hermetic}
	if err := resolvedSchema(t, IPCV2).Validate(withoutWorkspaceIPC); err == nil {
		t.Fatal("IPC accepted hermetic raw start without workspace authority")
	}
	withWorkspace := map[string]any{"action": "start", "operation_id": "h-workspace", "workspace_id": "ws_01K00000000000000000000000", "cwd": ".", "command": "true", "hermetic": hermetic}
	if err := resolvedSchema(t, MCPInputV2).Validate(withWorkspace); err != nil {
		t.Fatalf("MCP rejected workspace-bound hermetic start: %v", err)
	}
}

func durableHermeticBindingSchemaValue() map[string]any {
	return map[string]any{
		"schema_version": 1.0, "boundary_id": "hb_01K00000000000000000000000", "request": validHermeticSchemaValue(), "capture_manifest_sha256": strings.Repeat("d", 64), "capture_content_sha256": strings.Repeat("e", 64),
		"provider":  map[string]any{"provider": "bubblewrap", "version": "0.11.2", "binary_sha256": strings.Repeat("a", 64), "runtime_manifest_sha256": strings.Repeat("b", 64)},
		"toolchain": map[string]any{"id": "go-1.26.6-linux-amd64", "manifest_sha256": strings.Repeat("c", 64)},
	}
}
func durableHermeticResultSchemaValue() map[string]any {
	b := durableHermeticBindingSchemaValue()
	return map[string]any{"schema_version": 1.0, "boundary_id": b["boundary_id"], "provider": b["provider"], "toolchain": b["toolchain"], "established_pre_exec": true, "continuity": "complete"}
}
