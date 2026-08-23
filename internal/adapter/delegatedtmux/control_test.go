package delegatedtmux

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestParseControlTranscriptPreservesCommandAndPaneOutputOrdering(t *testing.T) {
	transcript := strings.Join([]string{
		"%begin 1 7 0",
		"value",
		"%end 1 7 0",
		"%output %2 hello\\040world\\012",
		"%message ready",
	}, "\n") + "\n"
	events, err := parseControl(strings.NewReader(transcript))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("events=%#v", events)
	}
	if events[0].Kind != eventCommandEnd || events[0].CommandNumber != 7 || events[0].Data != "value" {
		t.Fatalf("command=%#v", events[0])
	}
	if events[1].Kind != eventPaneOutput || events[1].PaneID != "%2" || events[1].Data != "hello world\n" {
		t.Fatalf("output=%#v", events[1])
	}
	if events[2].Kind != eventMessage || events[2].Data != "ready" {
		t.Fatalf("message=%#v", events[2])
	}
}

func TestParseControlRejectsNotificationInsideCommandBlockAndBadEscape(t *testing.T) {
	for name, transcript := range map[string]string{
		"notification_inside_block": "%begin 1 1 0\n%output %1 x\n%end 1 1 0\n",
		"bad_escape":                "%output %1 bad\\x\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseControl(strings.NewReader(transcript)); err == nil {
				t.Fatalf("accepted %q", transcript)
			}
		})
	}
}

func TestTmuxQuotePreventsParserExpansionAndKeepsOneControlLine(t *testing.T) {
	got, err := quoteTmuxArg("$HOME\\path\n'\"")
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsRune(got, '\n') || !strings.Contains(got, `\$HOME`) || !strings.Contains(got, `\012`) || !strings.Contains(got, `\\`) || !strings.Contains(got, `\"`) {
		t.Fatalf("unsafe tmux quote=%q", got)
	}
	if _, err := quoteTmuxArg("bad\x00value"); err == nil {
		t.Fatal("NUL accepted")
	}
}

type memoryOutputSink struct{ data []byte }

func (s *memoryOutputSink) Append(b []byte) error { s.data = append(s.data, b...); return nil }

func TestControlClientBuffersPreTargetOutputUntilExactPaneBinding(t *testing.T) {
	sink := &memoryOutputSink{}
	c := &controlClient{}
	c.deliverOutput(controlEvent{Kind: eventPaneOutput, PaneID: "%1", Data: "old"})
	c.deliverOutput(controlEvent{Kind: eventPaneOutput, PaneID: "%2", Data: "other"})
	if len(sink.data) != 0 {
		t.Fatal("unbound output delivered")
	}
	if err := c.setTarget("%1", sink); err != nil {
		t.Fatal(err)
	}
	if string(sink.data) != "old" {
		t.Fatalf("drained=%q", sink.data)
	}
}

func TestControlClientCloseWaitsForProcessExitNotReadLoopEOF(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", `trap '' TERM; exec 1>&-; while :; do :; done`)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	c := newControlClient(cmd, stdin, stdout)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-c.done:
	case <-time.After(time.Second):
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal("control read loop did not observe stdout EOF")
	}

	closed := make(chan error, 1)
	go func() { closed <- c.close() }()
	select {
	case err := <-closed:
		if err == nil {
			return
		}
		if cmd.ProcessState == nil {
			t.Fatalf("close err=%v process_state=<nil>", err)
		}
	case <-time.After(time.Second):
		_ = cmd.Process.Kill()
		err := <-closed
		t.Fatalf("control close exceeded bounded teardown window: %v", err)
	}
}
