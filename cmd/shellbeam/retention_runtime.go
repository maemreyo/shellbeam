package main

import (
	"context"
	"time"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
)

// Retention runs behind readiness, never in front of it.
//
// Collecting expired history is housekeeping: nothing a caller asks for depends
// on it having finished, and a store with a large backlog would otherwise make
// an agent wait through a full sweep before its first command could run.
// Correctness-critical recovery -- singleton ownership and the admission index
// -- still completes before the daemon serves; this does not.
const (
	// retentionSweepBound keeps one pass short enough that a backlog is worked
	// through in steps rather than in a single burst that competes with real
	// work for the disk.
	retentionSweepBound = 512
	// retentionSweepInterval is how often the daemon looks for newly expired
	// history once it has caught up.
	retentionSweepInterval = 30 * time.Minute
	// retentionBacklogInterval paces the passes that follow a bounded sweep
	// which still had work left.
	retentionBacklogInterval = 30 * time.Second
)

// startRetention begins collecting expired terminal history in the background.
func startRetention(ctx context.Context, store *storeadapter.Repository, retentionHours int) {
	policy := storeadapter.RetentionPolicy{
		TerminalRetention: time.Duration(retentionHours) * time.Hour,
		MaxDeletions:      retentionSweepBound,
	}
	if !policy.Enabled() {
		// Not configured is not the same as "keep nothing": without a window,
		// the daemon deletes no history at all.
		return
	}
	go func() {
		for {
			report, err := store.CollectExpiredTerminals(ctx, policy)
			delay := retentionSweepInterval
			if err == nil && report.Remaining {
				delay = retentionBacklogInterval
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
		}
	}()
}
