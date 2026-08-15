package failure

import "testing"

func TestPersistentSessionFailuresAreStableAndSecretSafe(t *testing.T) {
	codes := []Code{
		PersistentSessionNameConflict,
		PersistentSessionOwnershipLost,
		SupervisorUnavailable,
		SupervisorProtocolMismatch,
		SupervisorAuthFailed,
		SupervisorStateConflict,
		PersistentRecoveryOutputConflict,
		PersistentHistoryExhausted,
		PersistentInputHistoryExhausted,
		PersistentKillHistoryExhausted,
	}
	for _, code := range codes {
		got := Public(New(code, map[string]string{
			"session_id":       "session-a",
			"session_name":     "dev",
			"reason":           "test",
			"required_version": "1",
			"endpoint":         "/tmp/private.sock",
			"capability":       "SECRET",
			"pid":              "1234",
		}, nil))
		if got.Code != code {
			t.Fatalf("code=%q normalized=%q", code, got.Code)
		}
		for _, forbidden := range []string{"endpoint", "capability", "pid"} {
			if _, ok := got.Details[forbidden]; ok {
				t.Fatalf("code=%q leaked detail %q: %#v", code, forbidden, got.Details)
			}
		}
	}
}
