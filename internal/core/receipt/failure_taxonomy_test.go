package receipt

import (
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/session"
)

func exited(code int) ExitEvidence { return ExitEvidence{Reaped: true, Code: &code} }

func failed(exit ExitEvidence) Receipt {
	return Receipt{
		State: session.Failed, Outcome: session.Failure,
		Spawn: SpawnEvidence{Attempted: true, Succeeded: true}, Exit: exit,
	}
}

func classify(t *testing.T, r Receipt) Failure {
	t.Helper()
	failure := r.Failure()
	if failure == nil {
		t.Fatalf("receipt %#v produced no interpretation", r)
	}
	return *failure
}

// TestSuccessIsNotAFailure keeps interpretation out of the way of the 2,861
// receipts that worked.
func TestSuccessIsNotAFailure(t *testing.T) {
	completed := Receipt{State: session.Completed, Outcome: session.Success, Exit: exited(0)}
	if failure := completed.Failure(); failure != nil {
		t.Fatalf("a completed receipt was interpreted as %#v", failure)
	}
}

// TestShellConventionsAreDecodedForTheCaller.
//
// Sixty receipts in the corpus exited 127 -- an agent writing bash against a
// fish shell -- and each reported only that a command failed. Deciding what 127
// means is exactly the work a caller should not have to repeat.
func TestShellConventionsAreDecodedForTheCaller(t *testing.T) {
	notFound := classify(t, failed(exited(127)))
	if notFound.Code != "command_not_found" || notFound.Class != ClassNotFound {
		t.Fatalf("exit 127 = %#v, want a not_found command", notFound)
	}
	if notFound.Retryable {
		t.Fatal("a command that does not exist was reported as worth retrying")
	}
	if notFound.Details["exit_code"] != "127" {
		t.Fatalf("interpretation dropped the evidence it came from: %#v", notFound.Details)
	}

	notExecutable := classify(t, failed(exited(126)))
	if notExecutable.Code != "not_executable" || notExecutable.Class != ClassPermission {
		t.Fatalf("exit 126 = %#v, want a permission failure", notExecutable)
	}

	signalled := classify(t, failed(exited(141)))
	if signalled.Code != "signalled" || signalled.Details["signal_number"] != "13" {
		t.Fatalf("exit 141 = %#v, want a signalled child carrying SIGPIPE", signalled)
	}
}

// TestAnOrdinaryCommandFailureIsTheCommandsOwnAnswer. Five hundred and seventy
// receipts exited 1: a test that failed, a lint that complained. That is the
// command reporting, not ShellBeam failing, and it must not read as a fault to
// retry.
func TestAnOrdinaryCommandFailureIsTheCommandsOwnAnswer(t *testing.T) {
	failure := classify(t, failed(exited(1)))
	if failure.Class != ClassCommandFailed || failure.Code != "command_failed" {
		t.Fatalf("exit 1 = %#v", failure)
	}
	if failure.Retryable {
		t.Fatal("a command that reported failure was described as retryable")
	}
	if failure.Stage != StageExecution {
		t.Fatalf("stage = %q, want the failure attributed to the command", failure.Stage)
	}
}

// TestSpawnFailuresSayWhichThingWasMissing. Every spawn failure in the corpus
// was a working directory that did not exist, and all three reported the same
// generic code -- so the agent went looking with pwd and ls.
func TestSpawnFailuresSayWhichThingWasMissing(t *testing.T) {
	cwd := Receipt{
		State: session.Failed, Outcome: session.Failure, CWD: "/does/not/exist",
		Spawn: SpawnEvidence{Attempted: true, ErrorCode: "cwd_not_found"},
	}
	failure := classify(t, cwd)
	if failure.Stage != StageSpawn || failure.Class != ClassNotFound || failure.Code != "cwd_not_found" {
		t.Fatalf("missing cwd = %#v", failure)
	}
	if failure.Details["field"] != "cwd" || failure.Details["cwd"] != "/does/not/exist" {
		t.Fatalf("interpretation did not name the field to fix: %#v", failure.Details)
	}

	executable := Receipt{
		State: session.Failed, Outcome: session.Failure, Executable: "/usr/bin/nope",
		Spawn: SpawnEvidence{Attempted: true, ErrorCode: "executable_not_found"},
	}
	if got := classify(t, executable); got.Details["field"] != "executable" {
		t.Fatalf("missing executable = %#v", got)
	}

	exhausted := Receipt{
		State: session.Failed, Outcome: session.Failure,
		Spawn: SpawnEvidence{Attempted: true, ErrorCode: "resource_exhausted"},
	}
	if got := classify(t, exhausted); got.Class != ClassResource || !got.Retryable {
		t.Fatalf("a host out of resources = %#v, want a retryable resource failure", got)
	}
}

