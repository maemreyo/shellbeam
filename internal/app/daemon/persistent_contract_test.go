package daemon

import (
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/failure"
)

func TestPersistentStartMetadataRequiresModernNonTTYShape(t *testing.T) {
	cases := []struct {
		name string
		req  StartRequest
		code failure.Code
	}{
		{name: "legacy persistent", req: StartRequest{ProtocolVersion: 1, Persistent: true}, code: failure.FeatureUnavailable},
		{name: "legacy name", req: StartRequest{ProtocolVersion: 1, Persistent: true, SessionName: "dev"}, code: failure.FeatureUnavailable},
		{name: "name without persistent", req: StartRequest{ProtocolVersion: 2, SessionName: "dev"}, code: failure.InvalidInput},
		{name: "persistent tty", req: StartRequest{ProtocolVersion: 2, Persistent: true, TTY: true}, code: failure.FeatureUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateStartMetadata(tc.req)
			if got := failure.Public(err).Code; got != tc.code {
				t.Fatalf("code=%q want=%q err=%v", got, tc.code, err)
			}
		})
	}
	if err := validateStartMetadata(StartRequest{ProtocolVersion: 2, Persistent: true, SessionName: "dev"}); err != nil {
		t.Fatalf("valid persistent metadata rejected: %v", err)
	}
}
