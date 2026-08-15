package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	mutationapp "github.com/maemreyo/shellbeam/internal/app/mutationscope"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	core "github.com/maemreyo/shellbeam/internal/core/mutationscope"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"
)

type a26MCPClient struct {
	last   bridge.Request
	starts int
}

func (c *a26MCPClient) Forward(_ context.Context, req bridge.Request) (bridge.Response, error) {
	c.last = req
	if req.Action == "start" {
		c.starts++
	}
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	switch req.Action {
	case "mutation_scope.set":
		in := req.MutationScopeSet
		scope := core.Scope{SchemaVersion: 1, ScopeID: in.ScopeID, ActivityID: in.ActivityID, WorkspaceID: in.WorkspaceID, Mode: in.Mode, Paths: append([]string(nil), in.Paths...), DeclaredAt: now, ExpiresAt: now.Add(15 * time.Minute), RevisionID: in.MutationID}
		receipt := core.MutationReceipt{SchemaVersion: 1, MutationID: in.MutationID, RequestFingerprint: strings.Repeat("a", 64), Result: core.ResultSet, SetEffect: core.SetEffectCreated, ScopeID: in.ScopeID, CommittedAt: now, ExpiresAt: scope.ExpiresAt}
		result := mutationapp.MutationResult{Receipt: receipt, Scope: &scope, CurrentRevision: true, AdvisoryLimit: core.MaxAdvisories}
		return bridge.Response{Mutation: &result}, nil
	case "mutation_scope.release":
		in := req.MutationScopeRelease
		result := mutationapp.MutationResult{Receipt: core.MutationReceipt{SchemaVersion: 1, MutationID: in.MutationID, RequestFingerprint: strings.Repeat("b", 64), Result: core.ResultAlreadyAbsent, ScopeID: in.ScopeID, CommittedAt: now}, AdvisoryLimit: core.MaxAdvisories}
		return bridge.Response{Mutation: &result}, nil
	case "inspect.mutation_scopes":
		result := core.InspectResult{ActiveScopes: []core.Scope{}, Advisories: []core.Advisory{}, ActiveScopeLimit: core.MaxActiveScopesPerWorkspace, AdvisoryLimit: core.MaxAdvisories}
		return bridge.Response{MutationScopes: &result}, nil
	case "inspect.server":
		catalog := capability.Baseline(capability.Limits{}).WithMutationScopes(16, 64, 16, 256, 32, 900000, 1800000)
		return bridge.Response{Server: &catalog}, nil
	}
	return bridge.Response{}, nil
}

func TestA26MCPV2ForwardsMutationScopesThroughSingleToolWithSafeSummary(t *testing.T) {
	client := &a26MCPClient{}
	catalog := capability.Baseline(capability.Limits{}).WithMutationScopes(16, 64, 16, 256, 32, 900000, 1800000)
	session, closeSession := currentSession(t, New(bridge.New(client), catalog))
	defer closeSession()
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 1 || tools.Tools[0].Name != "local_shell" {
		t.Fatalf("tools=%#v", tools.Tools)
	}

	res, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(`{"action":"mutation_scope.set","mutation_id":"mutation-1","scope_id":"scope-a","activity_id":"activity-a","workspace_id":"ws_01K00000000000000000000000","mode":"mutate","paths":["secret-ish/path/**"],"ttl_ms":900000}`)})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError || client.starts != 0 || client.last.Action != "mutation_scope.set" || client.last.MutationScopeSet.Paths[0] != "secret-ish/path/**" {
		t.Fatalf("res=%#v last=%#v starts=%d", res, client.last, client.starts)
	}
	body, ok := res.StructuredContent.(map[string]any)
	if !ok || body["mutation"] == nil {
		t.Fatalf("body=%#v", res.StructuredContent)
	}
	if len(res.Content) != 1 {
		t.Fatalf("content=%#v", res.Content)
	}
	text, ok := res.Content[0].(*mcpgo.TextContent)
	if !ok {
		t.Fatalf("text content=%T", res.Content[0])
	}
	if strings.Contains(text.Text, "secret-ish/path") || strings.Contains(text.Text, "src/") {
		t.Fatalf("summary leaked selector: %q", text.Text)
	}

	inspect, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(`{"action":"inspect.mutation_scopes","workspace_id":"ws_01K00000000000000000000000","activity_id":"activity-a"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if inspect.IsError || client.last.Action != "inspect.mutation_scopes" || client.last.MutationScopeInspect.ActivityID != "activity-a" {
		t.Fatalf("inspect=%#v last=%#v", inspect, client.last)
	}
	inspectBody, ok := inspect.StructuredContent.(map[string]any)
	if !ok || inspectBody["mutation_scopes"] == nil {
		t.Fatalf("inspect body=%#v", inspect.StructuredContent)
	}

	release, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(`{"action":"mutation_scope.release","mutation_id":"release-1","scope_id":"scope-a"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if release.IsError || client.last.MutationScopeRelease.MutationID != "release-1" {
		t.Fatalf("release=%#v last=%#v", release, client.last)
	}
}

func TestA26LegacyCatalogProjectionStripsMutationScopeCapabilities(t *testing.T) {
	modern := capability.Baseline(capability.Limits{}).WithMutationScopes(16, 64, 16, 256, 32, 900000, 1800000)
	legacy := legacyCatalogView(modern)
	if _, ok := legacy.Features[capability.FeatureMutationScopes]; ok || len(legacy.MutationScopeSchemaVersions) != 0 {
		t.Fatalf("legacy feature leaked: %#v", legacy)
	}
	l := legacy.Limits
	if l.MutationScopeActivePerActivity != 0 || l.MutationScopeActivePerWorkspace != 0 || l.MutationScopePathsPerScope != 0 || l.MutationScopeSelectorBytes != 0 || l.MutationScopeAdvisories != 0 || l.MutationScopeMinTTLMS != 0 || l.MutationScopeDefaultTTLMS != 0 || l.MutationScopeMaxTTLMS != 0 {
		t.Fatalf("legacy limits leaked: %#v", l)
	}
}