// TestEndingsTheDaemonDecidedAreDistinguishedFromTheChilds. A child terminated
// on a deadline and a child someone killed both look like "terminated" in the
// exit evidence alone.
func TestEndingsTheDaemonDecidedAreDistinguishedFromTheChilds(t *testing.T) {
	timedOut := Receipt{
		State: session.TimedOut, Outcome: session.Timeout, TimeoutMS: 600000, TimeoutSource: "default",
		Spawn: SpawnEvidence{Attempted: true, Succeeded: true},
		Exit:  ExitEvidence{Reaped: true, Signal: "terminated"},
	}
	failure := classify(t, timedOut)
	if failure.Class != ClassTimeout || failure.Code != "timed_out" || !failure.Retryable {
		t.Fatalf("timeout = %#v", failure)
	}
	// The bound and who chose it are what a caller needs to decide what to do.
	if failure.Details["timeout_ms"] != "600000" || failure.Details["timeout_source"] != "default" {
		t.Fatalf("timeout interpretation lost its bound: %#v", failure.Details)
	}

	killed := Receipt{
		State: session.Killed, Outcome: session.KilledOutcome,
		Spawn: SpawnEvidence{Attempted: true, Succeeded: true}, Signal: SignalEvidence{Requested: "KILL"},
		Exit: ExitEvidence{Reaped: true, Signal: "killed"},
	}
	if got := classify(t, killed); got.Class != ClassInterrupted || got.Code != "killed" {
		t.Fatalf("killed = %#v", got)
	}

	abandoned := Receipt{
		State: session.Abandoned, Outcome: session.Ambiguous, FailureReason: "daemon_restarted",
	}
	got := classify(t, abandoned)
	if got.Stage != StageFinalization || !got.Retryable {
		t.Fatalf("abandoned = %#v, want a finalization failure the caller may retry", got)
	}
}

// TestDaemonSideFailuresAreNotBlamedOnTheCommand.
func TestDaemonSideFailuresAreNotBlamedOnTheCommand(t *testing.T) {
	for reason, code := range map[string]string{
		"input_delivery_failed": "input_delivery_failed",
		"output_capture_failed": "output_capture_failed",
	} {
		r := Receipt{
			State: session.Killed, Outcome: session.KilledOutcome, FailureReason: reason,
			Spawn: SpawnEvidence{Attempted: true, Succeeded: true}, Exit: exited(0),
		}
		got := classify(t, r)
		if got.Class != ClassInternal || got.Code != code {
			t.Fatalf("%s = %#v, want an internal failure", reason, got)
		}
	}
}

// TestInterpretationIsBounded so a receipt cannot grow without limit and
// nothing long rides along inside it.
func TestInterpretationIsBounded(t *testing.T) {
	long := ""
	for len(long) < 1000 {
		long += "/very-long-path-segment"
	}
	r := Receipt{
		State: session.Failed, Outcome: session.Failure, CWD: long,
		Spawn: SpawnEvidence{Attempted: true, ErrorCode: "cwd_not_found"},
	}
	failure := classify(t, r)
	if len(failure.Details) > maxFailureDetails {
		t.Fatalf("interpretation carried %d details", len(failure.Details))
	}
	for key, value := range failure.Details {
		if len(value) > maxFailureDetailValue {
			t.Fatalf("detail %q is %d bytes", key, len(value))
		}
	}
}

func TestHardResourceLimitReasonsOverrideLiteralExitInterpretationWithoutRewritingEvidence(t *testing.T) {
	for reason, kind := range map[string]string{
		"resource_limit_memory":    "memory",
		"resource_limit_processes": "processes",
	} {
		zero := 0
		r := Receipt{
			State: session.Failed, Outcome: session.Failure, FailureReason: reason,
			Spawn: SpawnEvidence{Attempted: true, Succeeded: true},
			Exit:  ExitEvidence{Reaped: true, Code: &zero},
		}
		got := classify(t, r)
		if got.Stage != StageExecution || got.Class != ClassResource || got.Code != "resource_limit" || got.Retryable {
			t.Fatalf("%s = %#v", reason, got)
		}
		if got.Details["resource_limit_kind"] != kind {
			t.Fatalf("%s details=%#v", reason, got.Details)
		}
		if r.Exit.Code == nil || *r.Exit.Code != 0 || r.Exit.Signal != "" {
			t.Fatalf("classification rewrote literal evidence: %#v", r.Exit)
		}
	}
}
