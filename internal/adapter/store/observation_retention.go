//go:build linux || darwin

package store

import (
	"context"
	"os"

	"github.com/maemreyo/shellbeam/internal/core/observation"
)

// Obligation retention exists because the ledger had no upper bound at all.
//
// Every admitted operation, every started and terminated process and every
// visible range of output writes one small file here, and nothing ever removed
// one. Historically that was not merely disk growth: observationSequences had
// a 65,536-entry ceiling used by startup and the materializer, so a busy daemon
// could eventually fail to open its own store. The scanner is now uncapped and
// metadata-only, but collection remains necessary to bound O(n) directory work,
// inode pressure and filesystem allocation-unit amplification.
//
// What makes collection safe is the event projection. A committed obligation at
// or below MaterializedThroughSeq has already been turned into a durable event,
// and the materializer only ever asks for sequences above that mark, so the
// record behind it has no remaining reader.
//
// Two records are never collectible, and both exceptions are load-bearing:
//
// A prepared obligation names a write whose subject is still unproven, which is
// exactly what reconcilePreparedExecutionObservations walks the ledger to
// resolve. It is also, by construction, below the projection only if the
// materializer stopped there.
//
// The newest record is pinned whatever its state, because initObservationStore
// rebuilds the high watermark from the highest surviving filename. Collect it
// and a restarted daemon comes back with a watermark below its own event
// projection, which the materializer treats as corruption and refuses to run
// against. Housekeeping must not be able to do that.

// ObligationRetentionPolicy bounds one collection pass.
type ObligationRetentionPolicy struct {
	// MaxDeletions bounds one sweep so a large backlog is worked through over
	// several passes rather than one burst. Zero means unbounded.
	MaxDeletions int
}

// ObligationRetentionReport describes one pass.
type ObligationRetentionReport struct {
	Scanned   int
	Collected int
	Freed     int64
	// Remaining is true when the sweep stopped at its bound with more to do.
	Remaining bool
}

// CollectMaterializedObligations removes obligation records the event
// projection has already absorbed.
func (r *Repository) CollectMaterializedObligations(ctx context.Context, policy ObligationRetentionPolicy) (ObligationRetentionReport, error) {
	report := ObligationRetentionReport{}
	state, err := r.LoadEventProjectionState(ctx)
	if err != nil {
		return report, err
	}
	if state.MaterializedThroughSeq == 0 {
		return report, nil
	}

	r.observationMu.Lock()
	defer r.observationMu.Unlock()
	sequences, err := observationSequences(r.observationDir())
	if err != nil {
		return report, err
	}
	if len(sequences) == 0 {
		return report, nil
	}
	newest := sequences[len(sequences)-1]

	for _, seq := range sequences {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		if seq > state.MaterializedThroughSeq {
			// Sequences are ascending, so nothing further is collectible.
			break
		}
		if seq == newest {
			continue
		}
		report.Scanned++
		if policy.MaxDeletions > 0 && report.Collected >= policy.MaxDeletions {
			report.Remaining = true
			break
		}
		freed, collected, err := r.collectObligation(seq)
		if err != nil {
			return report, err
		}
		if collected {
			report.Collected++
			report.Freed += freed
		}
	}
	if report.Collected > 0 {
		if err := syncPrivateDir(r.observationDir()); err != nil {
			return report, err
		}
		r.addStateBytes(-report.Freed)
	}
	return report, nil
}

// collectObligation removes one record if its state allows it.
func (r *Repository) collectObligation(seq observation.ChangeSeq) (int64, bool, error) {
	record, err := r.readObservation(seq)
	if err != nil {
		// A record this pass cannot interpret is left for reconciliation to
		// reason about rather than deleted on a guess.
		return 0, false, nil
	}
	if record.State == observation.ObligationPrepared {
		return 0, false, nil
	}
	path := r.observationPath(seq)
	info, err := os.Lstat(path)
	if err != nil {
		return 0, false, nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return 0, false, err
	}
	return info.Size(), true, nil
}
