package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	environmentcore "github.com/maemreyo/shellbeam/internal/core/environment"
	processcore "github.com/maemreyo/shellbeam/internal/core/process"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"
)

type a25MCPClient struct {
	last       bridge.Request
	startCalls int
}

func (c *a25MCPClient) Forward(_ context.Context, req bridge.Request) (bridge.Response, error) {
	c.last = req
	if req.Action == "start" {
		c.startCalls++
	}
	switch req.Action {
	case "inspect.environment":
		snapshot := environmentcore.Snapshot{
			SchemaVersion:          environmentcore.SnapshotSchemaVersion,
			SnapshotID:             "env_" + strings.Repeat("a", 64),
			CapturedAt:             time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC),
			Quality:                environmentcore.QualityComplete,
			EnvironmentFingerprint: strings.Repeat("b", 64),
			FingerprintVersion:     environmentcore.FingerprintVersion,
			Platform:               environmentcore.Platform{OS: "darwin", Architecture: "arm64"},
			Execution:              environmentcore.ExecutionContext{Mode: "shell", Identity: "/bin/zsh"},
			Path:                   environmentcore.PathObservation{Digest: strings.Repeat("c", 64), EntryCount: 2, Quality: environmentcore.QualityComplete},
		}
		return bridge.Response{Environment: &snapshot}, nil
	case "inspect.process":
		target := req.ProcessInspect.Target
		observation := processcore.Observation{
			SchemaVersion: processcore.SchemaVersion,
			ObservedAt:    time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC),
			Quality:       processcore.QualityComplete,
			Target:        target,
			Root:          &processcore.ProcessFact{PID: target.PID, Relation: processcore.RelationExternal, State: processcore.StateRunning},
		}
		return bridge.Response{Process: &observation}, nil
	case "inspect.server":
		catalog := capability.Baseline(capability.Limits{})
		return bridge.Response{Server: &catalog}, nil
	default:
		return bridge.Response{}, nil
	}
}

func TestA25MCPV2ForwardsObservationRequestsWithoutSpawn(t *testing.T) {
	client := &a25MCPClient{}
	session, closeSession := currentSession(t, New(bridge.New(client), capability.Baseline(capability.Limits{})))
	defer closeSession()

	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 1 || tools.Tools[0].Name != "local_shell" {
		t.Fatalf("tools=%#v", tools.Tools)
	}

	envRes, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(`{"action":"inspect.environment","workspace_id":"ws_01K00000000000000000000000","freshness":"refresh","execution":{"mode":"shell","identity":"/bin/zsh"}}`)})
	if err != nil {
		t.Fatal(err)
	}
	if envRes.IsError || client.startCalls != 0 || client.last.Action != "inspect.environment" || client.last.EnvironmentInspect.Freshness != environmentcore.FreshnessRefresh || client.last.EnvironmentInspect.Execution == nil || client.last.EnvironmentInspect.Execution.Identity != "/bin/zsh" {
		t.Fatalf("env result=%#v request=%#v starts=%d", envRes, client.last, client.startCalls)
	}
	envBody, ok := envRes.StructuredContent.(map[string]any)
	if !ok || envBody["environment"] == nil {
		t.Fatalf("environment body=%#v", envRes.StructuredContent)
	}

	procRes, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(`{"action":"inspect.process","process_target":{"kind":"pid","pid":123},"include_ports":true}`)})
	if err != nil {
		t.Fatal(err)
	}
	if procRes.IsError || client.startCalls != 0 || client.last.Action != "inspect.process" || client.last.ProcessInspect.Target.Kind != processcore.TargetPID || client.last.ProcessInspect.Target.PID != 123 || !client.last.ProcessInspect.IncludePorts {
		t.Fatalf("process result=%#v request=%#v starts=%d", procRes, client.last, client.startCalls)
	}
	procBody, ok := procRes.StructuredContent.(map[string]any)
	if !ok || procBody["process"] == nil {
		t.Fatalf("process body=%#v", procRes.StructuredContent)
	}

	encoded, err := json.Marshal([]any{envBody, procBody})
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, forbidden := range []string{"low-entropy-secret", "raw_path", "environment_values"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("MCP observation response leaked forbidden marker %q: %s", forbidden, text)
		}
	}
}
