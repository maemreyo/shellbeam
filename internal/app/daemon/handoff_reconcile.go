package daemon

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
)

const (
	defaultHandoffReconcilePerCandidate = 2 * time.Second
	defaultHandoffReconcileConcurrency  = 4
	defaultHandoffReconcileBudget       = 8 * time.Second
)

type HandoffStartupOptions struct {
	PerHandoff     time.Duration
	MaxConcurrency int
	TotalBudget    time.Duration
}

func normalizeHandoffStartupOptions(options HandoffStartupOptions) HandoffStartupOptions {
	if options.PerHandoff <= 0 {
		options.PerHandoff = defaultHandoffReconcilePerCandidate
	}
	if options.MaxConcurrency <= 0 {
		options.MaxConcurrency = defaultHandoffReconcileConcurrency
	}
	if options.TotalBudget <= 0 {
		options.TotalBudget = defaultHandoffReconcileBudget
	}
	return options
}

// ReconcileHandoffStartup re-proves every non-agent-owned handoff before the
// daemon is marked ready. A bounded timeout is a startup failure, not evidence
// that either actor owns the session.
func (s *Service) ReconcileHandoffStartup(ctx context.Context, candidates []handoff.State, options HandoffStartupOptions) error {
	if len(candidates) == 0 {
		return nil
	}
	if s.handoff == nil {
		return failure.New(failure.FeatureUnavailable, map[string]string{"feature": "interactive_handoff"}, nil)
	}
	options = normalizeHandoffStartupOptions(options)
	totalCtx, cancel := context.WithTimeout(ctx, options.TotalBudget)
	defer cancel()

	jobs := make(chan handoff.State)
	errs := make(chan error, len(candidates))
	workers := options.MaxConcurrency
	if workers > len(candidates) {
		workers = len(candidates)
	}
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for candidate := range jobs {
				if totalCtx.Err() != nil {
					errs <- handoffStartupBlocked(candidate, "startup_budget_expired", totalCtx.Err())
					continue
				}
				candidateCtx, cancelCandidate := context.WithTimeout(totalCtx, options.PerHandoff)
				_, err := s.handoff.Reconcile(candidateCtx, candidate.HandoffID)
				if err == nil && candidateCtx.Err() != nil {
					err = handoffStartupBlocked(candidate, "candidate_budget_expired", candidateCtx.Err())
				}
				cancelCandidate()
				errs <- err
			}
		}()
	}
	for _, candidate := range candidates {
		jobs <- candidate
	}
	close(jobs)
	wg.Wait()
	close(errs)
	var first error
	for err := range errs {
		if err != nil && first == nil {
			first = err
		}
	}
	return first
}

func handoffStartupBlocked(candidate handoff.State, reason string, cause error) error {
	phase := string(candidate.Phase)
	if phase == "" {
		phase = "unknown"
	}
	return failure.New(failure.HandoffReclaimBlocked, map[string]string{
		"handoff_id": candidate.HandoffID,
		"reason":     reason,
		"phase":      phase,
	}, fmt.Errorf("handoff startup reconciliation: %w", cause))
}
