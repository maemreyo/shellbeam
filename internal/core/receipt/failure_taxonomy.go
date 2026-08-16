package receipt

import (
	"strconv"

	"github.com/maemreyo/shellbeam/internal/core/session"
)

// Why a receipt says so little about why it failed.
//
// Of the non-success receipts in the corpus that prompted this, 667 out of 671
// carried no reason at all -- just a state and an exit code. An agent reading
// one had to recover the meaning itself: that 127 is a command the shell could
// not find, that 126 is a file it could not execute, that a terminated signal
// after a deadline is a timeout rather than someone killing it. Sixty of those
// were exit 127, the shape an agent produces when it writes bash and the
// configured shell is fish, and each one told it only "1 command failed".
//
// So classification is derived here rather than stored. The evidence a receipt
// already carries -- how it ended, whether it spawned, what it exited with --
// is enough to answer the question, and deriving it means every receipt already
// on disk gains the answer instead of only the ones written from now on. It
// also keeps the durable record what it was: raw evidence, not an
// interpretation that would freeze whatever this file understood on the day it
// was written.

// FailureStage says how far the operation got.
type FailureStage string

const (
	// StageSpawn: the child never started.
	StageSpawn FailureStage = "spawn"
	// StageExecution: the child ran and the failure is its own.
	StageExecution FailureStage = "execution"
	// StageFinalization: the child ran, but the daemon could not carry the
	// session through to a complete result.
	StageFinalization FailureStage = "finalization"
)

// FailureClass groups failures by what a caller would do about them.
type FailureClass string

const (
	// ClassNotFound: something the request named does not exist.
	ClassNotFound FailureClass = "not_found"
	// ClassPermission: it exists and was refused.
	ClassPermission FailureClass = "permission"
	// ClassInvalidRequest: the request could not be executed as written.
	ClassInvalidRequest FailureClass = "invalid_request"
	// ClassResource: the host ran out of something.
	ClassResource FailureClass = "resource"
	// ClassTimeout: the work outlived its bound.
	ClassTimeout FailureClass = "timeout"
	// ClassInterrupted: something stopped the work deliberately.
	ClassInterrupted FailureClass = "interrupted"
	// ClassCommandFailed: the command ran and reported failure, which is the
	// command's own answer rather than a fault in running it.
	ClassCommandFailed FailureClass = "command_failed"
	// ClassInternal: ShellBeam could not complete the session.
	ClassInternal FailureClass = "internal"
)

// Failure is the interpreted form of a terminal receipt.
type Failure struct {
	Stage FailureStage `json:"stage"`
	Class FailureClass `json:"class"`
	Code  string       `json:"code"`
	// Retryable says whether repeating this same request could plausibly do
	// something different. A command that does not exist will not appear
	// because it was asked for twice; a host that was out of memory might.
	Retryable bool              `json:"retryable"`
	Details   map[string]string `json:"details,omitempty"`
}

// Conventional exit statuses a POSIX shell reports.
const (
	exitCommandNotFound = 127
	exitNotExecutable   = 126
)

// maxFailureDetails bounds what interpretation may carry, so a receipt cannot
// grow without limit and nothing large or sensitive rides along in it.
const (
	maxFailureDetails     = 6
	maxFailureDetailValue = 200
)

// Failure interprets the receipt's evidence. It returns nil for a receipt that
// did not fail.
func (r Receipt) Failure() *Failure {
	if r.State == session.Completed || r.Outcome == session.Success {
		return nil
	}
	if failure := r.spawnFailure(); failure != nil {
		return failure
	}
	if failure := r.lifecycleFailure(); failure != nil {
		return failure
	}
	return r.exitFailure()
}

