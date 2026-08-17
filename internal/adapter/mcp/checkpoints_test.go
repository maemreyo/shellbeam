package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	checkpointapp "github.com/maemreyo/shellbeam/internal/app/checkpoint"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	core "github.com/maemreyo/shellbeam/internal/core/checkpoint"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"
)

type e26MCPClient struct {
	last   bridge.Request
	starts int
}

func (c *e26MCPClient) Forward(_ context.Context, req bridge.Request) (bridge.Response, error) {
	c.last = req
	if req.Action == "start" {
		c.starts++
	}
	checkpoint := e26MCPCheckpoint()
	switch req.Action {
	case "checkpoint_create":
		return bridge.Response{Checkpoint: &checkpoint}, nil
	case "checkpoint_restore":
		result := core.RestoreResult{SchemaVersion: core.SchemaVersion, RestoreID: req.CheckpointRestore.RestoreID, CheckpointID: req.CheckpointRestore.CheckpointID, Paths: []core.RestorePathResult{
			{Path: "secret-ish/a.go", Outcome: core.RestoreRestored},
			{Path: "secret-ish/b.go", Outcome: core.RestoreNoop},
			{Path: "secret-ish/c.go", Outcome: core.RestoreConflict, Reason: "current_changed"},
		}, Complete: false}
		return bridge.Response{Restore: &result}, nil
	case "checkpoint_inspect":
		inspection := checkpointapp.CheckpointInspection{Checkpoint: checkpoint, Provider: checkpointapp.ProviderCheckpointStatus{CheckpointID: checkpoint.CheckpointID, RetentionState: core.RetentionAvailable, Available: true}}
		return bridge.Response{CheckpointInspection: &inspection}, nil
	case "inspect.server":
		catalog := capability.Baseline(capability.Limits{}).WithSafetyCheckpoints(checkpoint.Provider, core.ConflictDetection{RegularFile: core.ConflictBestEffort, Symlink: core.ConflictBestEffort, AbsentToFile: core.ConflictBestEffort, DirectoryTree: core.ConflictUnsupported})
		return bridge.Response{Server: &catalog}, nil
	}
	return bridge.Response{}, nil
}

func TestE26MCPV2ForwardsCheckpointsThroughSingleToolWithSafeSummaries(t *testing.T) {
	client := &e26MCPClient{}
	catalog := capability.Baseline(capability.Limits{}).WithSafetyCheckpoints(core.ProviderIdentity{ID: "localfs", Version: 1}, core.ConflictDetection{RegularFile: core.ConflictBestEffort, Symlink: core.ConflictBestEffort, AbsentToFile: core.ConflictBestEffort, DirectoryTree: core.ConflictUnsupported})
	session, closeSession := currentSession(t, New(bridge.New(client), catalog))
	defer closeSession()
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 1 || tools.Tools[0].Name != "local_shell" {
		t.Fatalf("tools=%#v", tools.Tools)
	}

	create, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(`{"action":"checkpoint_create","checkpoint_create_id":"cp-create-1","workspace_id":"ws_01K00000000000000000000000","activity_id":"PI-756","paths":["secret-ish/**"]}`)})
	if err != nil {
		t.Fatal(err)
	}
	assertCheckpointToolSuccess(t, create, "checkpoint_create: chk_01K00000000000000000000000", "checkpoint")
	if client.starts != 0 || client.last.Action != "checkpoint_create" || client.last.CheckpointCreate.CreateID != "cp-create-1" {
		t.Fatalf("create last=%#v starts=%d", client.last, client.starts)
	}

	restore, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(`{"action":"checkpoint_restore","restore_id":"restore-1","checkpoint_id":"chk_01K00000000000000000000000","paths":["secret-ish/a.go","secret-ish/b.go","secret-ish/c.go"]}`)})
	if err != nil {
		t.Fatal(err)
	}
	assertCheckpointToolSuccess(t, restore, "checkpoint_restore: restored=1 noop=1 conflict=1 unsupported=0 failed=0", "restore")
	if client.starts != 0 || client.last.Action != "checkpoint_restore" || client.last.CheckpointRestore.RestoreID != "restore-1" {
		t.Fatalf("restore last=%#v starts=%d", client.last, client.starts)
	}
	text := restore.Content[0].(*mcpgo.TextContent).Text
	if strings.Contains(text, "secret-ish") {
		t.Fatalf("restore summary leaked path: %q", text)
	}

	inspect, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(`{"action":"checkpoint_inspect","checkpoint_id":"chk_01K00000000000000000000000"}`)})
	if err != nil {
		t.Fatal(err)
	}
	assertCheckpointToolSuccess(t, inspect, "checkpoint_inspect: available", "checkpoint_inspection")
	if client.starts != 0 || client.last.Action != "checkpoint_inspect" || client.last.CheckpointID != "chk_01K00000000000000000000000" {
		t.Fatalf("inspect last=%#v starts=%d", client.last, client.starts)
	}
}

