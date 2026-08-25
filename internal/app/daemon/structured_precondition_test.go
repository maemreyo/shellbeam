package daemon

import (
	"errors"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/failure"
)

func TestExplicitStructuredAdapterRejectsIncompatibleProducerBeforeExecution(t *testing.T) {
	cases := []struct {
		name string
		req  StartRequest
	}{
		{"raw shell", StartRequest{ProtocolVersion: 2, Command: "go test -json ./...", StructuredAdapter: "go-test-json"}},
		{"missing json", StartRequest{ProtocolVersion: 2, Argv: []string{"go", "test", "./..."}, StructuredAdapter: "go-test-json"}},
		{"wrong producer", StartRequest{ProtocolVersion: 2, Argv: []string{"go", "vet", "-json", "./..."}, StructuredAdapter: "go-test-json"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := normalizedStructuredAdapter(tc.req)
			if err == nil || !errors.Is(err, failure.InvalidInput) {
				t.Fatalf("err=%v", err)
			}
			public := failure.Public(err)
			if public.Code != failure.InvalidInput {
				t.Fatalf("public=%#v", public)
			}
		})
	}
}

func TestExplicitStructuredAdapterAcceptsMatchingDirectArgv(t *testing.T) {
	for _, executable := range []string{"go", "/opt/homebrew/bin/go"} {
		t.Run(executable, func(t *testing.T) {
			req := StartRequest{ProtocolVersion: 2, Argv: []string{executable, "test", "./...", "-json"}, StructuredAdapter: "go-test-json"}
			got, err := normalizedStructuredAdapter(req)
			if err != nil || got != "go-test-json" {
				t.Fatalf("adapter=%q err=%v", got, err)
			}
		})
	}
}
