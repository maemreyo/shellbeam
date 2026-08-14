package ipc

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/failure"
)

func TestDecodeRejectsUnknownAndTrailing(t *testing.T) {
	for _, raw := range []string{`{"ipc_version":2,"request_id":"x","payload":{"action":"poll","session_id":"s"}}`, `{"ipc_version":1,"request_id":"x","payload":{"action":"poll","session_id":"s"},"extra":1}`, `{"ipc_version":1,"request_id":"x","payload":{"action":"poll","session_id":"s"}} {}`} {
		if _, err := decodeRequest(strings.NewReader(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestErrorEnvelopeUsesStableTypedFailure(t *testing.T) {
	cause := errors.New("private failure /Users/test/.ssh/id_rsa token=secret")
	err := failure.New(failure.OperationConflict, map[string]string{
		"operation_id": "op-123",
		"path":         "/Users/test/private",
	}, cause)
	got := errorEnvelope(err)
	if got.Code != "operation_conflict" || got.Message != "operation conflicts with an existing intent" || got.Retryable {
		t.Fatalf("error envelope=%#v", got)
	}
	if len(got.Details) != 1 || got.Details["operation_id"] != "op-123" {
		t.Fatalf("unsafe details=%#v", got.Details)
	}
}

func TestErrorEnvelopeUnknownDoesNotLeakRawError(t *testing.T) {
	got := errorEnvelope(errors.New("open /Users/test/private.pem token=super-secret"))
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if got.Code != "internal" || got.Message != "internal error" || got.Retryable {
		t.Fatalf("error envelope=%#v", got)
	}
	if strings.Contains(text, "private.pem") || strings.Contains(text, "super-secret") {
		t.Fatalf("error envelope leaked raw error: %s", text)
	}
}
