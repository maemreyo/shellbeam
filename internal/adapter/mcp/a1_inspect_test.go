package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	activity "github.com/maemreyo/shellbeam/internal/core/activity"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	codeintel "github.com/maemreyo/shellbeam/internal/core/codeintel"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"
)

type a1InspectBridgeClient struct {
	workspace  workspace.Workspace
	activity   activity.Activity
	startCalls int
	last       bridge.Request
}

func (c *a1InspectBridgeClient) Forward(_ context.Context, req bridge.Request) (bridge.Response, error) {
	c.last = req
	switch req.Action {
	case "start":
		c.startCalls++
	case "inspect.workspace":
		return bridge.Response{Workspace: &c.workspace}, nil
	case "inspect.activity":
		return bridge.Response{Activity: &c.activity}, nil
	case "inspect.server":
		catalog := capability.Baseline(capability.Limits{})
		return bridge.Response{Server: &catalog}, nil
	case "inspect.code":
		result := codeintel.Result{Status: codeintel.StatusUnavailable, Query: *req.CodeQuery}
		return bridge.Response{CodeResult: &result}, nil
	}
	return bridge.Response{}, nil
}

func TestAgentExecutionA1MCPInspectWorkspaceAndActivityNeverSpawn(t *testing.T) {
	now := time.Date(2026, 8, 14, 3, 30, 0, 0, time.UTC)
	client := &a1InspectBridgeClient{
		workspace: workspace.Workspace{
			SchemaVersion: workspace.SchemaVersion,
			ID:            "ws_01K00000000000000000000000",
			RepositoryID:  "repo_01K00000000000000000000000",
			Label:         "primary",
			Root:          "/tmp/repo",
			GitDir:        "/tmp/repo/.git",
			CreatedAt:     now,
			LastSeenAt:    now,
		},
		activity: activity.Activity{
			SchemaVersion: activity.SchemaVersion,
			ID:            "activity-a1",
			WorkspaceIDs:  []workspace.WorkspaceID{"ws_01K00000000000000000000000"},
			CreatedAt:     now,
			UpdatedAt:     now,
		},
	}
	session, closeSession := currentSession(t, New(bridge.New(client), capability.Baseline(capability.Limits{})))
	defer closeSession()

	workspaceResult, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{
		Name:      "local_shell",
		Arguments: json.RawMessage(`{"action":"inspect.workspace","workspace_id":"ws_01K00000000000000000000000"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if workspaceResult.IsError {
		t.Fatalf("workspace result=%#v", workspaceResult)
	}
	body, ok := workspaceResult.StructuredContent.(map[string]any)
	if !ok || body["workspace"] == nil {
		t.Fatalf("workspace structured=%#v", workspaceResult.StructuredContent)
	}

	activityResult, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{
		Name:      "local_shell",
		Arguments: json.RawMessage(`{"action":"inspect.activity","activity_id":"activity-a1"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if activityResult.IsError {
		t.Fatalf("activity result=%#v", activityResult)
	}
	body, ok = activityResult.StructuredContent.(map[string]any)
	if !ok || body["activity"] == nil {
		t.Fatalf("activity structured=%#v", activityResult.StructuredContent)
	}
	if client.startCalls != 0 {
		t.Fatalf("inspect spawned command: starts=%d", client.startCalls)
	}
}

func TestAgentExecutionA1MCPInspectCodeMapsClosedQueryAndResult(t *testing.T) {
	client := &a1InspectBridgeClient{}
	session, closeSession := currentSession(t, New(bridge.New(client), capability.Baseline(capability.Limits{})))
	defer closeSession()

	result, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{
		Name:      "local_shell",
		Arguments: json.RawMessage(`{"action":"inspect.code","workspace_id":"ws_01K00000000000000000000000","activity_id":"ZMR-111-validator","code_query":{"kind":"diagnostics","scope":"changed_files"}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("code result=%#v", result)
	}
	body, ok := result.StructuredContent.(map[string]any)
	if !ok || body["code"] == nil {
		t.Fatalf("code structured=%#v", result.StructuredContent)
	}
	if client.last.CodeQuery == nil || client.last.CodeQuery.Kind != codeintel.QueryDiagnostics ||
		client.last.WorkspaceID != "ws_01K00000000000000000000000" || client.last.ActivityID != "ZMR-111-validator" {
		t.Fatalf("mapped request=%#v", client.last)
	}
}