func TestE26MCPV1RejectsCheckpointActions(t *testing.T) {
	client := &e26MCPClient{}
	catalog := capability.Baseline(capability.Limits{})
	server := New(bridge.New(client), catalog)
	forceLegacyDiscovery(server)
	session, closeSession := currentSession(t, server)
	defer closeSession()
	for _, raw := range []string{
		`{"action":"checkpoint_create","checkpoint_create_id":"cp-create-1","workspace_id":"ws_01K00000000000000000000000","paths":["src/**"]}`,
		`{"action":"checkpoint_restore","restore_id":"restore-1","checkpoint_id":"chk_01K00000000000000000000000","paths":["src/main.go"]}`,
		`{"action":"checkpoint_inspect","checkpoint_id":"chk_01K00000000000000000000000"}`,
	} {
		res, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(raw)})
		if err != nil {
			t.Fatal(err)
		}
		if !res.IsError || client.last.Action != "" {
			t.Fatalf("legacy checkpoint action accepted raw=%s res=%#v last=%#v", raw, res, client.last)
		}
	}
}

func TestE26LegacyCatalogProjectionStripsSafetyCheckpointCapabilities(t *testing.T) {
	modern := capability.Baseline(capability.Limits{}).WithSafetyCheckpoints(core.ProviderIdentity{ID: "localfs", Version: 1}, core.ConflictDetection{RegularFile: core.ConflictBestEffort, Symlink: core.ConflictBestEffort, AbsentToFile: core.ConflictBestEffort, DirectoryTree: core.ConflictUnsupported})
	legacy := legacyCatalogView(modern)
	if _, ok := legacy.Features[capability.FeatureSafetyCheckpoints]; ok || legacy.SafetyCheckpoints != nil {
		t.Fatalf("legacy checkpoint capability leaked: %#v", legacy)
	}
}

func assertCheckpointToolSuccess(t *testing.T, result *mcpgo.CallToolResult, wantText, field string) {
	t.Helper()
	if result.IsError || len(result.Content) != 1 {
		t.Fatalf("result=%#v", result)
	}
	text, ok := result.Content[0].(*mcpgo.TextContent)
	if !ok || text.Text != wantText {
		t.Fatalf("summary=%#v want=%q", result.Content, wantText)
	}
	body, ok := result.StructuredContent.(map[string]any)
	if !ok || body[field] == nil || body["action"] == nil || body["ok"] != true {
		t.Fatalf("structured=%#v", result.StructuredContent)
	}
}

func e26MCPCheckpoint() core.Checkpoint {
	return core.Checkpoint{SchemaVersion: core.SchemaVersion, CheckpointID: "chk_01K00000000000000000000000", CreateID: "cp-create-1", Provider: core.ProviderIdentity{ID: "localfs", Version: 1}, WorkspaceID: "ws_01K00000000000000000000000", ActivityID: "PI-756", SourceGeneration: "gen_" + strings.Repeat("a", 64), CreatedAt: time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC), CapturedPathCount: 1, TotalBytes: 7, CaptureQuality: core.CaptureComplete, RetentionState: core.RetentionAvailable, OpaqueEntryRefs: []string{"entry_01K00000000000000000000000"}}
}
