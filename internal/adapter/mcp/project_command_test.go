package mcp

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"
)

type projectCommandBridgeClient struct {
	last bridge.Request
}

func (c *projectCommandBridgeClient) Forward(_ context.Context, req bridge.Request) (bridge.Response, error) {
	c.last = req
	result := receipt.Result{
		SchemaVersion: 2,
		Operation:     receipt.OperationResult{OperationID: req.Start.OperationID, SessionID: "s", State: receipt.OperationRunning},
		Child:         &receipt.ChildResult{State: receipt.ChildRunning}, Output: receipt.OutputResult{CanonicalStream: "combined"},
	}
	return bridge.Response{Result: &result}, nil
}

func TestProjectCommandMCPV2ForwardsTypedStartWithoutRawExecutionFields(t *testing.T) {
	client := &projectCommandBridgeClient{}
	catalog := capability.Baseline(capability.Limits{}).WithTypedProjectCommands([]string{"go"})
	session, closeSession := currentSession(t, New(bridge.New(client), catalog))
	defer closeSession()
	res, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(`{"action":"start","operation_id":"typed-op","workspace_id":"ws_01K00000000000000000000000","project_command_id":"test_package","params":{"package":"./internal/app","count":"3"},"tty":true,"timeout_ms":5000}`)})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("result=%#v", res)
	}
	start := client.last.Start
	if client.last.ProtocolVersion != 2 || start.ProjectCommandID != "test_package" || !reflect.DeepEqual(start.Params, map[string]string{"package": "./internal/app", "count": "3"}) || start.Command != "" || len(start.Argv) != 0 || start.CWD != "" {
		t.Fatalf("request=%#v", client.last)
	}
}

func TestProjectCommandMCPV2RejectsRawCoexistenceAndParamsWithoutCommand(t *testing.T) {
	client := &projectCommandBridgeClient{}
	session, closeSession := currentSession(t, New(bridge.New(client), capability.Baseline(capability.Limits{}).WithTypedProjectCommands([]string{"go"})))
	defer closeSession()
	invalid := []string{
		`{"action":"start","operation_id":"typed-op","workspace_id":"ws_01K00000000000000000000000","project_command_id":"test_package","command":"true"}`,
		`{"action":"start","operation_id":"typed-op","workspace_id":"ws_01K00000000000000000000000","project_command_id":"test_package","argv":["true"]}`,
		`{"action":"start","operation_id":"typed-op","workspace_id":"ws_01K00000000000000000000000","project_command_id":"test_package","cwd":"/repo"}`,
		`{"action":"start","operation_id":"typed-op","workspace_id":"ws_01K00000000000000000000000","params":{"name":"x"}}`,
	}
	for _, raw := range invalid {
		res, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(raw)})
		if err != nil {
			t.Fatal(err)
		}
		if !res.IsError {
			t.Fatalf("invalid typed request accepted: %s result=%#v", raw, res)
		}
	}
}

func TestProjectCommandLegacyGenerationRejectsTypedFields(t *testing.T) {
	raw := []byte(`{"action":"start","operation_id":"typed-op","workspace_id":"ws_01K00000000000000000000000","project_command_id":"test_package","params":{"name":"x"}}`)
	in, err := decodeInput(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateForVersion(1, in, raw); err == nil {
		t.Fatal("legacy generation accepted typed project command fields")
	}
}

func TestLegacyCatalogHidesTypedProjectCommandSurfaceAndReceiptV3(t *testing.T) {
	modern := capability.Baseline(capability.Limits{}).WithTypedProjectCommands([]string{"go"})
	legacy := legacyCatalogView(modern)
	if _, ok := legacy.Features[capability.FeatureTypedProjectCommands]; ok {
		t.Fatalf("legacy features=%#v", legacy.Features)
	}
	if len(legacy.TypedCommandVersions) != 0 || legacy.TypedCommandManifestVersion != 0 || len(legacy.TypedCommandParameterKinds) != 0 || len(legacy.TypedCommandPackageProviders) != 0 {
		t.Fatalf("legacy typed metadata=%#v", legacy)
	}
	if !reflect.DeepEqual(legacy.ReceiptSchemaVersions, []int{1, 2}) {
		t.Fatalf("legacy receipt versions=%v", legacy.ReceiptSchemaVersions)
	}
}
