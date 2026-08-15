package mcp

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	project "github.com/maemreyo/shellbeam/internal/core/project"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"
)

type readinessBridgeClient struct {
	last       bridge.Request
	startCalls int
}

func (c *readinessBridgeClient) Forward(_ context.Context, req bridge.Request) (bridge.Response, error) {
	c.last = req
	if req.Action == "start" {
		c.startCalls++
	}
	if req.Action == "inspect.readiness" {
		value := mcpProjectReadiness()
		return bridge.Response{Readiness: &value}, nil
	}
	if req.Action == "inspect.server" {
		catalog := capability.Baseline(capability.Limits{}).WithProjectReadiness(30000, 256)
		return bridge.Response{Server: &catalog}, nil
	}
	return bridge.Response{}, nil
}

func TestProjectReadinessMCPV2ForwardsWorkspaceWithoutSpawn(t *testing.T) {
	client := &readinessBridgeClient{}
	catalog := capability.Baseline(capability.Limits{}).WithProjectReadiness(30000, 256)
	session, closeSession := currentSession(t, New(bridge.New(client), catalog))
	defer closeSession()
	const workspaceID = "ws_01K00000000000000000000000"
	res, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{
		Name:      "local_shell",
		Arguments: json.RawMessage(`{"action":"inspect.readiness","workspace_id":"` + workspaceID + `"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError || client.last.Action != "inspect.readiness" || client.last.WorkspaceID != workspaceID || client.startCalls != 0 {
		t.Fatalf("result=%#v request=%#v starts=%d", res, client.last, client.startCalls)
	}
	body, ok := res.StructuredContent.(map[string]any)
	if !ok || body["readiness"] == nil {
		t.Fatalf("structured=%#v", res.StructuredContent)
	}
}

func TestProjectReadinessMCPV2RejectsCrossActionFields(t *testing.T) {
	client := &readinessBridgeClient{}
	session, closeSession := currentSession(t, New(bridge.New(client), capability.Baseline(capability.Limits{}).WithProjectReadiness(30000, 256)))
	defer closeSession()
	res, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{
		Name:      "local_shell",
		Arguments: json.RawMessage(`{"action":"inspect.readiness","workspace_id":"ws_01K00000000000000000000000","operation_id":"op-1"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || client.last.Action != "" {
		t.Fatalf("invalid request reached bridge: result=%#v last=%#v", res, client.last)
	}
}

func TestLegacyCatalogProjectionOmitsProjectReadiness(t *testing.T) {
	catalog := capability.Baseline(capability.Limits{}).WithProjectReadiness(30000, 256)
	legacy := legacyCatalogView(catalog)
	if _, ok := legacy.Features[capability.FeatureProjectReadiness]; ok {
		t.Fatalf("legacy features expose readiness: %#v", legacy.Features)
	}
	if len(legacy.ReadinessSchemaVersions) != 0 || len(legacy.ReadinessRequirementKinds) != 0 ||
		legacy.Limits.ReadinessCacheTTLMS != 0 || legacy.Limits.ReadinessCacheEntries != 0 {
		t.Fatalf("legacy catalog exposes readiness metadata: %#v", legacy)
	}
	if !reflect.DeepEqual(catalog.ReadinessSchemaVersions, []int{1}) || catalog.Limits.ReadinessCacheTTLMS != 30000 {
		t.Fatalf("legacy projection mutated source catalog: %#v", catalog)
	}
}

func mcpProjectReadiness() project.Readiness {
	return project.Readiness{
		SchemaVersion:         project.ReadinessSchemaVersion,
		State:                 project.ReadinessReady,
		RepositoryID:          "repo_01K00000000000000000000000",
		WorkspaceID:           "ws_01K00000000000000000000000",
		ManifestDigest:        strings.Repeat("a", 64),
		ManifestSchemaVersion: project.ManifestSchemaV2,
		CapturedAt:            time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC),
		CacheQuality:          project.CacheFresh,
		Checks:                []project.ReadinessCheck{{ID: "git", Kind: project.RequirementExecutable, Required: true, Status: project.CheckAvailable}},
	}
}
