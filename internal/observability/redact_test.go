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
	if !strings.Contains(got, "start") {
		t.Fatalf("missing event: %s", got)
	}
}
