package store

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func (r *Repository) ReconcilePersistentOutput(ctx context.Context, id operation.SessionID, offset int64, data []byte) (app.PersistentOutputResult, app.StoreResult) {
	r.mu.Lock()
	defer r.mu.Unlock()

	path := filepath.Join(r.root, "sessions", string(id), "output.log")
	extent, err := outputSize(path)
	if err != nil {
		return app.PersistentOutputResult{}, persistentOutputFailure(id, "canonical_extent", err)
	}
	if offset < 0 {
		return app.PersistentOutputResult{CanonicalExtent: extent}, persistentOutputFailure(id, "invalid_offset", nil)
	}
	if offset > extent {
		return app.PersistentOutputResult{CanonicalExtent: extent}, persistentOutputFailure(id, "gap", nil)
	}
	end := offset + int64(len(data))
	if end < offset {
		return app.PersistentOutputResult{CanonicalExtent: extent}, persistentOutputFailure(id, "invalid_offset", nil)
	}
	if end > r.limits.MaxSessionOutput {
		return app.PersistentOutputResult{CanonicalExtent: extent}, persistentOutputFailure(id, "output_limit", nil)
	}

	overlapEnd := end
	if overlapEnd > extent {
		overlapEnd = extent
	}
	if overlapEnd > offset {
		match, err := canonicalOutputRangeMatches(path, offset, data[:int(overlapEnd-offset)])
		if err != nil {
			return app.PersistentOutputResult{CanonicalExtent: extent}, persistentOutputFailure(id, "canonical_read", err)
		}
		if !match {
			return app.PersistentOutputResult{CanonicalExtent: extent}, persistentOutputFailure(id, "overlap_mismatch", nil)
		}
	}
	if end <= extent {
		return app.PersistentOutputResult{CanonicalExtent: extent, Replay: true}, app.StoreResult{Durability: app.NoDurableChange}
	}

	suffix := data[int(extent-offset):]
	seq, prepared := r.prepareOutputObservation(ctx, id, extent, end)
	if prepared.Err != nil {
		return app.PersistentOutputResult{CanonicalExtent: extent}, prepared
	}
	n, result := appendOutputBytes(path, suffix)
	r.finishOutputObservation(seq, path, extent, end, result)
	result.ObservationSeq = uint64(seq)
	currentExtent, sizeErr := outputSize(path)
	if sizeErr != nil {
		if result.Err == nil {
			result.Err = sizeErr
		}
		return app.PersistentOutputResult{CanonicalExtent: extent, AppendedBytes: n}, result
	}
	return app.PersistentOutputResult{CanonicalExtent: currentExtent, AppendedBytes: n}, result
}

func canonicalOutputRangeMatches(path string, offset int64, want []byte) (bool, error) {
	if len(want) == 0 {
		return true, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()
	got := make([]byte, len(want))
	n, err := file.ReadAt(got, offset)
	if err != nil && err != io.EOF {
		return false, err
	}
	if n != len(want) {
		return false, fmt.Errorf("canonical output short read")
	}
	return bytes.Equal(got, want), nil
}

func persistentOutputFailure(id operation.SessionID, reason string, cause error) app.StoreResult {
	return app.StoreResult{
		Durability: app.NoDurableChange,
		Err: failure.New(failure.PersistentRecoveryOutputConflict, map[string]string{
			"session_id": string(id), "reason": reason,
		}, cause),
	}
}
