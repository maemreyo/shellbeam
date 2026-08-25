package store

import (
	"context"
	"errors"
	"fmt"
	"os"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	persistent "github.com/maemreyo/shellbeam/internal/core/persistentsession"
)

const maxPersistentKillRecordBytes = 4 << 10

func (r *Repository) LookupPersistentKill(ctx context.Context, sessionID operation.SessionID, killID string) (persistent.KillRecord, bool, error) {
	if err := ctx.Err(); err != nil {
		return persistent.KillRecord{}, false, err
	}
	if _, err := operation.ParseSessionID(string(sessionID)); err != nil {
		return persistent.KillRecord{}, false, err
	}
	if _, err := operation.ParseID(killID); err != nil {
		return persistent.KillRecord{}, false, err
	}
	r.persistentSessionMu.Lock()
	defer r.persistentSessionMu.Unlock()
	record, err := r.loadPersistentKillLocked(sessionID, killID)
	if errors.Is(err, ErrNotFound) {
		return persistent.KillRecord{}, false, nil
	}
	return record, err == nil, err
}

func (r *Repository) ReservePersistentKill(ctx context.Context, sessionID operation.SessionID, killID, signal string, terminal bool) (persistent.KillRecord, bool, app.StoreResult) {
	if err := ctx.Err(); err != nil {
		return persistent.KillRecord{}, false, app.StoreResult{Err: err}
	}
	if _, err := operation.ParseSessionID(string(sessionID)); err != nil {
		return persistent.KillRecord{}, false, app.StoreResult{Err: failure.New(failure.InvalidInput, map[string]string{"field": "session_id"}, err)}
	}
	if _, err := operation.ParseID(killID); err != nil {
		return persistent.KillRecord{}, false, app.StoreResult{Err: failure.New(failure.InvalidInput, map[string]string{"field": "kill_id"}, err)}
	}
	if signal != "INT" && signal != "TERM" && signal != "KILL" {
		return persistent.KillRecord{}, false, app.StoreResult{Err: failure.New(failure.InvalidInput, map[string]string{"field": "signal"}, fmt.Errorf("invalid signal"))}
	}

	r.persistentSessionMu.Lock()
	defer r.persistentSessionMu.Unlock()
	if existing, err := r.loadPersistentKillLocked(sessionID, killID); err == nil {
		if existing.Signal != signal {
			return existing, false, app.StoreResult{Durability: app.DurableChange, Err: failure.New(failure.OperationMetadataConflict, map[string]string{"field": "signal"}, fmt.Errorf("kill_conflict"))}
		}
		return existing, false, app.StoreResult{Durability: app.DurableChange}
	} else if !errors.Is(err, ErrNotFound) {
		return persistent.KillRecord{}, false, app.StoreResult{Err: err}
	}
	dir := r.persistentKillDir(sessionID)
	if err := ensurePrivateDir(dir); err != nil {
		return persistent.KillRecord{}, false, app.StoreResult{Err: err}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return persistent.KillRecord{}, false, app.StoreResult{Err: err}
	}
	if len(entries) >= persistent.MaxKillRecords {
		return persistent.KillRecord{}, false, app.StoreResult{Err: failure.New(failure.PersistentKillHistoryExhausted, map[string]string{"reason": "record_limit"}, nil)}
	}
	now := r.now()
	record := persistent.KillRecord{
		SchemaVersion: persistent.KillRecordSchemaVersion, SessionID: string(sessionID), KillID: killID, Signal: signal,
		Needed: !terminal, Complete: terminal, CreatedAt: now, UpdatedAt: now,
	}
	result := r.writer.Create(r.persistentKillPath(sessionID, killID), record)
	if result.Err == nil {
		return record, true, result
	}
	if errors.Is(result.Err, os.ErrExist) {
		existing, err := r.loadPersistentKillLocked(sessionID, killID)
		if err == nil && existing.Signal == signal {
			return existing, false, app.StoreResult{Durability: app.DurableChange}
		}
	}
	return persistent.KillRecord{}, false, result
}

func (r *Repository) CompletePersistentKill(ctx context.Context, want persistent.KillRecord) (persistent.KillRecord, app.StoreResult) {
	if err := ctx.Err(); err != nil {
		return persistent.KillRecord{}, app.StoreResult{Err: err}
	}
	if err := want.Validate(); err != nil || !want.Complete {
		return persistent.KillRecord{}, app.StoreResult{Err: failure.New(failure.InvalidInput, map[string]string{"field": "kill_id"}, err)}
	}
	sessionID := operation.SessionID(want.SessionID)
	r.persistentSessionMu.Lock()
	defer r.persistentSessionMu.Unlock()
	current, err := r.loadPersistentKillLocked(sessionID, want.KillID)
	if err != nil {
		return persistent.KillRecord{}, app.StoreResult{Err: err}
	}
	if current.Signal != want.Signal {
		return current, app.StoreResult{Durability: app.DurableChange, Err: failure.New(failure.OperationMetadataConflict, map[string]string{"field": "signal"}, fmt.Errorf("kill_conflict"))}
	}
	if current.Complete {
		if current.Attempted == want.Attempted && current.Succeeded == want.Succeeded && current.Needed == want.Needed {
			return current, app.StoreResult{Durability: app.DurableChange}
		}
		return current, app.StoreResult{Durability: app.DurableChange, Err: failure.New(failure.OperationConflict, nil, nil)}
	}
	next := current
	next.Attempted, next.Succeeded, next.Needed, next.Complete = want.Attempted, want.Succeeded, want.Needed, true
	next.UpdatedAt = r.now()
	if err := next.Validate(); err != nil {
		return current, app.StoreResult{Err: err}
	}
	result := r.writer.Replace(r.persistentKillPath(sessionID, want.KillID), next)
	if result.Err != nil {
		return current, result
	}
	return next, result
}

func (r *Repository) loadPersistentKillLocked(sessionID operation.SessionID, killID string) (persistent.KillRecord, error) {
	var record persistent.KillRecord
	if err := readPrivateJSON(r.persistentKillPath(sessionID, killID), maxPersistentKillRecordBytes, &record); err != nil {
		return record, err
	}
	if err := record.Validate(); err != nil || record.SessionID != string(sessionID) || record.KillID != killID {
		if err == nil {
			err = fmt.Errorf("persistent kill identity mismatch")
		}
		return record, err
	}
	return record, nil
}
