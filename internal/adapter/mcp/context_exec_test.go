package mcp

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	contextcore "github.com/maemreyo/shellbeam/internal/core/contextexec"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"
)

type contextExecMCPClient struct {
	calls int
	last  bridge.Request
}

func (c *contextExecMCPClient) Forward(_ context.Context, req bridge.Request) (bridge.Response, error) {
	c.calls++
	c.last = req
	response := bridge.Response{}
	field := reflect.ValueOf(&response).Elem().FieldByName("ContextExec")
	if field.IsValid() && field.CanSet() {
		state := &contextcore.PublicState{SchemaVersion: 1, ContextExecID: "ctxexec_public_01", SessionID: "session_public_01", AuthorityEpoch: 4, Lifecycle: contextcore.LifecycleHelperRequested, RequestedExecutable: "go"}
		field.Set(reflect.ValueOf(state))
	}
	return response, nil
}

func TestContextExecMCPV2UsesExistingToolAndCarriesExactRequestAndSafeState(t *testing.T) {
	client := &contextExecMCPClient{}
	session, closeSession := currentSession(t, New(bridge.New(client), capability.Baseline(capability.Limits{})))
	defer closeSession()

	res, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(`{"action":"context.exec","context_exec_id":"ctxexec_public_01","session_id":"session_public_01","authority_epoch":4,"argv":["go","test"],"timeout_ms":30000,"max_output_bytes":1048576}`)})
	if err != nil || res.IsError || client.calls != 1 {
		t.Fatalf("result=%#v err=%v calls=%d", res, err, client.calls)
	}
	carrier := reflect.ValueOf(client.last).FieldByName("ContextExec")
	if !carrier.IsValid() {
		t.Fatal("bridge request lacks ContextExec carrier")
	}
	wire, err := json.Marshal(carrier.Interface())
	if err != nil {
		t.Fatal(err)
	}
	text := string(wire)
	for _, required := range []string{"ctxexec_public_01", "session_public_01", `"authority_epoch":4`, `"argv":["go","test"]`, `"timeout_ms":30000`, `"max_output_bytes":1048576`} {
		if !strings.Contains(text, required) {
			t.Fatalf("bridge context request lost %s: %s", required, text)
		}
	}
	body, ok := res.StructuredContent.(map[string]any)
	if !ok || body["context_exec"] == nil {
		t.Fatalf("context.exec body=%#v", res.StructuredContent)
	}
	publicWire, err := json.Marshal(body["context_exec"])
	if err != nil {
		t.Fatal(err)
	}
	publicText := string(publicWire)
	for _, forbidden := range []string{"request_fingerprint", "provider_generation", "shell_identity", "cwd_observed", `"helper":`, "environment"} {
		if strings.Contains(publicText, forbidden) {
			t.Fatalf("unsafe public context state contains %q: %s", forbidden, publicText)
		}
	}
}
