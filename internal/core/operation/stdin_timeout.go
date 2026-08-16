package operation

import "fmt"

// Standard input and timeout policy.
//
// Both settings used to be expressed by a single value whose omission and whose
// zero were the same thing, and both defaulted to the unbounded choice: stdin
// stayed open, and timeout_ms == 0 meant run forever. A caller that said
// nothing therefore asked for a session that could never end on its own.
//
// That is how ShellBeam lost capacity. An agent writing a file with `cat >
// path` -- or running `python3 -` -- gets a child blocked on a terminal that
// never arrives, with no timeout, holding one of four session slots until the
// daemon restarts. Three such sessions were live for days in the corpus that
// prompted this change, leaving the daemon at one usable slot out of four while
// reporting capacity_exceeded as if it were busy.
//
// So omission is no longer a way to ask for unbounded work. Each setting has
// three states, and the unbounded one has to be named.

// StdinMode says what happens to a child's standard input.
type StdinMode string

const (
	// StdinModeUnset is the absence of a choice, resolved by policy.
	StdinModeUnset StdinMode = ""
	// StdinModeClosed delivers EOF as soon as the child is spawned.
	StdinModeClosed StdinMode = "closed"
	// StdinModeStream keeps the write end open for later input.
	StdinModeStream StdinMode = "stream"
)

func (m StdinMode) Validate() error {
	switch m {
	case StdinModeUnset, StdinModeClosed, StdinModeStream:
		return nil
	default:
		return fmt.Errorf("invalid stdin_mode %q", string(m))
	}
}

// TimeoutMode says how long a child may run.
type TimeoutMode string

const (
	// TimeoutModeUnset is the absence of a choice, resolved by policy.
	TimeoutModeUnset TimeoutMode = ""
	// TimeoutModeDefault asks for the ordinary bound explicitly.
	TimeoutModeDefault TimeoutMode = "default"
	// TimeoutModeFinite asks for a specific bound.
	TimeoutModeFinite TimeoutMode = "finite"
	// TimeoutModeUnlimited asks for no bound at all. It is only available to
	// work that has declared itself long-running, so an ordinary command cannot
	// reach it by accident -- which is the whole point of naming it.
	TimeoutModeUnlimited TimeoutMode = "unlimited"
)

func (m TimeoutMode) Validate() error {
	switch m {
	case TimeoutModeUnset, TimeoutModeDefault, TimeoutModeFinite, TimeoutModeUnlimited:
		return nil
	default:
		return fmt.Errorf("invalid timeout_mode %q", string(m))
	}
}

// ExecutionPolicy is the caller's request before defaults are applied.
type ExecutionPolicy struct {
	StdinMode   StdinMode
	TimeoutMode TimeoutMode
	TimeoutMS   int64
	// TTY, Persistent and LongRunning describe the work, and decide which
	// defaults apply and whether an unbounded timeout is even available.
	TTY         bool
	Persistent  bool
	LongRunning bool
	// Legacy marks a request arriving over a protocol version that predates
	// these settings. Such callers never had a way to ask for stdin or a
	// timeout explicitly, so they keep the semantics they were written
	// against rather than being silently retimed and cut off.
	Legacy bool
}

// ResolvedExecutionPolicy is what the daemon actually runs.
type ResolvedExecutionPolicy struct {
	StdinMode StdinMode
	// TimeoutMS is zero when the child may run without a bound.
	TimeoutMS int64
	// StdinClosedByDefault records that nothing asked for stdin to be closed;
	// policy did. Receipts carry this so an agent reading one can tell the
	// difference between "my input was cut off" and "I never asked to send
	// any", instead of inferring that the child closed its own input.
	StdinClosedByDefault bool
	// TimeoutFromDefault records the same thing for the bound.
	TimeoutFromDefault bool
	// FromLegacy records that these values come from compatibility rules rather
	// than from anything the caller could have asked for. Without it a receipt
	// would report a legacy session's unbounded timeout as though the caller
	// had requested one.
	FromLegacy bool
}

// Unlimited reports whether the child may run without a time bound.
func (r ResolvedExecutionPolicy) Unlimited() bool { return r.TimeoutMS <= 0 }

// PolicyLimits are the daemon's configured bounds.
type PolicyLimits struct {
	DefaultTimeoutMS int64
	MaxTimeoutMS     int64
}

// Resolve applies the defaults, rejecting requests that name a state they are
// not entitled to.
func (p ExecutionPolicy) Resolve(limits PolicyLimits) (ResolvedExecutionPolicy, error) {
	if err := p.StdinMode.Validate(); err != nil {
		return ResolvedExecutionPolicy{}, err
	}
	if err := p.TimeoutMode.Validate(); err != nil {
		return ResolvedExecutionPolicy{}, err
	}
	resolved := ResolvedExecutionPolicy{}

	stdin, byDefault, err := p.resolveStdin()
	if err != nil {
		return ResolvedExecutionPolicy{}, err
	}
	resolved.StdinMode, resolved.StdinClosedByDefault = stdin, byDefault

	timeout, fromDefault, err := p.resolveTimeout(limits)
	if err != nil {
		return ResolvedExecutionPolicy{}, err
	}
	resolved.TimeoutMS, resolved.TimeoutFromDefault = timeout, fromDefault
	resolved.FromLegacy = p.Legacy
	return resolved, nil
}

