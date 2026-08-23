package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func (r *Repository) AcquireContextExecLease(ctx context.Context, sessionID operation.SessionID, epoch delegated.AuthorityEpoch, contextExecID, requestFingerprint string) (operation.ContextExecLease, bool, app.StoreResult) {
	if err := ctx.Err(); err != nil {
		return operation.ContextExecLease{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	lease := operation.ContextExecLease{SessionID: sessionID, AuthorityEpoch: epoch, ContextExecID: contextExecID, RequestFingerprint: requestFingerprint}
	if err := lease.Validate(); err != nil {
		return operation.ContextExecLease{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	r.delegatedSessionMu.Lock()
	defer r.delegatedSessionMu.Unlock()
	binding, err := r.loadDelegatedBindingLocked(sessionID)
	if err != nil {
		return operation.ContextExecLease{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if binding.Lifecycle != delegated.LifecycleLive || binding.DesiredOwner != delegated.OwnerAgent {
		return operation.ContextExecLease{}, false, app.StoreResult{Durability: app.DurableChange, Err: failure.New(failure.ContextExecNotAgentOwned, map[string]string{"session_id": string(sessionID), "reason": "delegated_binding_not_agent_live"}, nil)}
	}
	if binding.AuthorityEpoch != epoch {
		return operation.ContextExecLease{}, false, app.StoreResult{Durability: app.DurableChange, Err: failure.New(failure.ContextExecStaleGeneration, map[string]string{"session_id": string(sessionID), "reason": "authority_epoch_changed"}, nil)}
	}
	if existing, found, err := r.findContextExecLeaseLocked(sessionID, epoch); err != nil {
		return operation.ContextExecLease{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	} else if found {
		if existing == lease {
			return existing, false, app.StoreResult{Durability: app.DurableChange}
		}
		return existing, false, app.StoreResult{Durability: app.DurableChange, Err: contextExecConflict(contextExecID)}
	}
	if err := r.ensureContextExecLeaseStore(); err != nil {
		return operation.ContextExecLease{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	path := r.contextExecLeasePath(sessionID, epoch)
	result := r.writer.Create(path, lease)
	if result.Err == nil {
		return lease, true, result
	}
	if errors.Is(result.Err, os.ErrExist) {
		existing, found, err := r.findContextExecLeaseLocked(sessionID, epoch)
		if err != nil {
			return operation.ContextExecLease{}, false, app.StoreResult{Durability: app.AmbiguousChange, Err: err}
		}
		if found && existing == lease {
			return existing, false, app.StoreResult{Durability: app.DurableChange}
		}
		return existing, false, app.StoreResult{Durability: app.DurableChange, Err: contextExecConflict(contextExecID)}
	}
	return operation.ContextExecLease{}, false, result
}

func (r *Repository) FindContextExecLease(ctx context.Context, sessionID operation.SessionID, epoch delegated.AuthorityEpoch) (operation.ContextExecLease, bool, error) {
	if err := ctx.Err(); err != nil {
		return operation.ContextExecLease{}, false, err
	}
	if _, err := operation.ParseSessionID(string(sessionID)); err != nil {
		return operation.ContextExecLease{}, false, err
	}
	if err := epoch.Validate(); err != nil {
		return operation.ContextExecLease{}, false, err
	}
	r.delegatedSessionMu.Lock()
	defer r.delegatedSessionMu.Unlock()
	return r.findContextExecLeaseLocked(sessionID, epoch)
}

func (r *Repository) findContextExecLeaseLocked(sessionID operation.SessionID, epoch delegated.AuthorityEpoch) (operation.ContextExecLease, bool, error) {
	path := r.contextExecLeasePath(sessionID, epoch)
	var lease operation.ContextExecLease
	if err := readPrivateJSON(path, maxContextExecRecordBytes, &lease); errors.Is(err, ErrNotFound) {
		return operation.ContextExecLease{}, false, nil
	} else if err != nil {
		return operation.ContextExecLease{}, false, err
	}
	if err := lease.Validate(); err != nil || lease.SessionID != sessionID || lease.AuthorityEpoch != epoch {
		if err == nil {
			err = fmt.Errorf("context exec lease identity mismatch")
		}
		return operation.ContextExecLease{}, false, err
	}
	return lease, true, nil
}

func (r *Repository) ReleaseContextExecLease(ctx context.Context, lease operation.ContextExecLease) app.StoreResult {
	if err := ctx.Err(); err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if err := lease.Validate(); err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	r.delegatedSessionMu.Lock()
	defer r.delegatedSessionMu.Unlock()
	current, found, err := r.findContextExecLeaseLocked(lease.SessionID, lease.AuthorityEpoch)
	if err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if !found {
		return app.StoreResult{Durability: app.DurableChange}
	}
	if current != lease {
		return app.StoreResult{Durability: app.DurableChange, Err: contextExecConflict(lease.ContextExecID)}
	}
	path := r.contextExecLeasePath(lease.SessionID, lease.AuthorityEpoch)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if err := syncPrivateDir(filepath.Dir(path)); err != nil {
		return app.StoreResult{Durability: app.AmbiguousChange, Err: err}
	}
	return app.StoreResult{Durability: app.DurableChange}
}

func contextExecLeaseKey(sessionID operation.SessionID, epoch delegated.AuthorityEpoch) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("shellbeam-context-exec-lease-v1\\x00%s\\x00%d", sessionID, epoch)))
	return hex.EncodeToString(sum[:])
}

func (r *Repository) contextExecLeaseDir() string {
	return filepath.Join(r.contextExecRoot(), "leases", "v1")
}

func (r *Repository) contextExecLeasePath(sessionID operation.SessionID, epoch delegated.AuthorityEpoch) string {
	return filepath.Join(r.contextExecLeaseDir(), contextExecLeaseKey(sessionID, epoch)+".json")
}

func (r *Repository) ensureContextExecLeaseStore() error {
	if err := ensurePrivateDir(r.contextExecRoot()); err != nil {
		return fmt.Errorf("context exec store root: %w", err)
	}
	leases := filepath.Join(r.contextExecRoot(), "leases")
	if err := ensurePrivateDir(leases); err != nil {
		return fmt.Errorf("context exec lease root: %w", err)
	}
	if err := ensurePrivateDir(r.contextExecLeaseDir()); err != nil {
		return fmt.Errorf("context exec lease version: %w", err)
	}
	return nil
}
