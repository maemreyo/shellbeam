package bridge

import (
	"context"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/capability"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

type failureClient struct{ response Response }

func (f failureClient) Forward(context.Context, Request) (Response, error) { return f.response, nil }

func TestHandlerSanitizesLegacyDaemonFailure(t *testing.T) {
	h := New(failureClient{response: Response{
		Code:      "/Users/test/private.pem token=secret",
		Message:   "open /Users/test/private.pem token=secret",
		Retryable: true,
	}})
	got, err := h.Handle(context.Background(), Request{Action: "poll"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Code != "internal" || got.Message != "internal error" || got.Retryable {
		t.Fatalf("sanitized response=%#v", got)
	}
}

func TestHandlerNormalizesKnownLegacyCode(t *testing.T) {
	h := New(failureClient{response: Response{
		Code:      "operation_conflict",
		Message:   "raw implementation message",
		Retryable: true,
	}})
	got, err := h.Handle(context.Background(), Request{Action: "start"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Code != "operation_conflict" || got.Message != "operation conflicts with an existing intent" || got.Retryable {
		t.Fatalf("normalized response=%#v", got)
	}
}

func TestMCPV2BridgePreservesStructuredDaemonResponse(t *testing.T) {
	result := receipt.Result{SchemaVersion: 2, Operation: receipt.OperationResult{OperationID: "op", SessionID: "s", State: receipt.OperationRunning}}
	catalog := capability.Baseline(capability.Limits{CommandBytes: 123})
	client := &recordingClient{response: Response{Result: &result, Server: &catalog}}
	h := New(client)
	got, err := h.Handle(context.Background(), Request{ProtocolVersion: 2, Action: "inspect.server"})
	if err != nil {
		t.Fatal(err)
	}
	if client.request.ProtocolVersion != 2 || got.Result != &result || got.Server != &catalog {
		t.Fatalf("request=%#v response=%#v", client.request, got)
	}
}

type recordingClient struct {
	request  Request
	response Response
}

func (c *recordingClient) Forward(_ context.Context, request Request) (Response, error) {
	c.request = request
	return c.response, nil
}
