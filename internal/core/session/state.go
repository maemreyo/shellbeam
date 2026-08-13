// Package session owns lifecycle, input, and signal invariants.
package session

type State string
type Outcome string

const (
	Starting      State   = "starting"
	Running       State   = "running"
	Finalizing    State   = "finalizing"
	Completed     State   = "completed"
	Failed        State   = "failed"
	TimedOut      State   = "timed_out"
	Killed        State   = "killed"
	Abandoned     State   = "abandoned"
	NoOutcome     Outcome = ""
	Success       Outcome = "success"
	Failure       Outcome = "failure"
	Timeout       Outcome = "timeout"
	KilledOutcome Outcome = "killed"
	Ambiguous     Outcome = "ambiguous"
)

func (s State) Terminal() bool {
	return s == Completed || s == Failed || s == TimedOut || s == Killed || s == Abandoned
}

func CanTransition(from, to State) bool {
	switch from {
	case Starting:
		return to == Running || to == Finalizing || to == Abandoned
	case Running:
		return to == Finalizing || to == Abandoned
	case Finalizing:
		return to == Completed || to == Failed || to == TimedOut || to == Killed || to == Abandoned
	default:
		return false
	}
}
