package ipc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/failure"
)

func TestCompatibilityV1Fixtures(t *testing.T) {
	want := map[string]string{
		"start.json": "start",
		"poll.json":  "poll",
		"write.json": "write",
		"kill.json":  "kill",
	}
	for name, action := range want {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", "v1", name))
			if err != nil {
				t.Fatal(err)
			}
			got, err := decodeRequest(bytesReaderV2(data))
			if err != nil {
				t.Fatal(err)
			}
			if got.IPVersion != 1 || got.Payload.Action != action {
				t.Fatalf("decoded=%#v", got)
			}
		})
	}
}

func TestIPCV2InspectServerFixture(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "v2", "inspect-server.json"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeRequestV2(bytesReaderV2(data))
	if err != nil {
		t.Fatal(err)
	}
	if got.IPVersion != 2 || got.Kind != "request" || got.Action != "inspect.server" || got.RequestID != "v2-inspect" {
		t.Fatalf("decoded=%#v", got)
	}
}

func TestIPCV2RejectsUnknownVersionActionAndExtraProperties(t *testing.T) {
	tests := []struct {
		name string
		code failure.Code
	}{
		{"unknown-version.json", failure.FeatureUnavailable},
		{"unknown-action.json", failure.InvalidInput},
		{"extra-property.json", failure.InvalidInput},
		{"read-output-missing-selector.json", failure.InvalidInput},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", "v2", tt.name))
			if err != nil {
				t.Fatal(err)
			}
			_, err = decodeRequestV2(bytesReaderV2(data))
			if !errors.Is(err, tt.code) {
				t.Fatalf("error=%v want %s", err, tt.code)
			}
		})
	}
}

func bytesReaderV2(data []byte) *bytes.Reader { return bytes.NewReader(data) }

func TestIPCV2InvalidRequestsPreserveHeader(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "v2", "read-output-missing-selector.json"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeRequestV2(bytesReaderV2(data))
	if !errors.Is(err, failure.InvalidInput) {
		t.Fatalf("error=%v", err)
	}
	if got.RequestID != "v2-feature" || got.Action != "read_output" || got.IPVersion != 2 {
		t.Fatalf("partial header lost: %#v", got)
	}
}

func TestIPCV2RejectsCrossBranchFields(t *testing.T) {
	raw := []byte(`{"ipc_version":2,"kind":"request","request_id":"x","action":"start","operation_id":"op","command":"echo hi","cwd":"/tmp","kill_id":"k"}`)
	_, err := decodeRequestV2(bytes.NewReader(raw))
	if !errors.Is(err, failure.InvalidInput) {
		t.Fatalf("cross-branch field error=%v, want invalid_input", err)
	}
}

type roundTripV2Func func(*http.Request) (*http.Response, error)

func (f roundTripV2Func) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestIPCV2ClientRejectsInvalidReadOutputBeforeNetwork(t *testing.T) {
	called := false
	client := &Client{http: &http.Client{Transport: roundTripV2Func(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, errors.New("network should not run")
	})}}
	_, err := client.CallV2(context.Background(), RequestV2{IPVersion: 2, Kind: "request", RequestID: "x", Action: "read_output"})
	if !errors.Is(err, failure.InvalidInput) {
		t.Fatalf("error=%v want invalid_input", err)
	}
	if called {
		t.Fatal("invalid read_output reached transport")
	}
}

func TestAgentExecutionA1PollMarshalDecodePreservesYieldControls(t *testing.T) {
	want := RequestV2{IPVersion: 2, Kind: "request", RequestID: "poll", Action: "poll", SessionID: "s", Cursor: 4, YieldMS: 1000, MaxOutputBytes: 4096}
	b, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateV2FieldSet(b, "poll"); err != nil {
		t.Fatalf("field-set %s: %#v", b, err)
	}
	var got RequestV2
	if err := strictDecodeV2(b, &got); err != nil {
		t.Fatalf("strict decode %s: %v", b, err)
	}
	if err := validateRequestV2(got); err != nil {
		t.Fatalf("request validate %s: %#v", b, err)
	}
	if got.YieldMS != want.YieldMS || got.MaxOutputBytes != want.MaxOutputBytes || got.Cursor != want.Cursor {
		t.Fatalf("got=%#v want=%#v", got, want)
	}
}

