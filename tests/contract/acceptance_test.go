package contract_test

import (
	"os"
	"strings"
	"testing"
)

func TestAcceptanceMatrixOwnsCriticalInvariants(t *testing.T) {
	b, err := os.ReadFile("../../docs/testing/acceptance-matrix.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for _, required := range []string{"Start fingerprint", "Reservation before spawn", "Bounded stdin retry/EOF", "Reap/drain/input terminal receipt", "Daemon restart ambiguity", "Unix socket/peer UID", "Tunnel/ChatGPT behavior"} {
		if !strings.Contains(text, required) {
			t.Errorf("missing %q", required)
		}
	}
}
