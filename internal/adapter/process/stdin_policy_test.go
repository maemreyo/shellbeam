//go:build linux || darwin

package process

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

// openFDs counts this process's open pipe descriptors.
func openFDs(t *testing.T) int {
	t.Helper()
	out, err := exec.Command("lsof", "-p", strconv.Itoa(os.Getpid())).Output()
	if err != nil {
		t.Skipf("lsof unavailable: %v", err)
	}
	return strings.Count(string(out), "PIPE")
}

type discardSink struct{}

func (discardSink) Append(context.Context, []byte) error { return nil }
func (discardSink) CaptureFailed(error)                  {}

func spawn(t *testing.T, spec operation.ExecutionSpec) *Handle {
	t.Helper()
	spec.Shell = "/bin/sh"
	spec.Mode = operation.ExecutionModeShell
	if spec.CWD == "" {
		spec.CWD = t.TempDir()
	}
	handle, evidence, err := Owner{}.Start(context.Background(), spec, discardSink{})
	if err != nil || !evidence.Succeeded {
		t.Fatalf("spawn %q: %v (%#v)", spec.Command, err, evidence)
	}
	return handle.(*Handle)
}

func waitWithin(t *testing.T, h *Handle, d time.Duration) receipt.ExitEvidence {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	exit := h.Wait(ctx)
	if !exit.Reaped {
		h.Signal("KILL")
		t.Fatalf("child did not exit within %s", d)
	}
	return exit
}

// TestStdinReadersFinishWhenInputIsClosed is the failure this policy exists to
// end.
//
// Both commands are real ones an agent ran: writing a file with `cat >` and
// piping a script into `python3 -`. With standard input left open and no
// timeout, each blocked forever on input that was never coming, holding a
// session slot until the daemon restarted. Three such sessions sat live for
// days in the corpus that prompted this work.
func TestStdinReadersFinishWhenInputIsClosed(t *testing.T) {
	target := filepath.Join(t.TempDir(), "written")
	for name, command := range map[string]string{
		"cat > file":  "cat > " + target,
		"python3 -":   "python3 -",
		"read a line": "read line",
	} {
		t.Run(name, func(t *testing.T) {
			h := spawn(t, operation.ExecutionSpec{Command: command, StdinMode: operation.StdinModeClosed})
			exit := waitWithin(t, h, 10*time.Second)
			if exit.Code == nil || *exit.Code != 0 {
				t.Logf("exit evidence: %#v", exit)
			}
		})
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("cat did not finish writing its file: %v", err)
	}
}

// TestStdinReadersHangWhenInputStaysOpen is the control. Without it, the test
// above could pass because these commands exit on their own.
func TestStdinReadersHangWhenInputStaysOpen(t *testing.T) {
	h := spawn(t, operation.ExecutionSpec{
		Command: "cat > " + filepath.Join(t.TempDir(), "written"), StdinMode: operation.StdinModeStream,
	})
	defer h.Signal("KILL")
	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()
	if exit := h.Wait(ctx); exit.Reaped {
		t.Fatalf("child exited with stdin held open: %#v", exit)
	}
}

// TestStreamingStdinStillDeliversInputAndEnds keeps the explicit mode whole:
// start, write, end of input, exit.
func TestStreamingStdinStillDeliversInputAndEnds(t *testing.T) {
	target := filepath.Join(t.TempDir(), "written")
	h := spawn(t, operation.ExecutionSpec{Command: "cat > " + target, StdinMode: operation.StdinModeStream})
	if err := h.Write([]byte("streamed\n")); err != nil {
		t.Fatal(err)
	}
	if err := h.CloseStdin(); err != nil {
		t.Fatal(err)
	}
	waitWithin(t, h, 10*time.Second)
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "streamed\n" {
		t.Fatalf("child received %q", content)
	}
}

// TestWritingToClosedStdinIsRefusedRatherThanLost tells a caller that its input
// went nowhere, instead of accepting it silently.
func TestWritingToClosedStdinIsRefusedRatherThanLost(t *testing.T) {
	h := spawn(t, operation.ExecutionSpec{Command: "cat", StdinMode: operation.StdinModeClosed})
	defer h.Signal("KILL")
	if err := h.Write([]byte("too late\n")); !errors.Is(err, ErrStdinClosed) {
		t.Fatalf("write to closed stdin = %v, want %v", err, ErrStdinClosed)
	}
}

// TestEndingInputIsIdempotent covers the two paths that close the write end
// meeting on the same session: policy at spawn, and an explicit end of input.
func TestEndingInputIsIdempotent(t *testing.T) {
	h := spawn(t, operation.ExecutionSpec{Command: "cat", StdinMode: operation.StdinModeClosed})
	defer h.Signal("KILL")
	for i := 0; i < 3; i++ {
		if err := h.CloseStdin(); err != nil {
			t.Fatalf("close %d: %v", i, err)
		}
	}
	if err := h.Close(); err != nil {
		t.Fatalf("handle close: %v", err)
	}
}

// TestClosedStdinDoesNotRetainThePipe: the write end is the descriptor a
// finished session used to keep, so closing it at spawn means an ordinary
// command never holds one.
func TestClosedStdinDoesNotRetainThePipe(t *testing.T) {
	before := openFDs(t)
	const runs = 20
	for i := 0; i < runs; i++ {
		h := spawn(t, operation.ExecutionSpec{Command: "true", StdinMode: operation.StdinModeClosed})
		waitWithin(t, h, 10*time.Second)
	}
	after := openFDs(t)
	t.Logf("pipe fds: before=%d after_%d_closed_stdin_sessions=%d", before, runs, after)
	if after-before >= runs {
		t.Fatalf("%d sessions retained %d pipe descriptors", runs, after-before)
	}
}