func TestIPCV2WorkspaceAddressAndHintContract(t *testing.T) {
	raw := []byte(`{"ipc_version":2,"kind":"request","request_id":"x","action":"start","operation_id":"op","command":"true","workspace_id":"ws_01K00000000000000000000000","workspace_hint":{"branch":"main"}}`)
	got, err := decodeRequestV2(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if got.WorkspaceID != "ws_01K00000000000000000000000" || got.CWD != "" || got.WorkspaceHint == nil || got.WorkspaceHint.Branch != "main" {
		t.Fatalf("decoded=%#v", got)
	}
	for _, invalid := range [][]byte{
		[]byte(`{"ipc_version":2,"kind":"request","request_id":"x","action":"start","operation_id":"op","command":"true","workspace_id":"ws_01K00000000000000000000000","cwd":"/tmp"}`),
		[]byte(`{"ipc_version":2,"kind":"request","request_id":"x","action":"start","operation_id":"op","command":"true","cwd":"relative"}`),
	} {
		if _, err := decodeRequestV2(bytes.NewReader(invalid)); !errors.Is(err, failure.InvalidInput) {
			t.Fatalf("invalid address err=%v", err)
		}
	}
}

func TestIPCV2ArgvIntentContract(t *testing.T) {
	raw := []byte(`{"ipc_version":2,"kind":"request","request_id":"x","action":"start","operation_id":"op","argv":["git","status"],"cwd":"/tmp","intent":{"kind":"inspect"}}`)
	got, err := decodeRequestV2(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Argv) != 2 || got.Argv[0] != "git" || got.Intent == nil || got.Intent.Kind != "inspect" {
		t.Fatalf("decoded=%#v", got)
	}
	bad := []byte(`{"ipc_version":2,"kind":"request","request_id":"x","action":"start","operation_id":"op","command":"true","argv":["true"],"cwd":"/tmp"}`)
	if _, err := decodeRequestV2(bytes.NewReader(bad)); !errors.Is(err, failure.InvalidInput) {
		t.Fatalf("err=%v", err)
	}
}

func TestIPCV2CodeInspectClosedContract(t *testing.T) {
	valid := []byte(`{"ipc_version":2,"kind":"request","request_id":"code","action":"inspect.code","workspace_id":"ws_01K00000000000000000000000","activity_id":"ZMR-111-validator","code_query":{"kind":"diagnostics","scope":"changed_files"}}`)
	got, err := decodeRequestV2(bytes.NewReader(valid))
	if err != nil {
		t.Fatalf("valid inspect.code rejected: %v", err)
	}
	if got.CodeQuery == nil || got.CodeQuery.Kind != "diagnostics" || got.WorkspaceID == "" || got.ActivityID == "" {
		t.Fatalf("decoded=%#v", got)
	}

	invalid := [][]byte{
		[]byte(`{"ipc_version":2,"kind":"request","request_id":"code","action":"inspect.code","code_query":{"kind":"diagnostics","scope":"changed_files"}}`),
		[]byte(`{"ipc_version":2,"kind":"request","request_id":"code","action":"inspect.code","workspace_id":"ws_01K00000000000000000000000"}`),
		[]byte(`{"ipc_version":2,"kind":"request","request_id":"code","action":"inspect.code","workspace_id":"ws_01K00000000000000000000000","code_query":{"kind":"diagnostics","scope":"changed_files","uri":"file:///tmp/main.go"}}`),
		[]byte(`{"ipc_version":2,"kind":"request","request_id":"code","action":"inspect.code","workspace_id":"ws_01K00000000000000000000000","code_query":{"kind":"diagnostics","scope":"changed_files","document_version":1}}`),
		[]byte(`{"ipc_version":2,"kind":"request","request_id":"code","action":"inspect.code","workspace_id":"ws_01K00000000000000000000000","code_query":{"kind":"diagnostics","scope":"changed_files","jsonrpc_id":7}}`),
		[]byte(`{"ipc_version":2,"kind":"request","request_id":"code","action":"inspect.code","workspace_id":"ws_01K00000000000000000000000","code_query":{"kind":"definition","path":"main.go","line":1}}`),
		[]byte(`{"ipc_version":2,"kind":"request","request_id":"code","action":"inspect.code","workspace_id":"ws_01K00000000000000000000000","code_query":{"kind":"diagnostics","scope":"file","path":"main.go","line":1,"column":1}}`),
		[]byte(`{"ipc_version":2,"kind":"request","request_id":"code","action":"inspect.code","workspace_id":"ws_01K00000000000000000000000","code_query":{"kind":"diagnostics","scope":"repository"}}`),
		[]byte(`{"ipc_version":2,"kind":"request","request_id":"code","action":"inspect.code","workspace_id":"ws_01K00000000000000000000000","code_query":{"kind":"hover","path":"main.go","line":1,"column":1}}`),
		[]byte(`{"ipc_version":2,"kind":"request","request_id":"code","action":"inspect.code","workspace_id":"ws_01K00000000000000000000000","code_query":{"kind":"diagnostics","scope":"changed_files","provider":"mystery"}}`),
	}
	for i, raw := range invalid {
		if _, err := decodeRequestV2(bytes.NewReader(raw)); !errors.Is(err, failure.InvalidInput) {
			t.Fatalf("invalid case %d error=%v", i, err)
		}
	}
}

func TestStrictDecodeV2RejectsAmbiguousJSON(t *testing.T) {
	type payload struct {
		Action string `json:"action"`
	}
	cases := map[string][]byte{
		"duplicate":    []byte(`{"action":"start","action":"poll"}`),
		"wrong-case":   []byte(`{"Action":"start"}`),
		"unknown":      []byte(`{"action":"start","unknown":1}`),
		"invalid-utf8": append([]byte(`{"action":"`), append([]byte{0xff}, []byte(`"}`)...)...),
		"trailing":     []byte(`{"action":"start"} {}`),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			var got payload
			if err := strictDecodeV2(raw, &got); err == nil {
				t.Fatalf("accepted %q", raw)
			}
		})
	}
}
