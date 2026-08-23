package daemon

import (
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

// resolveExecutionPolicy turns what a caller asked for into what will run.
//
// Resolution happens once, here, before a reservation exists -- so every later
// stage reads a decision rather than re-deriving one, and the spawner never has
// to know what a default is.
func (s *Service) resolveExecutionPolicy(req StartRequest) (operation.ResolvedExecutionPolicy, error) {
	return operation.ExecutionPolicy{
		StdinMode:   req.StdinMode,
		TimeoutMode: req.TimeoutMode,
		TimeoutMS:   req.TimeoutMS,
		TTY:         req.TTY,
		Persistent:  req.Persistent || req.SessionMode == delegated.ModeDelegatedInteractive,
		LongRunning: req.Intent != nil && req.Intent.Kind == operation.IntentKindLongRunning,
		// Callers on the protocol version that predates these settings had no
		// way to name either one, so they keep what they were written against.
		Legacy: req.ProtocolVersion < 2,
	}.Resolve(operation.PolicyLimits{
		DefaultTimeoutMS: s.options.DefaultTimeoutMS,
		MaxTimeoutMS:     s.options.MaxTimeoutMS,
	})
}

// timeoutSourceOf names who chose the bound, for the receipt.
func timeoutSourceOf(resolved operation.ResolvedExecutionPolicy) string {
	switch {
	case resolved.FromLegacy:
		return timeoutSourceLegacy
	case resolved.Unlimited():
		return timeoutSourceUnlimited
	case resolved.TimeoutFromDefault:
		return timeoutSourceDefault
	default:
		return timeoutSourceRequested
	}
}
