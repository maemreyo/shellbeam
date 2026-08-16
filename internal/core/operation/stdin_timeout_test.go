package operation

import "testing"

var testLimits = PolicyLimits{DefaultTimeoutMS: 600000, MaxTimeoutMS: 86400000}

func resolveOK(t *testing.T, p ExecutionPolicy) ResolvedExecutionPolicy {
	t.Helper()
	resolved, err := p.Resolve(testLimits)
	if err != nil {
		t.Fatalf("resolve %#v: %v", p, err)
	}
	return resolved
}

// TestOmissionNeverAsksForUnboundedWork is the property this file exists for.
// An ordinary caller who says nothing must not end up with a session that can
// never finish on its own.
func TestOmissionNeverAsksForUnboundedWork(t *testing.T) {
	got := resolveOK(t, ExecutionPolicy{})
	if got.StdinMode != StdinModeClosed {
		t.Fatalf("stdin for an ordinary command = %q, want %q", got.StdinMode, StdinModeClosed)
	}
	if got.Unlimited() {
		t.Fatal("an ordinary command with no timeout named was left unbounded")
	}
	if got.TimeoutMS != testLimits.DefaultTimeoutMS {
		t.Fatalf("timeout = %d, want the ordinary default %d", got.TimeoutMS, testLimits.DefaultTimeoutMS)
	}
	if !got.StdinClosedByDefault || !got.TimeoutFromDefault {
		t.Fatalf("policy did not record that it, rather than the caller, chose: %#v", got)
	}
}

func TestExplicitFiniteTimeoutIsHonouredExactly(t *testing.T) {
	got := resolveOK(t, ExecutionPolicy{TimeoutMode: TimeoutModeFinite, TimeoutMS: 20000})
	if got.TimeoutMS != 20000 {
		t.Fatalf("timeout = %d, want 20000", got.TimeoutMS)
	}
	if got.TimeoutFromDefault {
		t.Fatal("an explicitly requested bound was reported as a default")
	}
	// A bare positive timeout_ms, as callers sent before timeout_mode existed,
	// still means exactly that bound.
	if bare := resolveOK(t, ExecutionPolicy{TimeoutMS: 20000}); bare.TimeoutMS != 20000 {
		t.Fatalf("bare timeout_ms = %d, want 20000", bare.TimeoutMS)
	}
}

func TestUnlimitedRequiresDeclaredLongRunningWork(t *testing.T) {
	if _, err := (ExecutionPolicy{TimeoutMode: TimeoutModeUnlimited}).Resolve(testLimits); err == nil {
		t.Fatal("an ordinary command was allowed to ask for an unbounded timeout")
	}
	for _, p := range []ExecutionPolicy{
		{TimeoutMode: TimeoutModeUnlimited, Persistent: true},
		{TimeoutMode: TimeoutModeUnlimited, LongRunning: true},
	} {
		got := resolveOK(t, p)
		if !got.Unlimited() {
			t.Fatalf("declared long-running work was bounded: %#v", got)
		}
	}
}

// TestLongRunningWorkIsNotCutOffByTheOrdinaryDefault keeps the default from
// killing the sessions it was never aimed at.
func TestLongRunningWorkIsNotCutOffByTheOrdinaryDefault(t *testing.T) {
	for name, p := range map[string]ExecutionPolicy{
		"persistent":   {Persistent: true},
		"long_running": {LongRunning: true},
	} {
		if got := resolveOK(t, p); !got.Unlimited() {
			t.Fatalf("%s work received the ordinary bound %d", name, got.TimeoutMS)
		}
	}
	// But an explicit bound still applies to it.
	got := resolveOK(t, ExecutionPolicy{Persistent: true, TimeoutMode: TimeoutModeFinite, TimeoutMS: 5000})
	if got.TimeoutMS != 5000 {
		t.Fatalf("explicit bound on persistent work = %d, want 5000", got.TimeoutMS)
	}
}

func TestInteractiveAndPersistentWorkKeepsWritableStdin(t *testing.T) {
	for name, p := range map[string]ExecutionPolicy{
		"tty":        {TTY: true},
		"persistent": {Persistent: true},
		"legacy":     {Legacy: true},
	} {
		got := resolveOK(t, p)
		if got.StdinMode != StdinModeStream {
			t.Fatalf("%s stdin = %q, want %q", name, got.StdinMode, StdinModeStream)
		}
		if got.StdinClosedByDefault {
			t.Fatalf("%s reported a default close it did not perform", name)
		}
	}
}

// TestLegacyCallersKeepTheSemanticsTheyWereWrittenAgainst -- they had no way to
// name either setting, so retiming and cutting them off would be a change they
// could not have anticipated.
func TestLegacyCallersKeepTheSemanticsTheyWereWrittenAgainst(t *testing.T) {
	got := resolveOK(t, ExecutionPolicy{Legacy: true})
	if got.StdinMode != StdinModeStream {
		t.Fatalf("legacy stdin = %q, want %q", got.StdinMode, StdinModeStream)
	}
	if !got.Unlimited() {
		t.Fatalf("legacy timeout = %d, want unbounded", got.TimeoutMS)
	}
}

func TestExplicitChoicesOverrideTheDefaults(t *testing.T) {
	streaming := resolveOK(t, ExecutionPolicy{StdinMode: StdinModeStream})
	if streaming.StdinMode != StdinModeStream || streaming.StdinClosedByDefault {
		t.Fatalf("explicit stream = %#v", streaming)
	}
	closed := resolveOK(t, ExecutionPolicy{StdinMode: StdinModeClosed, Persistent: true})
	if closed.StdinMode != StdinModeClosed || closed.StdinClosedByDefault {
		t.Fatalf("explicit close on a persistent session = %#v", closed)
	}
}

// TestClosingStdinOnATerminalIsRefused rather than silently reinterpreted: a
// pty's input and output are one device, so there is nothing to close that
// would read as EOF to the child.
func TestClosingStdinOnATerminalIsRefused(t *testing.T) {
	if _, err := (ExecutionPolicy{StdinMode: StdinModeClosed, TTY: true}).Resolve(testLimits); err == nil {
		t.Fatal("stdin_mode closed was accepted for a tty session")
	}
}

func TestMalformedPolicyIsRejected(t *testing.T) {
	for name, p := range map[string]ExecutionPolicy{
		"unknown stdin mode":     {StdinMode: StdinMode("half-open")},
		"unknown timeout mode":   {TimeoutMode: TimeoutMode("eventually")},
		"finite without a bound": {TimeoutMode: TimeoutModeFinite},
		"finite with zero":       {TimeoutMode: TimeoutModeFinite, TimeoutMS: 0},
		"unlimited with a bound": {TimeoutMode: TimeoutModeUnlimited, Persistent: true, TimeoutMS: 1000},
		"default with a bound":   {TimeoutMode: TimeoutModeDefault, TimeoutMS: 1000},
		"negative bound":         {TimeoutMS: -1},
		"beyond the maximum":     {TimeoutMode: TimeoutModeFinite, TimeoutMS: testLimits.MaxTimeoutMS + 1},
	} {
		if _, err := p.Resolve(testLimits); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
}
