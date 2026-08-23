package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

const (
	delegatedCaptureStateSchemaVersion = 1
	maxDelegatedCaptureStateBytes      = 8 << 10
)

type delegatedCaptureState struct {
	SchemaVersion int                  `json:"schema_version"`
	SessionID     string               `json:"session_id"`
	Truth         receipt.CaptureTruth `json:"truth"`
	UpdatedAt     time.Time            `json:"updated_at"`
}

func (v delegatedCaptureState) validate() error {
	if v.SchemaVersion != delegatedCaptureStateSchemaVersion || v.UpdatedAt.IsZero() {
		return fmt.Errorf("invalid delegated capture state")
	}
	if _, err := operation.ParseSessionID(v.SessionID); err != nil {
		return err
	}
	return v.Truth.Validate()
}

func (r *Repository) LoadDelegatedCaptureTruth(ctx context.Context, sid operation.SessionID) (receipt.CaptureTruth, error) {
	if err := ctx.Err(); err != nil {
		return receipt.CaptureTruth{}, err
	}
	if _, err := operation.ParseSessionID(string(sid)); err != nil {
		return receipt.CaptureTruth{}, err
	}
	r.delegatedSessionMu.Lock()
	defer r.delegatedSessionMu.Unlock()
	if _, err := r.loadDelegatedBindingLocked(sid); err != nil {
		return receipt.CaptureTruth{}, err
	}
	return r.loadDelegatedCaptureTruthLocked(sid)
}

func (r *Repository) loadDelegatedCaptureTruthLocked(sid operation.SessionID) (receipt.CaptureTruth, error) {
	var state delegatedCaptureState
	if err := readPrivateJSON(r.delegatedCapturePath(sid), maxDelegatedCaptureStateBytes, &state); err != nil {
		if errors.Is(err, ErrNotFound) {
			return receipt.CompleteCaptureTruth(), nil
		}
		return receipt.CaptureTruth{}, err
	}
	if err := state.validate(); err != nil || state.SessionID != string(sid) {
		return receipt.CaptureTruth{}, fmt.Errorf("invalid delegated capture state")
	}
	return state.Truth.Clone(), nil
}

func (r *Repository) MarkDelegatedCaptureReason(ctx context.Context, sid operation.SessionID, reason receipt.CaptureReason) (receipt.CaptureTruth, app.StoreResult) {
	if err := ctx.Err(); err != nil {
		return receipt.CaptureTruth{}, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if _, err := operation.ParseSessionID(string(sid)); err != nil {
		return receipt.CaptureTruth{}, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	r.delegatedSessionMu.Lock()
	defer r.delegatedSessionMu.Unlock()
	binding, err := r.loadDelegatedBindingLocked(sid)
	if err != nil {
		return receipt.CaptureTruth{}, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if binding.Lifecycle != delegated.LifecycleProvisioning && binding.Lifecycle != delegated.LifecycleLive {
		return receipt.CaptureTruth{}, app.StoreResult{Durability: app.DurableChange, Err: fmt.Errorf("delegated capture state is terminal")}
	}
	current, err := r.loadDelegatedCaptureTruthLocked(sid)
	if err != nil {
		return receipt.CaptureTruth{}, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	next, err := current.WithReason(reason)
	if err != nil {
		return receipt.CaptureTruth{}, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if sameCaptureTruth(current, next) {
		return current.Clone(), app.StoreResult{Durability: app.DurableChange}
	}
	state := delegatedCaptureState{SchemaVersion: delegatedCaptureStateSchemaVersion, SessionID: string(sid), Truth: next.Clone(), UpdatedAt: r.now().UTC()}
	if state.UpdatedAt.IsZero() {
		return receipt.CaptureTruth{}, app.StoreResult{Durability: app.NoDurableChange, Err: fmt.Errorf("delegated capture timestamp unavailable")}
	}
	path := r.delegatedCapturePath(sid)
	var existing delegatedCaptureState
	var result app.StoreResult
	if err := readPrivateJSON(path, maxDelegatedCaptureStateBytes, &existing); errors.Is(err, ErrNotFound) {
		result = r.writer.Create(path, state)
		if errors.Is(result.Err, os.ErrExist) {
			result = r.writer.Replace(path, state)
		}
	} else if err != nil {
		return receipt.CaptureTruth{}, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	} else {
		if err := existing.validate(); err != nil || existing.SessionID != string(sid) {
			return receipt.CaptureTruth{}, app.StoreResult{Durability: app.DurableChange, Err: fmt.Errorf("invalid delegated capture state")}
		}
		result = r.writer.Replace(path, state)
	}
	if result.Err != nil {
		return receipt.CaptureTruth{}, result
	}
	return next.Clone(), result
}

func sameCaptureTruth(a, b receipt.CaptureTruth) bool {
	if a.Quality != b.Quality || a.OutputComplete != b.OutputComplete || len(a.Reasons) != len(b.Reasons) {
		return false
	}
	for i := range a.Reasons {
		if a.Reasons[i] != b.Reasons[i] {
			return false
		}
	}
	return true
}
