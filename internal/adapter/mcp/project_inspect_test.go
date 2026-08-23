package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	projectadapter "github.com/maemreyo/shellbeam/internal/adapter/project"
	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	projectapp "github.com/maemreyo/shellbeam/internal/app/project"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"
)

type mcpProjectWorkspaceLookup struct {
	values []workspace.Workspace
}

func (p mcpProjectWorkspaceLookup) ListWorkspaces(context.Context) ([]workspace.Workspace, error) {
	return append([]workspace.Workspace(nil), p.values...), nil
}

type projectBridgeClient struct {
	service    *projectapp.Service
	last       bridge.Request
	startCalls int
}

func (c *projectBridgeClient) Forward(ctx context.Context, req bridge.Request) (bridge.Response, error) {
	c.last = req
	if req.Action == "start" {
		c.startCalls++
	}
	if req.Action == "inspect.project" {
		inspection, err := c.service.Inspect(ctx, req.WorkspaceID)
		return bridge.Response{Project: &inspection}, err
	}
	if req.Action == "inspect.server" {
		catalog := capability.Baseline(capability.Limits{})
		return bridge.Response{Server: &catalog}, nil
	}
	return bridge.Response{}, nil
}

func TestProjectInspectionDoesNotExecuteManifestCommandThroughMCP(t *testing.T) {
	root := t.TempDir()
	sentinel := filepath.Join(root, "SENTINEL")
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("# Agent bootstrap\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".shellbeam"), 0o700); err != nil {
		t.Fatal(err)
	}
	data := "schema_version=1\n[commands.never_run]\nshell=\"touch " + sentinel + "\"\n"
	if err := os.WriteFile(filepath.Join(root, ".shellbeam", "project.toml"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	const workspaceID = "ws_01K00000000000000000000000"
	service := projectapp.New(mcpProjectWorkspaceLookup{values: []workspace.Workspace{{ID: workspaceID, Root: root}}}, projectadapter.NewLoader())
	client := &projectBridgeClient{service: service}
	catalog := capability.Baseline(capability.Limits{})
	session, closeSession := currentSession(t, New(bridge.New(client), catalog))
	defer closeSession()
	res, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(`{"action":"inspect.project","workspace_id":"` + workspaceID + `"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError || client.last.Action != "inspect.project" || client.last.WorkspaceID != workspaceID || client.startCalls != 0 {
		t.Fatalf("result=%#v request=%#v starts=%d", res, client.last, client.startCalls)
	}
	body, ok := res.StructuredContent.(map[string]any)
	if !ok || body["project"] == nil {
		t.Fatalf("structured=%#v", res.StructuredContent)
	}
	project, ok := body["project"].(map[string]any)
	if !ok {
		t.Fatalf("project=%#v", body["project"])
	}
	bootstrap, ok := project["agent_bootstrap"].(map[string]any)
	if !ok || bootstrap["path"] != "AGENTS.md" || bootstrap["provenance"] != "workspace_convention" {
		t.Fatalf("agent_bootstrap=%#v project=%#v", bootstrap, project)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("manifest command executed through MCP inspect: %v", err)
	}
}
