package contextexec

import (
	"bytes"
	"strings"
	"testing"
)

func TestSanitizeChildEnvironmentPreservesUserContextAndStripsOnlyClosedControlKeys(t *testing.T) {
	secret := "h5-user-secret-canary"
	input := []string{
		"PATH=/usr/bin:/bin",
		"H5_SECRET=" + secret,
		"APP_CONFIG=SHELLBEAM_CONTEXT_EXEC_CLAIM-is-just-user-data",
		"SHELLBEAM_CONTEXT_EXEC_SOCKET=/private/control.sock",
		"SHELLBEAM_CONTEXT_EXEC_CLAIM=claim-material",
		"SHELLBEAM_CONTEXT_EXEC_GENERATION=generation_01",
		"SHELLBEAM_CONTEXT_EXEC_LAUNCH_ID=launch_01",
		"SHELLBEAM_TRACE_SOCKET=/private/trace.sock",
		"SHELLBEAM_TRACE_PROTOCOL=1",
		"SHELLBEAM_TRACE_ID=trace_01",
		"SHELLBEAM_PROVIDER_GENERATION=provider_01",
		"SHELLBEAM_H0_TMUX=/opt/homebrew/bin/tmux",
	}
	got := SanitizeChildEnvironment(input)
	joined := strings.Join(got, "\n")
	for _, want := range []string{"H5_SECRET=" + secret, "APP_CONFIG=SHELLBEAM_CONTEXT_EXEC_CLAIM-is-just-user-data", "SHELLBEAM_H0_TMUX=/opt/homebrew/bin/tmux", "PATH=/usr/bin:/bin"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("lost user context %q: %s", want, joined)
		}
	}
	for _, forbidden := range []string{"SHELLBEAM_CONTEXT_EXEC_SOCKET=", "SHELLBEAM_CONTEXT_EXEC_CLAIM=claim", "SHELLBEAM_CONTEXT_EXEC_GENERATION=", "SHELLBEAM_CONTEXT_EXEC_LAUNCH_ID=", "SHELLBEAM_TRACE_SOCKET=", "SHELLBEAM_TRACE_PROTOCOL=", "SHELLBEAM_TRACE_ID=", "SHELLBEAM_PROVIDER_GENERATION="} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("control env survived: %q in %s", forbidden, joined)
		}
	}
}

func TestCaptureOutputKeepsStreamsSeparateAndBoundsCanonicalBytes(t *testing.T) {
	var frames []OutputFrame
	got, err := CaptureOutput(bytes.NewBufferString("stdout-owned"), bytes.NewBufferString("stderr-owned"), 8, func(frame OutputFrame) error {
		frames = append(frames, frame)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Truncated || got.Complete || got.StdoutBytes+got.StderrBytes > 8 {
		t.Fatalf("capture=%#v", got)
	}
	for _, frame := range frames {
		if frame.Stream != StreamStdout && frame.Stream != StreamStderr {
			t.Fatalf("mixed frame=%#v", frame)
		}
		if bytes.Contains(frame.Data, []byte("pane-noise")) {
			t.Fatalf("pane bytes attributed: %#v", frame)
		}
	}
}
