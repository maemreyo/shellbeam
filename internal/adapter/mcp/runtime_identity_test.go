package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"
)

func testRuntimeIdentity(revision, digest string) capability.RuntimeIdentity {
	return capability.RuntimeIdentity{SchemaVersion: 1, Revision: revision, BinarySHA256: digest}
}

func TestMCPV2ProvenBinarySkewFailsBeforeRequestedDaemonOperation(t *testing.T) {
	local := testRuntimeIdentity("aaaaaaaa", strings.Repeat("1", 64))
	daemon := testRuntimeIdentity("aaaaaaaa", strings.Repeat("2", 64))
	catalog := capability.Baseline(capability.Limits{}).WithRuntime(daemon)
	fake := &discoveryClient{catalog: catalog}
	session, closeSession := currentSession(t, newServerWithRuntimeIdentity(bridge.New(fake), catalog, local))
	defer closeSession()

	res, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{
		Name: "local_shell", Arguments: json.RawMessage(`{"action":"start","operation_id":"skew","command":"true","cwd":"/tmp"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || fake.startCalls != 0 {
		t.Fatalf("result=%#v startCalls=%d", res, fake.startCalls)
	}
	body, _ := res.StructuredContent.(map[string]any)
	errBody, _ := body["error"].(map[string]any)
	details, _ := errBody["details"].(map[string]any)
	if errBody["code"] != "runtime_version_mismatch" || details["reason"] != "binary_identity_mismatch" || details["recovery"] != "restart_daemon" || details["mcp_revision"] != "aaaaaaaa" || details["daemon_revision"] != "aaaaaaaa" {
		t.Fatalf("body=%#v", body)
	}
}

func TestMCPV2RevisionSkewFailsWhenDigestUnavailable(t *testing.T) {
	local := testRuntimeIdentity("aaaaaaaa", "")
	daemon := testRuntimeIdentity("bbbbbbbb", "")
	catalog := capability.Baseline(capability.Limits{}).WithRuntime(daemon)
	fake := &discoveryClient{catalog: catalog}
	session, closeSession := currentSession(t, newServerWithRuntimeIdentity(bridge.New(fake), catalog, local))
	defer closeSession()
	res, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(`{"action":"start","operation_id":"revision-skew","command":"true","cwd":"/tmp"}`)})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := res.StructuredContent.(map[string]any)
	errBody, _ := body["error"].(map[string]any)
	details, _ := errBody["details"].(map[string]any)
	if !res.IsError || fake.startCalls != 0 || errBody["code"] != "runtime_version_mismatch" || details["reason"] != "revision_mismatch" {
		t.Fatalf("result=%#v startCalls=%d", res, fake.startCalls)
	}
}

func TestMCPV2MatchingOrUnknownRuntimeIdentityRemainsCompatible(t *testing.T) {
	matching := testRuntimeIdentity("aaaaaaaa", strings.Repeat("1", 64))
	for name, daemonRuntime := range map[string]*capability.RuntimeIdentity{
		"matching": &matching,
		"unknown":  nil,
	} {
		t.Run(name, func(t *testing.T) {
			catalog := capability.Baseline(capability.Limits{})
			if daemonRuntime != nil {
				catalog = catalog.WithRuntime(*daemonRuntime)
			}
			fake := &discoveryClient{catalog: catalog}
			session, closeSession := currentSession(t, newServerWithRuntimeIdentity(bridge.New(fake), catalog, matching))
			defer closeSession()
			res, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(`{"action":"start","operation_id":"compatible","command":"true","cwd":"/tmp"}`)})
			if err != nil {
				t.Fatal(err)
			}
			if res.IsError || fake.startCalls != 1 {
				t.Fatalf("result=%#v startCalls=%d", res, fake.startCalls)
			}
		})
	}
}

func TestLegacyCatalogProjectionOmitsRuntimeIdentity(t *testing.T) {
	runtime := testRuntimeIdentity("aaaaaaaa", strings.Repeat("1", 64))
	modern := capability.Baseline(capability.Limits{}).WithRuntime(runtime)
	legacy := legacyCatalogView(modern)
	if legacy.Runtime != nil {
		t.Fatalf("legacy catalog leaked runtime identity: %#v", legacy.Runtime)
	}
}
