package main

import (
	"strings"
	"testing"
)

func TestControlParserPreservesCommandAndNotificationOrder(t *testing.T) {
	input := "%begin 1 7 0\nanswer\n%end 1 7 0\n%output %3 abc\\012\n%message hello\n%client-detached client-1\n%exit done\n"
	events, err := parseControl(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 5 {
		t.Fatalf("len(events)=%d events=%#v", len(events), events)
	}
	if events[0].Kind != EventCommandEnd || events[0].CommandNumber != 7 || events[0].Data != "answer" {
		t.Fatalf("command event=%#v", events[0])
	}
	if events[1].Kind != EventPaneOutput || events[1].PaneID != "%3" || events[1].Data != "abc\n" {
		t.Fatalf("pane event=%#v", events[1])
	}
	if events[2].Kind != EventMessage || events[2].Data != "hello" {
		t.Fatalf("message event=%#v", events[2])
	}
	if events[3].Kind != EventClientDetached || events[3].Data != "client-1" {
		t.Fatalf("client-detached event=%#v", events[3])
	}
	if events[4].Kind != EventExit || events[4].Data != "done" {
		t.Fatalf("exit event=%#v", events[4])
	}
}

func TestControlParserRetainsUnknownNotification(t *testing.T) {
	input := "%future-notice a b c\n"
	events, err := parseControl(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != EventUnknownNotification || events[0].Raw != "%future-notice a b c" {
		t.Fatalf("events=%#v", events)
	}
}

func TestControlParserRejectsNestedBegin(t *testing.T) {
	input := "%begin 1 7 0\n%begin 1 8 0\n"
	if _, err := parseControl(strings.NewReader(input)); err == nil || !strings.Contains(err.Error(), "nested") {
		t.Fatalf("err=%v", err)
	}
}

func TestControlParserRejectsMismatchedCommandNumber(t *testing.T) {
	input := "%begin 1 7 0\nanswer\n%end 1 8 0\n"
	if _, err := parseControl(strings.NewReader(input)); err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("err=%v", err)
	}
}

func TestControlParserSurfacesCommandError(t *testing.T) {
	input := "%begin 1 7 0\nbad command\n%error 1 7 0\n"
	events, err := parseControl(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != EventCommandError || events[0].CommandNumber != 7 || events[0].Data != "bad command" {
		t.Fatalf("events=%#v", events)
	}
}

func TestControlParserRejectsInvalidOctalOutput(t *testing.T) {
	input := "%output %3 abc\\09z\n"
	if _, err := parseControl(strings.NewReader(input)); err == nil || !strings.Contains(err.Error(), "escape") {
		t.Fatalf("err=%v", err)
	}
}

func TestControlParserRejectsEOFMidBlock(t *testing.T) {
	input := "%begin 1 7 0\nanswer\n"
	if _, err := parseControl(strings.NewReader(input)); err == nil || !strings.Contains(err.Error(), "EOF") {
		t.Fatalf("err=%v", err)
	}
}

func TestControlParserRejectsNotificationInsideCommandBlock(t *testing.T) {
	input := "%begin 1 7 0\n%output %3 sneaky\n%end 1 7 0\n"
	if _, err := parseControl(strings.NewReader(input)); err == nil || !strings.Contains(err.Error(), "notification inside command block") {
		t.Fatalf("err=%v", err)
	}
}

func TestControlParserBoundsUnknownNotification(t *testing.T) {
	input := "%future-notice " + strings.Repeat("x", maxControlLineBytes) + "\n"
	if _, err := parseControl(strings.NewReader(input)); err == nil || !strings.Contains(err.Error(), "line too long") {
		t.Fatalf("err=%v", err)
	}
}
