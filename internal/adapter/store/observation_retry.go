package store

import (
	"context"
	"sort"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/observation"
)

type observationTransitionRetry struct {
	state  observation.ObligationState
	reason string
}

type pendingObservationTransition struct {
	seq   observation.ChangeSeq
	retry observationTransitionRetry
}

func (r *Repository) enqueueObservationTransitionRetry(seq observation.ChangeSeq, state observation.ObligationState, reason string) {
	if seq == 0 {
		return
	}
	retry := observationTransitionRetry{state: state, reason: reason}
	r.observationRetryMu.Lock()
	current, exists := r.observationRetries[uint64(seq)]
	if !exists || current == retry {
		r.observationRetries[uint64(seq)] = retry
	}
	r.observationRetryMu.Unlock()
	if !exists || current == retry {
		r.signalObservationTransitionRetry()
	}
}

// ObservationTransitionRetryWakeups exposes only same-incarnation transition
// failures already known to a canonical writer. It never discovers or
// reconciles arbitrary prepared obligations.
func (r *Repository) ObservationTransitionRetryWakeups() <-chan struct{} {
	return r.observationRetryWake
}

func (r *Repository) signalObservationTransitionRetry() {
	select {
	case r.observationRetryWake <- struct{}{}:
	default:
	}
}

// RetryObservationTransitions retries only exact terminal transitions queued by
// a caller that already knows whether its canonical mutation committed or
// aborted. Startup reconciliation remains responsible for cross-incarnation
// prepared obligations whose intended transition was lost with the process.
func (r *Repository) RetryObservationTransitions(ctx context.Context) (int, error) {
	pending := r.snapshotObservationTransitionRetries()
	var firstErr error
	for _, item := range pending {
		if err := ctx.Err(); err != nil {
			return r.observationTransitionRetryCount(), err
		}
		var result app.StoreResult
		switch item.retry.state {
		case observation.ObligationCommitted:
			result = r.CommitObservation(ctx, item.seq)
		case observation.ObligationAborted:
			result = r.AbortObservation(ctx, item.seq, item.retry.reason)
		default:
			result = app.StoreResult{Durability: app.NoDurableChange, Err: observationStateConflict().Err}
		}
		if result.Err != nil {
			if firstErr == nil {
				firstErr = result.Err
			}
			continue
		}
		r.removeObservationTransitionRetry(item)
		r.cleanupObservationTransitionRetry(item)
	}
	return r.observationTransitionRetryCount(), firstErr
}

func (r *Repository) snapshotObservationTransitionRetries() []pendingObservationTransition {
	r.observationRetryMu.Lock()
	defer r.observationRetryMu.Unlock()
	out := make([]pendingObservationTransition, 0, len(r.observationRetries))
	for rawSeq, retry := range r.observationRetries {
		out = append(out, pendingObservationTransition{seq: observation.ChangeSeq(rawSeq), retry: retry})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].seq < out[j].seq })
	return out
}

func (r *Repository) removeObservationTransitionRetry(item pendingObservationTransition) {
	r.observationRetryMu.Lock()
	defer r.observationRetryMu.Unlock()
	if current, ok := r.observationRetries[uint64(item.seq)]; ok && current == item.retry {
		delete(r.observationRetries, uint64(item.seq))
	}
}

func (r *Repository) observationTransitionRetryCount() int {
	r.observationRetryMu.Lock()
	defer r.observationRetryMu.Unlock()
	return len(r.observationRetries)
}

func (r *Repository) cleanupObservationTransitionRetry(item pendingObservationTransition) {
	if item.retry.state != observation.ObligationCommitted {
		return
	}
	obligation, err := r.readObservation(item.seq)
	if err == nil && obligation.Kind == observation.EventMutationScopeChanged {
		r.removeMutationScopeObservationProofBestEffort(item.seq)
	}
}
