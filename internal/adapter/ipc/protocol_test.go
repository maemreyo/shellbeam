package ipc

import (
	"strings"
	"testing"
)

func TestDecodeRejectsUnknownAndTrailing(t *testing.T) {
	for _, raw := range []string{`{"ipc_version":2,"request_id":"x","payload":{"action":"poll","session_id":"s"}}`, `{"ipc_version":1,"request_id":"x","payload":{"action":"poll","session_id":"s"},"extra":1}`, `{"ipc_version":1,"request_id":"x","payload":{"action":"poll","session_id":"s"}} {}`} {
		if _, err := decodeRequest(strings.NewReader(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
