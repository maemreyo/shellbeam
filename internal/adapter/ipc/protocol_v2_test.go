package ipc

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/failure"
)

func TestV1CompatibilityFixtures(t *testing.T) {
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
		{"unsupported-feature.json", failure.FeatureUnavailable},
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

func TestIPCV2ErrorsPreserveHeader(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "v2", "unsupported-feature.json"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeRequestV2(bytesReaderV2(data))
	if !errors.Is(err, failure.FeatureUnavailable) {
		t.Fatalf("error=%v", err)
	}
	if got.RequestID != "v2-feature" || got.Action != "inspect.workspace" || got.IPVersion != 2 {
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

func TestIPCV2ClientRejectsUnsupportedFeatureBeforeNetwork(t *testing.T) {
	called := false
	client := &Client{http: &http.Client{Transport: roundTripV2Func(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, errors.New("network should not run")
	})}}
	_, err := client.CallV2(context.Background(), RequestV2{IPVersion: 2, Kind: "request", RequestID: "x", Action: "inspect.workspace"})
	if !errors.Is(err, failure.FeatureUnavailable) {
		t.Fatalf("error=%v want feature_unavailable", err)
	}
	if called {
		t.Fatal("unsupported feature reached transport")
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