func (p ExecutionPolicy) resolveStdin() (StdinMode, bool, error) {
	if p.StdinMode != StdinModeUnset {
		// A pseudo-terminal has no standard input that can be closed on its
		// own: the child's input and output are the same device, and closing it
		// tears the terminal down rather than delivering EOF. Refusing is
		// honest; accepting would mean quietly doing something else.
		if p.StdinMode == StdinModeClosed && p.TTY {
			return "", false, fmt.Errorf("stdin_mode closed is not available with tty")
		}
		return p.StdinMode, false, nil
	}
	// A terminal implies an interactive child, and a persistent session exists
	// to be written to; closing input on either would contradict the request
	// that created it. Legacy callers keep the open stdin they were built
	// against. Everything else is an ordinary command, and an ordinary command
	// that is not given input should not wait for it.
	if p.TTY || p.Persistent || p.Legacy {
		return StdinModeStream, false, nil
	}
	return StdinModeClosed, true, nil
}

func (p ExecutionPolicy) resolveTimeout(limits PolicyLimits) (int64, bool, error) {
	switch p.TimeoutMode {
	case TimeoutModeFinite:
		if p.TimeoutMS <= 0 {
			return 0, false, fmt.Errorf("timeout_mode finite requires a positive timeout_ms")
		}
	case TimeoutModeUnlimited:
		if p.TimeoutMS != 0 {
			return 0, false, fmt.Errorf("timeout_mode unlimited must not carry a timeout_ms")
		}
		if !p.Persistent && !p.LongRunning {
			return 0, false, fmt.Errorf("timeout_mode unlimited requires persistent or long_running intent")
		}
		return 0, false, nil
	case TimeoutModeDefault:
		if p.TimeoutMS != 0 {
			return 0, false, fmt.Errorf("timeout_mode default must not carry a timeout_ms")
		}
		p.TimeoutMS = 0
	}
	if p.TimeoutMS > 0 {
		if limits.MaxTimeoutMS > 0 && p.TimeoutMS > limits.MaxTimeoutMS {
			return 0, false, fmt.Errorf("timeout_ms exceeds max_timeout_ms")
		}
		return p.TimeoutMS, false, nil
	}
	if p.TimeoutMS < 0 {
		return 0, false, fmt.Errorf("timeout_ms must not be negative")
	}
	// Nothing was named. Work that declared itself long-running keeps running;
	// anything else gets the ordinary bound, because an unbounded default is
	// what let stuck sessions hold capacity forever.
	if p.Persistent || p.LongRunning || p.Legacy {
		return 0, false, nil
	}
	if limits.DefaultTimeoutMS <= 0 {
		return 0, false, nil
	}
	return limits.DefaultTimeoutMS, true, nil
}

// policyDigest is the part of a fingerprint that describes stdin and timeout.
//
// It is a pointer inside the hashed structures so that a nil digest marshals to
// nothing at all: a request that named neither setting hashes byte-for-byte as
// it did before these settings existed, which is what keeps the operations
// already on disk replayable.
type policyDigest struct {
	StdinMode         StdinMode   `json:"stdin_mode,omitempty"`
	TimeoutMode       TimeoutMode `json:"timeout_mode,omitempty"`
	ResolvedTimeoutMS int64       `json:"resolved_timeout_ms,omitempty"`
	TimeoutSource     string      `json:"timeout_source,omitempty"`
}

// RequestPolicyDigest describes what the caller actually sent.
//
// Explicit choices are never normalized into omission, even when today's
// defaults would resolve them the same way. A fingerprint answers "did the
// caller replay the same request", and folding "said nothing" together with
// "said closed" would make that answer depend on the policy version in force
// rather than on the request -- so changing a default later would silently
// change which past requests count as identical.
func RequestPolicyDigest(stdin StdinMode, timeout TimeoutMode) *policyDigest {
	if stdin == StdinModeUnset && timeout == TimeoutModeUnset {
		return nil
	}
	return &policyDigest{StdinMode: stdin, TimeoutMode: timeout}
}

// ExecutionPolicyDigest describes the contract the daemon will actually run.
func ExecutionPolicyDigest(resolved ResolvedExecutionPolicy, source string) *policyDigest {
	return &policyDigest{StdinMode: resolved.StdinMode, ResolvedTimeoutMS: resolved.TimeoutMS, TimeoutSource: source}
}
