package schema

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/checkpoint"
)

const e26CheckpointID = "chk_01K00000000000000000000000"
const e26WorkspaceID = "ws_01K00000000000000000000000"

func TestE26CheckpointRequestSchemasAreClosedAndPathSafe(t *testing.T) {
	validMCP := []map[string]any{
		{"action": "checkpoint_create", "checkpoint_create_id": "cp-create-1", "workspace_id": e26WorkspaceID, "activity_id": "PI-756", "paths": []any{"internal/runtime/**", "tests/runtime/file.go"}},
		{"action": "checkpoint_restore", "restore_id": "restore-1", "checkpoint_id": e26CheckpointID, "paths": []any{"internal/runtime/file.go"}},
		{"action": "checkpoint_inspect", "checkpoint_id": e26CheckpointID},
	}
	for _, payload := range validMCP {
		if err := resolvedSchema(t, MCPInputV2).Validate(payload); err != nil {
			t.Errorf("valid MCP E26 rejected %v: %v", payload, err)
		}
	}

	invalidMCP := []map[string]any{
		{"action": "checkpoint_create", "checkpoint_create_id": "cp-create-1", "workspace_id": e26WorkspaceID, "paths": []any{"**"}},
		{"action": "checkpoint_create", "checkpoint_create_id": "cp-create-1", "workspace_id": e26WorkspaceID, "paths": []any{"/private/path"}},
		{"action": "checkpoint_create", "checkpoint_create_id": "cp-create-1", "workspace_id": e26WorkspaceID, "paths": []any{"src/*.go"}},
		{"action": "checkpoint_create", "checkpoint_create_id": "cp-create-1", "workspace_id": e26WorkspaceID, "paths": []any{"src"}, "checkpoint_id": e26CheckpointID},
		{"action": "checkpoint_restore", "restore_id": "restore-1", "checkpoint_id": e26CheckpointID, "paths": []any{"src/**"}},
		{"action": "checkpoint_restore", "restore_id": "restore-1", "checkpoint_id": e26CheckpointID, "paths": []any{"/src/file.go"}},
		{"action": "checkpoint_restore", "restore_id": "restore-1", "checkpoint_id": e26CheckpointID, "paths": []any{"src/file.go"}, "workspace_id": e26WorkspaceID},
		{"action": "checkpoint_inspect", "checkpoint_id": e26CheckpointID, "paths": []any{"src/file.go"}},
		{"action": "checkpoint_inspect", "checkpoint_id": "bad"},
		{"action": "checkpoint_create", "checkpoint_create_id": "cp-create-1", "workspace_id": e26WorkspaceID, "paths": []any{"src"}, "raw_content": "secret"},
		{"action": "checkpoint_inspect", "checkpoint_id": e26CheckpointID, "private_hash": strings.Repeat("a", 64)},
	}
	for _, payload := range invalidMCP {
		if err := resolvedSchema(t, MCPInputV2).Validate(payload); err == nil {
			t.Errorf("invalid MCP E26 accepted %v", payload)
		}
	}

	validIPC := []map[string]any{
		{"ipc_version": 2.0, "kind": "request", "request_id": "create", "action": "checkpoint_create", "checkpoint_create_id": "cp-create-1", "workspace_id": e26WorkspaceID, "paths": []any{"src/**"}},
		{"ipc_version": 2.0, "kind": "request", "request_id": "restore", "action": "checkpoint_restore", "restore_id": "restore-1", "checkpoint_id": e26CheckpointID, "paths": []any{"src/file.go"}},
		{"ipc_version": 2.0, "kind": "request", "request_id": "inspect", "action": "checkpoint_inspect", "checkpoint_id": e26CheckpointID},
	}
	for _, payload := range validIPC {
		if err := resolvedSchema(t, IPCV2).Validate(payload); err != nil {
			t.Errorf("valid IPC E26 rejected %v: %v", payload, err)
		}
	}
}

