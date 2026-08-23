package main

import "testing"

func TestSessionAttachParserAcceptsOnlyExactHandoffID(t *testing.T) {
	got, err := parseSessionAttachArgs([]string{"--handoff-id", "handoff_local_1"})
	if err != nil || got.handoffID != "handoff_local_1" {
		t.Fatalf("got=%#v err=%v", got, err)
	}
	for _, args := range [][]string{{}, {"--handoff-id", "../escape"}, {"--handoff-id", ".leading"}, {"--handoff-id", "_leading"}, {"--handoff-id", "-leading"}, {"--handoff-id", "contains.dot"}, {"--handoff-id", "h", "--session-id", "s"}, {"--socket", "/tmp/x", "--handoff-id", "h"}, {"--handoff-id", "h", "extra"}} {
		if _, err := parseSessionAttachArgs(args); err == nil {
			t.Fatalf("accepted %#v", args)
		}
	}
}
