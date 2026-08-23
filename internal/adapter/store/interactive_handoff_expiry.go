package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
)

const MaxExpiredHandoffBatch = 256

// ListExpiredHandoffs returns a bounded deterministic batch of non-terminal
// handoffs whose total lifetime began before cutoff. It performs no mutation;
// one central daemon housekeeping loop owns repeated expiry passes.
func (r *Repository) ListExpiredHandoffs(ctx context.Context, cutoff time.Time, limit int) ([]string, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if cutoff.IsZero() || limit < 1 || limit > MaxExpiredHandoffBatch {
		return nil, false, fmt.Errorf("invalid handoff expiry bounds")
	}
	r.delegatedSessionMu.Lock()
	defer r.delegatedSessionMu.Unlock()
	entries, err := os.ReadDir(r.interactiveHandoffRecordDir())
	if err != nil {
		return nil, false, err
	}
	out := make([]string, 0, min(limit, len(entries)))
	more := false
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		id := entry.Name()[:len(entry.Name())-len(".json")]
		record, err := r.loadHandoffRecordLocked(id)
		if err != nil {
			return nil, false, err
		}
		if record.State.Phase == handoff.PhaseAgentOwned || record.State.Phase == handoff.PhaseAborted || !record.CreatedAt.Before(cutoff) {
			continue
		}
		if len(out) == limit {
			more = true
			break
		}
		out = append(out, id)
	}
	return out, more, nil
}