func TestE26CheckpointResponseSchemasExposeOnlyPublicMetadata(t *testing.T) {
	checkpoint := e26CheckpointPayload()
	restore := map[string]any{
		"schema_version": 1.0,
		"restore_id":     "restore-1",
		"checkpoint_id":  e26CheckpointID,
		"paths":          []any{map[string]any{"path": "src/file.go", "outcome": "restored"}},
		"complete":       true,
	}
	inspection := map[string]any{
		"checkpoint": checkpoint,
		"provider":   map[string]any{"checkpoint_id": e26CheckpointID, "retention_state": "available", "available": true},
	}
	validMCP := []map[string]any{
		{"schema_version": 2.0, "ok": true, "action": "checkpoint_create", "checkpoint": checkpoint},
		{"schema_version": 2.0, "ok": true, "action": "checkpoint_restore", "restore": restore},
		{"schema_version": 2.0, "ok": true, "action": "checkpoint_inspect", "checkpoint_inspection": inspection},
	}
	for _, payload := range validMCP {
		if err := resolvedSchema(t, MCPOutputV2).Validate(payload); err != nil {
			t.Errorf("valid MCP E26 output rejected %v: %v", payload, err)
		}
	}
	validIPC := []map[string]any{
		{"ipc_version": 2.0, "kind": "response", "request_id": "create", "action": "checkpoint_create", "ok": true, "checkpoint": checkpoint},
		{"ipc_version": 2.0, "kind": "response", "request_id": "restore", "action": "checkpoint_restore", "ok": true, "restore": restore},
		{"ipc_version": 2.0, "kind": "response", "request_id": "inspect", "action": "checkpoint_inspect", "ok": true, "checkpoint_inspection": inspection},
	}
	for _, payload := range validIPC {
		if err := resolvedSchema(t, IPCV2).Validate(payload); err != nil {
			t.Errorf("valid IPC E26 output rejected %v: %v", payload, err)
		}
	}

	leaky := e26CheckpointPayload()
	leaky["raw_content"] = "pink-elephant-secret"
	if err := resolvedSchema(t, MCPOutputV2).Validate(map[string]any{"schema_version": 2.0, "ok": true, "action": "checkpoint_create", "checkpoint": leaky}); err == nil {
		t.Fatal("checkpoint response accepted raw content")
	}
	leaky = e26CheckpointPayload()
	leaky["private_hash"] = strings.Repeat("a", 64)
	if err := resolvedSchema(t, MCPOutputV2).Validate(map[string]any{"schema_version": 2.0, "ok": true, "action": "checkpoint_create", "checkpoint": leaky}); err == nil {
		t.Fatal("checkpoint response accepted private hash")
	}
}

func TestE26CheckpointCoreJSONUsesStableTotalBytesName(t *testing.T) {
	raw, err := json.Marshal(core.Checkpoint{TotalBytes: 7})
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, `"total_bytes":7`) || strings.Contains(text, `"TotalBytes"`) {
		t.Fatalf("checkpoint JSON total bytes field unstable: %s", text)
	}
}

func e26CheckpointPayload() map[string]any {
	return map[string]any{
		"schema_version":       1.0,
		"checkpoint_id":        e26CheckpointID,
		"checkpoint_create_id": "cp-create-1",
		"provider":             map[string]any{"provider_id": "localfs", "provider_version": 1.0},
		"workspace_id":         e26WorkspaceID,
		"activity_id":          "PI-756",
		"source_generation":    "gen_" + strings.Repeat("a", 64),
		"created_at":           "2026-08-16T12:00:00Z",
		"captured_path_count":  1.0,
		"total_bytes":          7.0,
		"capture_quality":      "complete",
		"retention_state":      "available",
		"opaque_entry_refs":    []any{"entry_01K00000000000000000000000"},
	}
}

func TestE26CheckpointActionsRemainAbsentFromLegacySchemas(t *testing.T) {
	legacyMCP := resolvedSchema(t, MCPInputV1)
	for _, payload := range []map[string]any{
		{"action": "checkpoint_create", "checkpoint_create_id": "cp-create-1", "workspace_id": e26WorkspaceID, "paths": []any{"src/**"}},
		{"action": "checkpoint_restore", "restore_id": "restore-1", "checkpoint_id": e26CheckpointID, "paths": []any{"src/main.go"}},
		{"action": "checkpoint_inspect", "checkpoint_id": e26CheckpointID},
	} {
		if err := legacyMCP.Validate(payload); err == nil {
			t.Errorf("legacy MCP accepted checkpoint payload %v", payload)
		}
	}
}

func TestE26CheckpointGoStructsValidateAgainstV2OutputSchema(t *testing.T) {
	checkpoint := core.Checkpoint{
		SchemaVersion: core.SchemaVersion, CheckpointID: e26CheckpointID, CreateID: "cp-create-1",
		Provider: core.ProviderIdentity{ID: "localfs", Version: 1}, WorkspaceID: e26WorkspaceID, ActivityID: "PI-756",
		SourceGeneration: "gen_" + strings.Repeat("a", 64), CreatedAt: time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC),
		CapturedPathCount: 1, TotalBytes: 7, CaptureQuality: core.CaptureComplete, RetentionState: core.RetentionAvailable,
		OpaqueEntryRefs: []string{"entry_01K00000000000000000000000"},
	}
	for _, payload := range []map[string]any{
		{"schema_version": 2, "ok": true, "action": "checkpoint_create", "checkpoint": checkpoint},
		{"schema_version": 2, "ok": true, "action": "checkpoint_restore", "restore": core.RestoreResult{SchemaVersion: core.SchemaVersion, RestoreID: "restore-1", CheckpointID: e26CheckpointID, Paths: []core.RestorePathResult{{Path: "src/main.go", Outcome: core.RestoreRestored}}, Complete: true}},
	} {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		if err := resolvedSchema(t, MCPOutputV2).Validate(decoded); err != nil {
			t.Fatalf("Go checkpoint output rejected %s: %v", raw, err)
		}
	}
}
