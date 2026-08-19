package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

const (
	handoffStoreSchemaVersion  = 1
	maxHandoffRecordBytes      = 32 << 10
	maxHandoffTransactionBytes = 64 << 10
)

var _ app.InteractiveHandoffStore = (*Repository)(nil)

type handoffRecord struct {
	SchemaVersion int             `json:"schema_version"`
	Request       handoff.Request `json:"request"`
	State         handoff.State   `json:"state"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

func (v handoffRecord) validate() error {
	if v.SchemaVersion != handoffStoreSchemaVersion || v.CreatedAt.IsZero() || v.UpdatedAt.Before(v.CreatedAt) {
		return fmt.Errorf("invalid handoff record")
	}
	if err := v.Request.ValidateH2(); err != nil {
		return err
	}
	if err := v.State.ValidateH2(); err != nil {
		return err
	}
	if v.Request.HandoffID != v.State.HandoffID || v.Request.SessionID != v.State.SessionID {
		return fmt.Errorf("handoff record identity mismatch")
	}
	return nil
}

type handoffTransaction struct {
	SchemaVersion int               `json:"schema_version"`
	Record        handoffRecord     `json:"record"`
	PriorBinding  delegated.Binding `json:"prior_binding"`
	TargetBinding delegated.Binding `json:"target_binding"`
}

func (v handoffTransaction) validate() error {
	if v.SchemaVersion != handoffStoreSchemaVersion {
		return fmt.Errorf("invalid handoff transaction")
	}
	if err := v.Record.validate(); err != nil {
		return err
	}
	if err := v.PriorBinding.Validate(); err != nil {
		return err
	}
	if err := v.TargetBinding.Validate(); err != nil {
		return err
	}
	if !sameDelegatedBindingIdentity(v.PriorBinding, v.TargetBinding) || v.PriorBinding.SessionID != v.Record.State.SessionID {
		return fmt.Errorf("handoff transaction binding mismatch")
	}
	if v.TargetBinding.AuthorityEpoch < v.PriorBinding.AuthorityEpoch || v.TargetBinding.AuthorityEpoch != v.Record.State.AuthorityEpoch || v.TargetBinding.DesiredOwner != v.Record.State.DesiredOwner {
		return fmt.Errorf("handoff transaction authority mismatch")
	}
	return nil
}

func (r *Repository) initInteractiveHandoffStore() error {
	for _, path := range []string{r.interactiveHandoffDir(), r.interactiveHandoffRecordDir(), r.interactiveHandoffTransactionDir(), r.interactiveHandoffControlDir(), r.delegatedProvenanceDir()} {
		if err := ensurePrivateDir(path); err != nil {
			return fmt.Errorf("interactive handoff: %w", err)
		}
	}
	return nil
}

func (r *Repository) ReserveHandoff(ctx context.Context, req handoff.Request, initial handoff.State) (handoff.State, bool, app.StoreResult) {
	if err := ctx.Err(); err != nil {
		return handoff.State{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if err := validateInitialH2Handoff(req, initial); err != nil {
		return handoff.State{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	r.delegatedSessionMu.Lock()
	defer r.delegatedSessionMu.Unlock()
	if tx, err := r.loadHandoffTransactionLocked(req.HandoffID); err == nil {
		if !reflect.DeepEqual(tx.Record.Request, req) {
			return handoff.State{}, false, app.StoreResult{Durability: app.DurableChange, Err: handoffConflict(req.HandoffID, "transaction_conflict")}
		}
		return r.finishHandoffTransactionLocked(tx, false)
	} else if !errors.Is(err, ErrNotFound) {
		return handoff.State{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if existing, err := r.loadHandoffRecordLocked(req.HandoffID); err == nil {
		if !reflect.DeepEqual(existing.Request, req) {
			return existing.State, false, app.StoreResult{Durability: app.DurableChange, Err: handoffConflict(req.HandoffID, "request_conflict")}
		}
		return existing.State, false, app.StoreResult{Durability: app.DurableChange}
	} else if !errors.Is(err, ErrNotFound) {
		return handoff.State{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}

	sid := operation.SessionID(req.SessionID)
	binding, err := r.loadDelegatedBindingLocked(sid)
	if err != nil {
		return handoff.State{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if binding.Lifecycle != delegated.LifecycleLive || binding.DesiredOwner != delegated.OwnerAgent {
		return handoff.State{}, false, app.StoreResult{Durability: app.DurableChange, Err: handoffConflict(req.HandoffID, "delegated_session_not_agent_live")}
	}
	if initial.AuthorityEpoch != binding.AuthorityEpoch+1 {
		return handoff.State{}, false, app.StoreResult{Durability: app.DurableChange, Err: failure.New(failure.StaleControlGeneration, map[string]string{"session_id": req.SessionID, "expected_epoch": fmt.Sprint(binding.AuthorityEpoch + 1), "current_epoch": fmt.Sprint(initial.AuthorityEpoch)}, nil)}
	}
	now := r.now().UTC()
	record := handoffRecord{SchemaVersion: handoffStoreSchemaVersion, Request: req, State: initial, CreatedAt: now, UpdatedAt: now}
	target := binding
	target.AuthorityEpoch = initial.AuthorityEpoch
	target.DesiredOwner = initial.DesiredOwner
	target.UpdatedAt = monotonicHandoffTime(now, binding.UpdatedAt)
	tx := handoffTransaction{SchemaVersion: handoffStoreSchemaVersion, Record: record, PriorBinding: binding, TargetBinding: target}
	if err := tx.validate(); err != nil {
		return handoff.State{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if result := r.writer.Create(r.interactiveHandoffTransactionPath(req.HandoffID), tx); result.Err != nil {
		return handoff.State{}, false, result
	}
	return r.finishHandoffTransactionLocked(tx, true)
}

func validateInitialH2Handoff(req handoff.Request, state handoff.State) error {
	if err := req.ValidateH2(); err != nil {
		return err
	}
	if err := state.ValidateH2(); err != nil {
		return err
	}
	if req.HandoffID != state.HandoffID || req.SessionID != state.SessionID {
		return failure.New(failure.HandoffConflict, map[string]string{"handoff_id": req.HandoffID}, nil)
	}
	if state.Phase != handoff.PhaseAgentFencing || state.DesiredOwner != delegated.OwnerHuman || state.ProviderOwner != delegated.OwnerAgent || state.AgentIngress == handoff.IngressWritable || state.HumanIngress != handoff.IngressFenced || state.HumanClient != nil {
		return failure.New(failure.HandoffConflict, map[string]string{"handoff_id": req.HandoffID, "phase": string(state.Phase)}, nil)
	}
	return nil
}

func (r *Repository) finishHandoffTransactionLocked(tx handoffTransaction, created bool) (handoff.State, bool, app.StoreResult) {
	if err := tx.validate(); err != nil {
		return handoff.State{}, false, app.StoreResult{Durability: app.DurableChange, Err: err}
	}
	current, err := r.loadDelegatedBindingLocked(operation.SessionID(tx.TargetBinding.SessionID))
	if err != nil {
		return handoff.State{}, false, app.StoreResult{Durability: app.DurableChange, Err: err}
	}
	if reflect.DeepEqual(current, tx.PriorBinding) {
		if result := r.writer.Replace(r.delegatedBindingPath(operation.SessionID(current.SessionID)), tx.TargetBinding); result.Err != nil {
			return handoff.State{}, false, result
		}
	} else if !reflect.DeepEqual(current, tx.TargetBinding) {
		return handoff.State{}, false, app.StoreResult{Durability: app.DurableChange, Err: failure.New(failure.HandoffReclaimBlocked, map[string]string{"handoff_id": tx.Record.Request.HandoffID, "reason": "binding_transaction_mismatch"}, nil)}
	}
	path := r.interactiveHandoffRecordPath(tx.Record.Request.HandoffID)
	if existing, err := r.loadHandoffRecordLocked(tx.Record.Request.HandoffID); err == nil {
		if !reflect.DeepEqual(existing.Request, tx.Record.Request) {
			return existing.State, false, app.StoreResult{Durability: app.DurableChange, Err: handoffConflict(tx.Record.Request.HandoffID, "record_conflict")}
		}
	} else if errors.Is(err, ErrNotFound) {
		if result := r.writer.Create(path, tx.Record); result.Err != nil {
			return handoff.State{}, false, result
		}
	} else {
		return handoff.State{}, false, app.StoreResult{Durability: app.DurableChange, Err: err}
	}
	if err := r.removeHandoffTransactionLocked(tx.Record.Request.HandoffID); err != nil {
		return tx.Record.State, created, app.StoreResult{Durability: app.AmbiguousChange, Err: err}
	}
	return tx.Record.State, created, app.StoreResult{Durability: app.DurableChange}
}

func (r *Repository) AdvanceHandoff(ctx context.Context, want handoff.State) app.StoreResult {
	if err := ctx.Err(); err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if err := want.ValidateH2(); err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	r.delegatedSessionMu.Lock()
	defer r.delegatedSessionMu.Unlock()
	if tx, err := r.loadHandoffTransactionLocked(want.HandoffID); err == nil {
		if _, _, result := r.finishHandoffAdvanceTransactionLocked(tx); result.Err != nil {
			return result
		}
	} else if !errors.Is(err, ErrNotFound) {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	record, err := r.loadHandoffRecordLocked(want.HandoffID)
	if err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if want.HandoffID != record.State.HandoffID || want.SessionID != record.State.SessionID || want.AuthorityEpoch < record.State.AuthorityEpoch {
		return app.StoreResult{Durability: app.DurableChange, Err: handoffConflict(want.HandoffID, "state_identity_or_epoch_conflict")}
	}
	if reflect.DeepEqual(record.State, want) {
		return app.StoreResult{Durability: app.DurableChange}
	}
	binding, err := r.loadDelegatedBindingLocked(operation.SessionID(want.SessionID))
	if err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	bindingChange := binding.AuthorityEpoch != want.AuthorityEpoch || binding.DesiredOwner != want.DesiredOwner
	if bindingChange {
		if want.AuthorityEpoch <= binding.AuthorityEpoch {
			return app.StoreResult{Durability: app.DurableChange, Err: handoffConflict(want.HandoffID, "owner_change_without_epoch_rotation")}
		}
		now := r.now().UTC()
		target := binding
		target.AuthorityEpoch = want.AuthorityEpoch
		target.DesiredOwner = want.DesiredOwner
		target.UpdatedAt = monotonicHandoffTime(now, binding.UpdatedAt)
		nextRecord := record
		nextRecord.State = want
		nextRecord.UpdatedAt = monotonicHandoffTime(now, record.UpdatedAt)
		tx := handoffTransaction{SchemaVersion: handoffStoreSchemaVersion, Record: nextRecord, PriorBinding: binding, TargetBinding: target}
		if result := r.writer.Create(r.interactiveHandoffTransactionPath(want.HandoffID), tx); result.Err != nil && !errors.Is(result.Err, os.ErrExist) {
			return result
		}
		_, _, result := r.finishHandoffAdvanceTransactionLocked(tx)
		return result
	}
	record.State = want
	record.UpdatedAt = monotonicHandoffTime(r.now().UTC(), record.UpdatedAt)
	return r.writer.Replace(r.interactiveHandoffRecordPath(want.HandoffID), record)
}

func (r *Repository) finishHandoffAdvanceTransactionLocked(tx handoffTransaction) (handoff.State, bool, app.StoreResult) {
	current, err := r.loadDelegatedBindingLocked(operation.SessionID(tx.TargetBinding.SessionID))
	if err != nil {
		return handoff.State{}, false, app.StoreResult{Durability: app.DurableChange, Err: err}
	}
	if reflect.DeepEqual(current, tx.PriorBinding) {
		if result := r.writer.Replace(r.delegatedBindingPath(operation.SessionID(current.SessionID)), tx.TargetBinding); result.Err != nil {
			return handoff.State{}, false, result
		}
	} else if !reflect.DeepEqual(current, tx.TargetBinding) {
		return handoff.State{}, false, app.StoreResult{Durability: app.DurableChange, Err: failure.New(failure.HandoffReclaimBlocked, map[string]string{"handoff_id": tx.Record.Request.HandoffID, "reason": "advance_binding_mismatch"}, nil)}
	}
	if result := r.writer.Replace(r.interactiveHandoffRecordPath(tx.Record.Request.HandoffID), tx.Record); result.Err != nil {
		return handoff.State{}, false, result
	}
	if err := r.removeHandoffTransactionLocked(tx.Record.Request.HandoffID); err != nil {
		return tx.Record.State, false, app.StoreResult{Durability: app.AmbiguousChange, Err: err}
	}
	return tx.Record.State, false, app.StoreResult{Durability: app.DurableChange}
}

func (r *Repository) LoadHandoff(ctx context.Context, handoffID string) (handoff.Request, handoff.State, error) {
	if err := ctx.Err(); err != nil {
		return handoff.Request{}, handoff.State{}, err
	}
	if !validHandoffStoreID(handoffID) {
		return handoff.Request{}, handoff.State{}, failure.New(failure.InvalidInput, map[string]string{"field": "handoff_id"}, nil)
	}
	r.delegatedSessionMu.Lock()
	defer r.delegatedSessionMu.Unlock()
	record, err := r.loadHandoffRecordLocked(handoffID)
	return record.Request, record.State, err
}

func (r *Repository) loadHandoffRecordLocked(id string) (handoffRecord, error) {
	var out handoffRecord
	if err := readPrivateJSON(r.interactiveHandoffRecordPath(id), maxHandoffRecordBytes, &out); err != nil {
		return out, err
	}
	if err := out.validate(); err != nil || out.Request.HandoffID != id {
		return handoffRecord{}, fmt.Errorf("invalid handoff record")
	}
	return out, nil
}

func monotonicHandoffTime(now, prior time.Time) time.Time {
	if now.After(prior) {
		return now
	}
	return prior.Add(time.Nanosecond)
}

func handoffConflict(id, reason string) error {
	return failure.New(failure.HandoffConflict, map[string]string{"handoff_id": id, "reason": reason}, nil)
}

func validHandoffStoreID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		alphaNum := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
		if !alphaNum && (i == 0 || c != '_' && c != '-') {
			return false
		}
	}
	return true
}
