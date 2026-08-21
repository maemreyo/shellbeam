package browserbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	protocol "github.com/maemreyo/shellbeam/internal/core/browserbridge"
)

func TestHelloParsesLenientlyAndReportsSupportedVersions(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader(`{"protocol_version":1,"verb":"hello","future_field_from_a_newer_extension":true}`)
	if err := Serve(context.Background(), NewPlanner(&fakeReader{}), in, &out); err != nil {
		t.Fatalf("serve: %v", err)
	}
	var resp protocol.Response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Status != protocol.StatusOK || resp.Verb != protocol.VerbHello {
		t.Fatalf("hello response = %+v", resp)
	}
	if len(resp.SupportedVersions) != 1 || resp.SupportedVersions[0] != protocol.ProtocolVersion {
		t.Fatalf("supported versions = %v", resp.SupportedVersions)
	}
}

func TestNonHelloVerbsRejectUnknownFields(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader(`{"protocol_version":1,"verb":"activity_facts","correlation_id":"wt","command":"rm -rf /"}`)
	if err := Serve(context.Background(), NewPlanner(&fakeReader{}), in, &out); err != nil {
		t.Fatalf("serve: %v", err)
	}
	var resp protocol.Response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Status != protocol.StatusInvalidRequest {
		t.Fatalf("status = %q, want invalid_request", resp.Status)
	}
}

func TestIncompatibleProtocolVersionIsDistinguishable(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader(`{"protocol_version":9,"verb":"hello"}`)
	if err := Serve(context.Background(), NewPlanner(&fakeReader{}), in, &out); err != nil {
		t.Fatalf("serve: %v", err)
	}
	var resp protocol.Response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Status != protocol.StatusProtocolIncompatible {
		t.Fatalf("status = %q, want protocol_incompatible", resp.Status)
	}
	if len(resp.SupportedVersions) == 0 {
		t.Fatal("mismatch response omitted the supported version set")
	}
}

func TestServeWritesExactlyOneMessageAndIgnoresTrailingInput(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader(`{"protocol_version":1,"verb":"hello"}{"protocol_version":1,"verb":"hello"}`)
	if err := Serve(context.Background(), NewPlanner(&fakeReader{}), in, &out); err != nil {
		t.Fatalf("serve: %v", err)
	}
	if bytes.Count(out.Bytes(), []byte("\"verb\"")) != 1 {
		t.Fatalf("wrote more than one message: %s", out.String())
	}
	var resp protocol.Response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Status != protocol.StatusOK || resp.Verb != protocol.VerbHello {
		t.Fatalf("first message was not served independently of trailing input: %+v", resp)
	}
}
