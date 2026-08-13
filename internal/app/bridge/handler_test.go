package bridge

import (
	"context"
	"testing"
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
