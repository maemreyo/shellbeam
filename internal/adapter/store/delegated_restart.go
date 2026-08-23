package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func (r *Repository) ensureDelegatedRecoveryMarkerLocked(marker delegatedRecoveryMarker) app.StoreResult {
	if err := marker.validate(); err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	path := r.delegatedRecoveryPath(operation.SessionID(marker.Binding.SessionID))
	var existing delegatedRecoveryMarker
	if err := readPrivateJSON(path, maxDelegatedRecoveryMarkerBytes, &existing); err == nil {
		if existing.validate() == nil && existing == marker {
			return app.StoreResult{Durability: app.DurableChange}
		}
		return app.StoreResult{Durability: app.DurableChange, Err: delegatedStateConflict(marker.Binding, "recovery_marker_conflict", nil)}
	} else if !errors.Is(err, ErrNotFound) {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	result := r.writer.Create(path, marker)
	if errors.Is(result.Err, os.ErrExist) {
		return r.ensureDelegatedRecoveryMarkerLocked(marker)
	}
	return result
}

func (r *Repository) ListDelegatedRecoveryCandidates(ctx context.Context) ([]delegated.Binding, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.delegatedSessionMu.Lock()
	defer r.delegatedSessionMu.Unlock()
	entries, err := os.ReadDir(r.delegatedRecoveryDir())
	if err != nil {
		return nil, err
	}
	if len(entries) > r.limits.MaxSessions {
		return nil, failure.New(failure.CapacityExceeded, nil, nil)
	}
	out := make([]delegated.Binding, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(entry.Name(), ".json") {
			return nil, fmt.Errorf("invalid delegated recovery entry")
		}
		sid, err := operation.ParseSessionID(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			return nil, err
		}
		marker, err := r.loadDelegatedRecoveryMarkerLocked(sid)
		if err != nil {
			return nil, err
		}
		binding, err := r.loadDelegatedBindingLocked(sid)
		if errors.Is(err, ErrNotFound) {
			if result := r.writer.Create(r.delegatedBindingPath(sid), marker.Binding); result.Err != nil && !errors.Is(result.Err, os.ErrExist) {
				return nil, result.Err
			}
			binding = marker.Binding
		} else if err != nil {
			return nil, err
		}
		if _, err := r.loadDelegatedProviderRefLocked(sid); errors.Is(err, ErrNotFound) {
			if result := r.writer.Create(r.delegatedProviderRefPath(sid), marker.ProviderRef); result.Err != nil && !errors.Is(result.Err, os.ErrExist) {
				return nil, result.Err
			}
		} else if err != nil {
			return nil, err
		}
		if !sameDelegatedBindingIdentity(binding, marker.Binding) {
			return nil, delegatedStateConflict(binding, "recovery_identity", nil)
		}
		switch binding.Lifecycle {
		case delegated.LifecycleProvisioning, delegated.LifecycleLive:
			out = append(out, binding)
		case delegated.LifecycleTerminal, delegated.LifecycleLost:
			_ = r.removeDelegatedRecoveryMarkerLocked(sid)
		default:
			return nil, fmt.Errorf("invalid delegated recovery lifecycle")
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SessionID < out[j].SessionID })
	return out, nil
}

func (r *Repository) loadDelegatedRecoveryMarkerLocked(sid operation.SessionID) (delegatedRecoveryMarker, error) {
	var m delegatedRecoveryMarker
	if err := readPrivateJSON(r.delegatedRecoveryPath(sid), maxDelegatedRecoveryMarkerBytes, &m); err != nil {
		return m, err
	}
	if err := m.validate(); err != nil || m.Binding.SessionID != string(sid) {
		return m, fmt.Errorf("invalid delegated recovery marker")
	}
	return m, nil
}

func (r *Repository) removeDelegatedRecoveryMarkerLocked(sid operation.SessionID) error {
	if err := os.Remove(r.delegatedRecoveryPath(sid)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return syncPrivateDir(r.delegatedRecoveryDir())
}
