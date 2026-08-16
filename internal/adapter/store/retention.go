package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

// Retention is garbage collection of terminal history, and nothing else.
//
// It is deliberately not part of admission. Admission answers "may this run",
// in constant time, from an index; retention answers "is this still worth
// keeping", by walking history. Letting the second leak into the first is what
// made every Start pay for the whole store, and this must not reintroduce it --
// so retention runs on its own, off the request path, and admission never calls
// into it.
//
// Only ordinary sessions are collected. A persistent session has bindings,
// recovery records and reconnection semantics of its own; treating its terminal
// receipt as the end of its life would delete state another subsystem still
// reasons about. Extending retention to cover it needs that subsystem audited
// first, not assumed.

// gcStagingPrefix marks a session directory that has been withdrawn from view
// and is being removed. Its presence is not an error: it is what an interrupted
// sweep leaves behind, and the next sweep finishes the job.
const gcStagingPrefix = ".gc-"

// RetentionPolicy decides what may be collected.
type RetentionPolicy struct {
	// TerminalRetention is how long a terminal session is kept. Zero or less
	// disables collection entirely rather than meaning "keep nothing": an
	// operator who has not configured retention has not asked for deletion.
	TerminalRetention time.Duration
	// MaxDeletions bounds one sweep so a large backlog is worked through over
	// several passes instead of one unbounded burst. Zero means unbounded.
	MaxDeletions int
	// Now allows tests to place the cutoff precisely.
	Now func() time.Time
}

// Enabled reports whether this policy collects anything at all.
func (p RetentionPolicy) Enabled() bool { return p.TerminalRetention > 0 }

// RetentionReport describes one sweep.
type RetentionReport struct {
	Scanned   int
	Collected int
	Freed     int64
	// Remaining is true when the sweep stopped at its bound with more to do.
	Remaining bool
}

// CollectExpiredTerminals removes terminal ordinary sessions whose durable
// terminal record is older than the retention window.
//
// It is idempotent and safe to interrupt: each session is withdrawn from view
// by an atomic rename before anything under it is removed, so a caller either
// sees the whole session or none of it, and an interrupted removal is finished
// by the next sweep.
func (r *Repository) CollectExpiredTerminals(ctx context.Context, policy RetentionPolicy) (RetentionReport, error) {
	report := RetentionReport{}
	if !policy.Enabled() {
		return report, nil
	}
	now := policy.Now
	if now == nil {
		now = r.now
	}
	cutoff := now().Add(-policy.TerminalRetention)

	entries, err := os.ReadDir(filepath.Join(r.root, "sessions"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return report, nil
		}
		return report, err
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		if strings.HasPrefix(entry.Name(), gcStagingPrefix) {
			// Left by an interrupted sweep; finish it.
			r.discardStaged(entry.Name())
			continue
		}
		if !entry.IsDir() {
			continue
		}
		report.Scanned++
		if policy.MaxDeletions > 0 && report.Collected >= policy.MaxDeletions {
			report.Remaining = true
			break
		}
		freed, collected, err := r.collectSession(ctx, entry.Name(), cutoff)
		if err != nil {
			return report, err
		}
		if collected {
			report.Collected++
			report.Freed += freed
		}
	}
	if report.Freed > 0 {
		r.addStateBytes(-report.Freed)
	}
	return report, nil
}

// collectSession removes one session if it is eligible, reporting the bytes it
// gave back.
func (r *Repository) collectSession(ctx context.Context, sessionID string, cutoff time.Time) (int64, bool, error) {
	dir := filepath.Join(r.root, "sessions", sessionID)
	var snapshot session.Snapshot
	if err := readStrict(filepath.Join(dir, "metadata.json"), &snapshot); err != nil {
		// A session with no readable state is not something retention should
		// interpret; leave it for reconciliation to reason about.
		return 0, false, nil
	}
	// Age comes from the durable terminal record, not from the filesystem. A
	// modification time is not domain truth: copying, restoring or compacting a
	// store rewrites it, and history would then expire by accident or outlive
	// its window depending on how the state directory was moved.
	if !snapshot.State.Terminal() || !snapshot.UpdatedAt.Before(cutoff) {
		return 0, false, nil
	}
	reservation, err := r.LoadOperation(ctx, operation.ID(snapshot.OperationID))
	switch {
	case err == nil && reservation.Persistent:
		return 0, false, nil
	case err != nil && !errors.Is(err, ErrNotFound):
		return 0, false, err
	}

	freed := directorySize(dir)

	// The operation record goes first, and the order is forced rather than
	// tidy. A reservation whose session directory is missing is repaired at
	// startup by recreating that session as starting -- so leaving the record
	// behind would resurrect everything retention had just collected, each one
	// holding a capacity slot. Losing the record and keeping the directory is
	// harmless by comparison: the next sweep collects it.
	if err := removeIfPresent(r.operationPath(operation.ID(snapshot.OperationID))); err != nil {
		return 0, false, err
	}
	if err := removeIfPresent(r.rawOutputRefPath(sessionID)); err != nil {
		return 0, false, err
	}

	// One rename withdraws the whole session from view. Removing files in place
	// would let a concurrent reader see a session with a receipt but no state,
	// or output with neither; visibility has to be all or nothing.
	staged := filepath.Join(r.root, "sessions", gcStagingPrefix+sessionID)
	if err := os.Rename(dir, staged); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, false, nil
		}
		return 0, false, err
	}
	if err := os.RemoveAll(staged); err != nil {
		// The session is already invisible; the leftover is collected next time.
		return freed, true, nil
	}
	return freed, true, nil
}

func (r *Repository) discardStaged(name string) {
	_ = os.RemoveAll(filepath.Join(r.root, "sessions", name))
}

func (r *Repository) operationPath(id operation.ID) string {
	return filepath.Join(r.root, "operations", string(id)+".json")
}

func removeIfPresent(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// directorySize sums what a session is holding, for advisory byte accounting.
func directorySize(dir string) int64 {
	var total int64
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total
}
