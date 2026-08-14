package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/observation"
)

func (r *Repository) CompactEvents(ctx context.Context, policy EventRetentionPolicy) (EventRetentionResult, error) {
	if err := ctx.Err(); err != nil {
		return EventRetentionResult{}, err
	}
	r.eventMu.Lock()
	defer r.eventMu.Unlock()
	if policy.MaxEvents < 1 || policy.MaxBytes < 1 || policy.MaxAge <= 0 {
		return EventRetentionResult{}, fmt.Errorf("invalid event retention policy")
	}
	if policy.Now.IsZero() {
		policy.Now = time.Now().UTC()
	}
	entries, err := eventEntries(r.eventDir())
	if err != nil {
		return EventRetentionResult{}, err
	}
	remove := make([]bool, len(entries))
	count := len(entries)
	var bytes int64
	for i, entry := range entries {
		bytes += entry.size
		if policy.Now.Sub(entry.modTime) > policy.MaxAge {
			remove[i] = true
			count--
			bytes -= entry.size
		}
	}
	for i := range entries {
		if remove[i] {
			continue
		}
		if count <= policy.MaxEvents && bytes <= policy.MaxBytes {
			break
		}
		remove[i] = true
		count--
		bytes -= entries[i].size
	}
	state, err := r.LoadEventProjectionState(ctx)
	if err != nil {
		return EventRetentionResult{}, err
	}
	compacted := state.CompactedThroughSeq
	for i, yes := range remove {
		if yes && entries[i].seq > compacted {
			compacted = entries[i].seq
		}
	}
	if compacted != state.CompactedThroughSeq {
		next := state
		next.CompactedThroughSeq = compacted
		if err := r.saveEventProjectionStateLocked(ctx, next); err != nil {
			return EventRetentionResult{}, err
		}
		for i, yes := range remove {
			if !yes {
				continue
			}
			if err := os.Remove(r.eventPath(entries[i].seq)); err != nil {
				return EventRetentionResult{}, err
			}
		}
		if err := syncPrivateDir(r.eventDir()); err != nil {
			return EventRetentionResult{}, err
		}
	}
	return EventRetentionResult{CompactedThroughSeq: compacted, RemainingEvents: count, RemainingBytes: bytes}, nil
}

type eventEntry struct {
	seq     observation.ChangeSeq
	size    int64
	modTime time.Time
}

func eventEntries(dir string) ([]eventEntry, error) {
	sequences, err := eventSequences(dir)
	if err != nil {
		return nil, err
	}
	out := make([]eventEntry, 0, len(sequences))
	for _, seq := range sequences {
		info, err := os.Lstat(filepath.Join(dir, fmt.Sprintf("%020d.json", uint64(seq))))
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || !ownedByCurrent(info) || info.Size() < 1 || info.Size() > MaxEventBytes {
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("unsafe event entry")
		}
		out = append(out, eventEntry{seq: seq, size: info.Size(), modTime: info.ModTime()})
	}
	return out, nil
}

func syncPrivateDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
