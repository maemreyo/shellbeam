package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestContextExecHelperCommandsAreHiddenAndPresentationCarriesOnlyOpaqueLaunchID(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(nil, &out, &errOut); code != 2 {
		t.Fatalf("usage code=%d", code)
	}
	usage := errOut.String()
	for _, hidden := range []string{"__context_exec_helper", "__context_exec_fdexec"} {
		if strings.Contains(usage, hidden) {
			t.Fatalf("hidden command leaked: %s", usage)
		}
	}
	out.Reset()
	errOut.Reset()
	if code := run([]string{"__context_exec_helper"}, &out, &errOut); code == 0 || strings.Contains(errOut.String(), "unknown command") {
		t.Fatalf("missing helper handling code=%d err=%q", code, errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := run([]string{"__context_exec_helper", "launch_01", "/bin/echo", "secret"}, &out, &errOut); code == 0 {
		t.Fatal("helper presentation accepted child argv")
	}
}
