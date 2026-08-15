package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestPrivateSupervisorDispatchIsRecognizedButAbsentFromPublicUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr); code != 2 {
		t.Fatalf("usage code=%d", code)
	}
	if strings.Contains(stderr.String(), "__supervisor") {
		t.Fatalf("private command leaked into usage: %q", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"__supervisor", "unexpected"}, &stdout, &stderr); code != 1 {
		t.Fatalf("private dispatch code=%d stderr=%q", code, stderr.String())
	}
	if strings.Contains(stderr.String(), "unknown command") || !strings.Contains(stderr.String(), "shellbeam __supervisor:") {
		t.Fatalf("private dispatch was not recognized safely: %q", stderr.String())
	}
}