// spawnFailure covers a child that never started. The reason is whatever the
// spawner could determine, which is more than "it did not work": a directory
// that is not there and an executable that is not there send a caller to
// different places.
func (r Receipt) spawnFailure() *Failure {
	if !r.Spawn.Attempted || r.Spawn.Succeeded {
		return nil
	}
	failure := &Failure{Stage: StageSpawn, Code: r.Spawn.ErrorCode}
	if failure.Code == "" {
		failure.Code = "spawn_failed"
	}
	switch failure.Code {
	case "cwd_not_found":
		failure.Class, failure.Details = ClassNotFound, details("field", "cwd", "cwd", r.CWD)
	case "executable_not_found", "command_not_found":
		failure.Class, failure.Details = ClassNotFound, details("field", "executable", "executable", r.Executable)
	case "permission_denied":
		failure.Class, failure.Details = ClassPermission, details("executable", r.Executable)
	case "resource_exhausted", "stdin_pipe_failed", "output_pipe_failed":
		failure.Class, failure.Retryable = ClassResource, true
	case "invalid_execution_spec", "tty_unsupported":
		failure.Class = ClassInvalidRequest
	default:
		failure.Class = ClassInternal
	}
	return failure
}

// lifecycleFailure covers the endings the daemon decided rather than the child.
func (r Receipt) lifecycleFailure() *Failure {
	switch r.State {
	case session.TimedOut:
		return &Failure{
			Stage: StageExecution, Class: ClassTimeout, Code: "timed_out", Retryable: true,
			Details: details("timeout_ms", strconv.FormatInt(r.TimeoutMS, 10), "timeout_source", r.TimeoutSource),
		}
	case session.Abandoned:
		// The daemon that was running this is gone; the work may or may not
		// have happened, and only the caller knows if repeating it is safe.
		return &Failure{Stage: StageFinalization, Class: ClassInternal, Code: "daemon_restarted", Retryable: true}
	}
	switch r.FailureReason {
	case "input_delivery_failed":
		return &Failure{Stage: StageExecution, Class: ClassInternal, Code: "input_delivery_failed"}
	case "output_capture_failed":
		return &Failure{Stage: StageExecution, Class: ClassInternal, Code: "output_capture_failed"}
	}
	if r.State == session.Killed {
		return &Failure{
			Stage: StageExecution, Class: ClassInterrupted, Code: "killed",
			Details: details("signal", r.Signal.Requested),
		}
	}
	return nil
}

// exitFailure interprets how the child itself ended.
func (r Receipt) exitFailure() *Failure {
	if r.Exit.Signal != "" {
		return &Failure{
			Stage: StageExecution, Class: ClassInterrupted, Code: "signalled",
			Details: details("signal", r.Exit.Signal),
		}
	}
	if r.Exit.Code == nil {
		return &Failure{Stage: StageFinalization, Class: ClassInternal, Code: "exit_unobserved"}
	}
	code := *r.Exit.Code
	exit := details("exit_code", strconv.Itoa(code))
	switch code {
	case exitCommandNotFound:
		// The single most common recoverable failure in the corpus, and the one
		// an agent most often causes by writing for a different shell than the
		// one configured. Naming it is what lets the agent fix the command
		// rather than retry it unchanged.
		return &Failure{Stage: StageExecution, Class: ClassNotFound, Code: "command_not_found", Details: exit}
	case exitNotExecutable:
		return &Failure{Stage: StageExecution, Class: ClassPermission, Code: "not_executable", Details: exit}
	}
	if code > 128 && code < 192 {
		// The shell's way of reporting that the child was signalled.
		exit["signal_number"] = strconv.Itoa(code - 128)
		return &Failure{Stage: StageExecution, Class: ClassInterrupted, Code: "signalled", Details: exit}
	}
	// The command ran and said no. That is its answer, not a fault in running
	// it, and it is not something repeating the request would change.
	return &Failure{Stage: StageExecution, Class: ClassCommandFailed, Code: "command_failed", Details: exit}
}

// details builds a bounded key/value map from alternating pairs, dropping empty
// values so a caller never has to distinguish "absent" from "blank".
func details(pairs ...string) map[string]string {
	out := map[string]string{}
	for i := 0; i+1 < len(pairs); i += 2 {
		key, value := pairs[i], pairs[i+1]
		if key == "" || value == "" || len(out) >= maxFailureDetails {
			continue
		}
		if len(value) > maxFailureDetailValue {
			value = value[:maxFailureDetailValue]
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// failureOf interprets a receipt that may not be there, so callers assembling a
// result do not each have to check.
func (r *Receipt) failureOf() *Failure {
	if r == nil {
		return nil
	}
	return r.Failure()
}
