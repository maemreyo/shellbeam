//go:build linux || darwin

package main

import (
	"context"
	"time"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
)

// The observation ledger is collected on its own terms, not the session's.
//
// Terminal-session retention is optional by design: an operator who configured
// no window has not asked for their history to be deleted. This is not that.
// The obligation ledger historically had a 65,536-entry scan ceiling shared by
// daemon startup and the materializer. The scanner no longer carries that hard
// refusal, but leaving the ledger uncollected would still make every metadata
// scan grow linearly and retain one allocation unit/inode per tiny record. This
// sweep therefore runs whatever terminal-session retention is configured.
//
// Both passes here only ever remove records the event projection has already
// absorbed, which is why neither needs a window an operator has to choose.
const (
	// observationSweepBound keeps one pass short enough that a backlog is
	// worked through in steps rather than in one burst competing with real work.
	observationSweepBound = 1024
	// observationSweepInterval is the pace once the ledger has caught up.
	observationSweepInterval = 10 * time.Minute
	// observationBacklogInterval paces the passes that follow a bounded sweep
	// which still had work left.
	observationBacklogInterval = 30 * time.Second
	// eventRetentionMaxRecords and eventRetentionMaxBytes bound the projected
	// event log, which is what agents actually read.
	eventRetentionMaxRecords = 8192
	eventRetentionMaxBytes   = 32 << 20
	eventRetentionAge        = 7 * 24 * time.Hour
)

// startObservationRetention begins collecting the observation ledger.
func startObservationRetention(ctx context.Context, store *storeadapter.Repository) {
	go func() {
		for {
			remaining := sweepObservations(ctx, store)
			delay := observationSweepInterval
			if remaining {
				delay = observationBacklogInterval
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
		}
	}()
}

// sweepObservations runs one pass and reports whether it stopped at its bound.
//
// Errors are dropped rather than retried on a tighter loop: this is
// housekeeping behind readiness, and a store that cannot be collected right now
// is a store the next pass will find in the same or a better state.
func sweepObservations(ctx context.Context, store *storeadapter.Repository) bool {
	report, err := store.CollectMaterializedObligations(ctx, storeadapter.ObligationRetentionPolicy{
		MaxDeletions: observationSweepBound,
	})
	_, _ = store.CompactEvents(ctx, storeadapter.EventRetentionPolicy{
		MaxEvents: eventRetentionMaxRecords, MaxBytes: eventRetentionMaxBytes, MaxAge: eventRetentionAge,
	})
	return err == nil && report.Remaining
}
