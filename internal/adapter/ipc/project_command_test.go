//go:build linux || darwin

package ipc

import (
	"reflect"
	"testing"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	daemon "github.com/maemreyo/shellbeam/internal/app/daemon"
)

func TestProjectCommandIPCV2AcceptsClosedTypedStartAndForwardsFields(t *testing.T) {
	raw := []byte(`{"ipc_version":2,"kind":"request","request_id":"typed","action":"start","operation_id":"typed-op","workspace_id":"ws_01K00000000000000000000000","project_command_id":"test_package","params":{"package":"./internal/app","count":"3"},"tty":true,"timeout_ms":5000,"yield_time_ms":0,"max_output_bytes":20000}`)
	got, err := decodeRequestV2(bytesReaderV2(raw))
	if err != nil {
		t.Fatal(err)
	}
	if got.ProjectCommandID != "test_package" || !reflect.DeepEqual(got.Params, map[string]string{"package": "./internal/app", "count": "3"}) || got.Command != "" || len(got.Argv) != 0 || got.CWD != "" {
		t.Fatalf("decoded=%#v", got)
	}
	bridgeReq := requestV2FromBridge(bridge.Request{ProtocolVersion: 2, Action: "start", Start: daemon.StartRequest{
		OperationID: "typed-op", WorkspaceID: "ws_01K00000000000000000000000",
		ProjectCommandID: "test_package", Params: map[string]string{"package": "./internal/app"},
		TTY: true, TimeoutMS: 5000,
	}})
	if bridgeReq.ProjectCommandID != "test_package" || !reflect.DeepEqual(bridgeReq.Params, map[string]string{"package": "./internal/app"}) {
		t.Fatalf("bridge request=%#v", bridgeReq)
	}
	bridgeReq.Params["package"] = "mutated"
	if bridgeReq.Params["package"] == "./internal/app" {
		t.Fatal("test mutation did not apply")
	}
}

func TestProjectCommandIPCV2RejectsRawCoexistenceParamsWithoutCommandAndCrossActionFields(t *testing.T) {
	base := `"ipc_version":2,"kind":"request","request_id":"typed","action":"start","operation_id":"typed-op","workspace_id":"ws_01K00000000000000000000000","project_command_id":"test_package"`
	invalid := []string{
		`{` + base + `,"command":"true"}`,
		`{` + base + `,"argv":["true"]}`,
		`{` + base + `,"cwd":"/repo"}`,
		`{"ipc_version":2,"kind":"request","request_id":"typed","action":"start","operation_id":"typed-op","workspace_id":"ws_01K00000000000000000000000","params":{"name":"x"}}`,
		`{"ipc_version":2,"kind":"request","request_id":"p","action":"poll","session_id":"s","project_command_id":"test"}`,
	}
	for _, raw := range invalid {
		if got, err := decodeRequestV2(bytesReaderV2([]byte(raw))); err == nil {
			t.Fatalf("invalid typed request accepted: %#v", got)
		}
	}
}

func TestProjectCommandIPCV2RejectsUnboundedParams(t *testing.T) {
	params := make(map[string]string, 33)
	for i := 0; i < 33; i++ {
		params[string(rune('a'+i%26))+string(rune('a'+i/26))] = "v"
	}
	req := RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "typed", Action: "start", OperationID: "typed-op",
		WorkspaceID: "ws_01K00000000000000000000000", ProjectCommandID: "test_package", Params: params,
	}
	if err := validateRequestV2(req); err == nil {
		t.Fatal("unbounded params accepted")
	}
}
