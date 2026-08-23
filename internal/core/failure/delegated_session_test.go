package failure

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDelegatedSessionFailureCodesAreStableAndFilterPrivateProviderDetails(t *testing.T) {
	codes := []Code{
		StaleControlGeneration,
		SessionControlNotOwned,
		DelegatedSessionUnavailable,
		DelegatedProviderLost,
		DelegatedProviderMismatch,
		DelegatedReconcileBlocked,
	}
	for _, code := range codes {
		public := Public(New(code, map[string]string{
			"session_id":       "01M0H1DELEGATEDSESSION000001",
			"provider_id":      "tmux_control_mode",
			"provider_version": "1",
			"expected_epoch":   "4",
			"current_epoch":    "3",
			"reason":           "provider_observation_stale",
			"socket_path":      "/private/tmp/shellbeam-h0/control.sock",
			"pane_token":       "%42",
		}, nil))
		if public.Code != code || public.Message == "" {
			t.Fatalf("missing delegated public failure spec for %q: %#v", code, public)
		}
		data, err := json.Marshal(public)
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		for _, forbidden := range []string{"socket_path", "shellbeam-h0", "pane_token", "%42"} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("delegated failure %q leaked %q: %s", code, forbidden, text)
			}
		}
	}
}
