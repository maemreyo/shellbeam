package observability

import (
	"bytes"
	"strings"
	"testing"
)

func TestLoggerRedactsValues(t *testing.T) {
	var b bytes.Buffer
	l := New(&b)
	l.Event("start", "session", "s", "command", "secret", "cwd", "/private")
	got := b.String()
	if strings.Contains(got, "secret") || strings.Contains(got, "/private") {
		t.Fatalf("log leaked: %s", got)
	}
	if !strings.Contains(got, "start") || !strings.Contains(got, `"session":"s"`) {
		t.Fatalf("missing safe event metadata: %s", got)
	}
}

func TestLoggerRedactsMediaPayloadAndCanonicalPathSentinels(t *testing.T) {
	var b bytes.Buffer
	l := New(&b)
	payload := "MEDIA_PAYLOAD_SENTINEL_GPS_21.0285_105.8542"
	canonical := "/private/CANONICAL_MEDIA_SENTINEL/probe.png"
	l.Event("read_media", "payload", payload, "resolved_path", canonical, "cwd", "/caller/alias")
	got := b.String()
	for _, forbidden := range []string{payload, canonical, "/caller/alias", "GPS_21.0285_105.8542"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("media value leaked into log: %q in %s", forbidden, got)
		}
	}
	if !strings.Contains(got, "read_media") {
		t.Fatalf("missing safe event name: %s", got)
	}
}
