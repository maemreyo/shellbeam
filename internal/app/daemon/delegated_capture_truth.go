package daemon

import (
	"context"

	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func (s *Service) delegatedCaptureTruth(sessionID string, captureGap bool) (receipt.CaptureTruth, bool) {
	store := s.delegatedStore()
	if store == nil {
		return receipt.CompleteCaptureTruth(), true
	}
	truth, err := store.LoadDelegatedCaptureTruth(context.Background(), operation.SessionID(sessionID))
	if err != nil {
		return receipt.CompleteCaptureTruth(), true
	}
	return truth, captureGap
}

func classifyDelegatedTerminal(snapshot delegatedTerminalSnapshot, obs delegatedapp.Observation, waitErr error) delegatedTerminalDecision {
	truth := snapshot.captureTruth.Clone()
	if truth.Quality == "" {
		truth = receipt.CompleteCaptureTruth()
	}
	addReason := func(reason receipt.CaptureReason) {
		next, err := truth.WithReason(reason)
		if err != nil {
			// The reason vocabulary is compile-time closed. Treat an impossible
			// internal merge failure as incomplete rather than overclaim capture.
			truth = receipt.CaptureTruth{Quality: receipt.CaptureIncomplete, Reasons: []receipt.CaptureReason{receipt.CaptureReasonTransportGap}, OutputComplete: false}
			return
		}
		truth = next
	}
	if snapshot.captureGap {
		addReason(receipt.CaptureReasonTransportGap)
	}
	decision := delegatedTerminalDecision{
		state: session.Abandoned, outcome: session.Ambiguous,
		captureQuality: truth.Quality, captureReasons: append([]receipt.CaptureReason(nil), truth.Reasons...), outputComplete: truth.OutputComplete,
		bindingLifecycle: delegated.LifecycleLost,
	}
	applyTruth := func() {
		decision.captureQuality = truth.Quality
		decision.captureReasons = append(decision.captureReasons[:0], truth.Reasons...)
		decision.outputComplete = truth.OutputComplete
	}
	if waitErr != nil || obs.Provider != snapshot.binding.ProviderIdentity() || !obs.ProviderCurrent || !obs.Terminal || obs.Owner != delegated.OwnerNone {
		decision.failureReason = "provider_lost"
		addReason(receipt.CaptureReasonProviderLost)
		applyTruth()
		return decision
	}
	decision.bindingLifecycle = delegated.LifecycleTerminal
	decision.exit.Code = obs.ExitCode
	// A reattached control observer counts only bytes delivered after the new
	// observer was established. Compare that forward-only count with the
	// canonical durable delta, never with the session lifetime total.
	observedDurableBytes := snapshot.outputBytes - snapshot.observerBase
	if observedDurableBytes < 0 || obs.OutputBytes != observedDurableBytes {
		decision.state, decision.outcome = session.Failed, session.Failure
		decision.failureReason = "output_capture_failed"
		addReason(receipt.CaptureReasonTransportGap)
		applyTruth()
		return decision
	}
	applyTruth()
	switch {
	case snapshot.target == session.Killed:
		decision.state, decision.outcome = session.Killed, session.KilledOutcome
	case snapshot.target == session.TimedOut:
		decision.state, decision.outcome = session.TimedOut, session.Timeout
	case obs.ExitCode == nil:
		decision.state, decision.outcome = session.Failed, session.Failure
		decision.failureReason = "exit_unobserved"
	case *obs.ExitCode == 0:
		decision.state, decision.outcome = session.Completed, session.Success
	default:
		decision.state, decision.outcome = session.Failed, session.Failure
	}
	return decision
}
