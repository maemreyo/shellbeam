package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	contextexec "github.com/maemreyo/shellbeam/internal/core/contextexec"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func (r *Repository) ListContextExecRecoveryCandidates(ctx context.Context) ([]operation.ContextExecState, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(r.contextExecDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]operation.ContextExecState, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		if !validContextExecStoreID(id) || filepath.Base(entry.Name()) != entry.Name() {
			return nil, errors.New("unsafe context exec recovery entry")
		}
		record, err := r.loadContextExecRecordUnlocked(id)
		if err != nil {
			return nil, err
		}
		recover, err := r.contextExecRecoveryCandidate(ctx, record.State)
		if err != nil {
			return nil, err
		}
		if recover {
			out = append(out, record.State.Clone())
		}
	}
	return out, nil
}

func (r *Repository) contextExecRecoveryCandidate(ctx context.Context, state operation.ContextExecState) (bool, error) {
	if !state.Lifecycle.Terminal() {
		return true, nil
	}
	if state.Lifecycle != contextexec.LifecycleCanonicalized {
		return false, nil
	}
	lease, found, err := r.FindContextExecLease(ctx, operation.SessionID(state.Request.SessionID), state.Request.AuthorityEpoch)
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}
	if lease.ContextExecID != state.Request.ContextExecID || lease.RequestFingerprint != state.RequestFingerprint {
		return false, failure.New(failure.ContextExecAmbiguous, map[string]string{"context_exec_id": state.Request.ContextExecID, "session_id": state.Request.SessionID, "reason": "recovery_lease_identity_mismatch"}, nil)
	}
	return true, nil
}
