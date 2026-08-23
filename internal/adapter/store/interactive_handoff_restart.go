package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

const maxDelegatedProvenanceBytes = 4 << 10

type delegatedInputProvenance struct {
	SchemaVersion int       `json:"schema_version"`
	SessionID     string    `json:"session_id"`
	Value         string    `json:"input_authority_provenance"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (v delegatedInputProvenance) validate() error {
	if v.SchemaVersion != 1 || v.UpdatedAt.IsZero() {
		return fmt.Errorf("invalid delegated provenance")
	}
	if _, err := operation.ParseSessionID(v.SessionID); err != nil {
		return err
	}
	if v.Value != receipt.InputAuthorityHumanWriteGranted {
		return fmt.Errorf("invalid persisted delegated provenance")
	}
	return nil
}

func (r *Repository) MarkHumanWriteAuthorityGranted(ctx context.Context, sid operation.SessionID) app.StoreResult {
	if err := ctx.Err(); err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if _, err := operation.ParseSessionID(string(sid)); err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: failure.New(failure.InvalidInput, map[string]string{"field": "session_id"}, err)}
	}
	r.delegatedSessionMu.Lock()
	defer r.delegatedSessionMu.Unlock()
	if _, err := r.loadDelegatedBindingLocked(sid); err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	path := r.delegatedProvenancePath(sid)
	var existing delegatedInputProvenance
	if err := readPrivateJSON(path, maxDelegatedProvenanceBytes, &existing); err == nil {
		if existing.validate() != nil || existing.SessionID != string(sid) {
			return app.StoreResult{Durability: app.DurableChange, Err: fmt.Errorf("invalid delegated provenance")}
		}
		return app.StoreResult{Durability: app.DurableChange}
	} else if !errors.Is(err, ErrNotFound) {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	v := delegatedInputProvenance{SchemaVersion: 1, SessionID: string(sid), Value: receipt.InputAuthorityHumanWriteGranted, UpdatedAt: r.now().UTC()}
	return r.writer.Create(path, v)
}

func (r *Repository) LoadInputAuthorityProvenance(ctx context.Context, sid operation.SessionID) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if _, err := operation.ParseSessionID(string(sid)); err != nil {
		return "", failure.New(failure.InvalidInput, map[string]string{"field": "session_id"}, err)
	}
	r.delegatedSessionMu.Lock()
	defer r.delegatedSessionMu.Unlock()
	if _, err := r.loadDelegatedBindingLocked(sid); err != nil {
		return "", err
	}
	var v delegatedInputProvenance
	if err := readPrivateJSON(r.delegatedProvenancePath(sid), maxDelegatedProvenanceBytes, &v); errors.Is(err, ErrNotFound) {
		return receipt.InputAuthorityAgentOnly, nil
	} else if err != nil {
		return "", err
	}
	if err := v.validate(); err != nil || v.SessionID != string(sid) {
		return "", fmt.Errorf("invalid delegated provenance")
	}
	return v.Value, nil
}

func (r *Repository) ListHandoffRecoveryCandidates(ctx context.Context) ([]handoff.State, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.delegatedSessionMu.Lock()
	defer r.delegatedSessionMu.Unlock()
	byID := map[string]handoff.State{}
	entries, err := os.ReadDir(r.interactiveHandoffRecordDir())
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		id := entry.Name()[:len(entry.Name())-len(".json")]
		record, loadErr := r.loadHandoffRecordLocked(id)
		if loadErr != nil {
			return nil, loadErr
		}
		if record.State.Phase != handoff.PhaseAgentOwned {
			byID[id] = record.State
		}
	}
	txs, err := os.ReadDir(r.interactiveHandoffTransactionDir())
	if err != nil {
		return nil, err
	}
	for _, entry := range txs {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		id := entry.Name()[:len(entry.Name())-len(".json")]
		tx, loadErr := r.loadHandoffTransactionLocked(id)
		if loadErr != nil {
			return nil, loadErr
		}
		state := tx.Record.State
		state.AgentIngress = handoff.IngressFenced
		state.HumanIngress = handoff.IngressFenced
		state.HumanClient = nil
		byID[id] = state
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]handoff.State, 0, len(ids))
	for _, id := range ids {
		out = append(out, byID[id])
	}
	return out, nil
}

func (r *Repository) RecoverHandoff(ctx context.Context, id string) (handoff.State, app.StoreResult) {
	if err := ctx.Err(); err != nil {
		return handoff.State{}, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if !validHandoffStoreID(id) {
		return handoff.State{}, app.StoreResult{Durability: app.NoDurableChange, Err: failure.New(failure.InvalidInput, map[string]string{"field": "handoff_id"}, nil)}
	}
	r.delegatedSessionMu.Lock()
	defer r.delegatedSessionMu.Unlock()
	if _, err := r.loadHandoffTransactionLocked(id); err == nil {
		if result := r.reconcileHandoffTransactionLocked(id); result.Err != nil {
			return handoff.State{}, result
		}
	} else if !errors.Is(err, ErrNotFound) {
		return handoff.State{}, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	record, err := r.loadHandoffRecordLocked(id)
	if err != nil {
		return handoff.State{}, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	return record.State, app.StoreResult{Durability: app.DurableChange}
}

func (r *Repository) loadHandoffTransactionLocked(id string) (handoffTransaction, error) {
	var out handoffTransaction
	if err := readPrivateJSON(r.interactiveHandoffTransactionPath(id), maxHandoffTransactionBytes, &out); err != nil {
		return out, err
	}
	if err := out.validate(); err != nil || out.Record.Request.HandoffID != id {
		return handoffTransaction{}, fmt.Errorf("invalid handoff transaction")
	}
	return out, nil
}

func (r *Repository) removeHandoffTransactionLocked(id string) error {
	path := r.interactiveHandoffTransactionPath(id)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return syncPrivateDir(filepath.Dir(path))
}

func (r *Repository) reconcileHandoffTransactionLocked(id string) app.StoreResult {
	tx, err := r.loadHandoffTransactionLocked(id)
	if err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	_, recordErr := r.loadHandoffRecordLocked(id)
	var result app.StoreResult
	switch {
	case errors.Is(recordErr, ErrNotFound):
		_, _, result = r.finishHandoffTransactionLocked(tx, false)
	case recordErr == nil:
		_, _, result = r.finishHandoffAdvanceTransactionLocked(tx)
	default:
		return app.StoreResult{Durability: app.NoDurableChange, Err: recordErr}
	}
	return result
}

func (r *Repository) reconcileHandoffTransactions(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.delegatedSessionMu.Lock()
	defer r.delegatedSessionMu.Unlock()
	entries, err := os.ReadDir(r.interactiveHandoffTransactionDir())
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			return fmt.Errorf("invalid handoff transaction entry")
		}
		id := entry.Name()[:len(entry.Name())-len(".json")]
		if !validHandoffStoreID(id) {
			return fmt.Errorf("invalid handoff transaction identity")
		}
		if result := r.reconcileHandoffTransactionLocked(id); result.Err != nil {
			return result.Err
		}
	}
	return nil
}
