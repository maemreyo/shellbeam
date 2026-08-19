package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
)

const maxHumanControlRecordBytes = 16 << 10

type humanControlRecord struct {
	SchemaVersion int                   `json:"schema_version"`
	Signal        handoff.ControlSignal `json:"signal"`
	Outcome       string                `json:"outcome,omitempty"`
	Completed     bool                  `json:"completed"`
	CreatedAt     time.Time             `json:"created_at"`
	UpdatedAt     time.Time             `json:"updated_at"`
}

func (v humanControlRecord) validate() error {
	if v.SchemaVersion != handoffStoreSchemaVersion || v.CreatedAt.IsZero() || v.UpdatedAt.Before(v.CreatedAt) {
		return fmt.Errorf("invalid human control record")
	}
	if err := v.Signal.Validate(); err != nil {
		return err
	}
	if v.Completed && !validControlOutcome(v.Outcome) {
		return fmt.Errorf("invalid human control outcome")
	}
	if !v.Completed && v.Outcome != "" {
		return fmt.Errorf("pending human control has outcome")
	}
	return nil
}

func (r *Repository) ReserveControlSignal(ctx context.Context, signal handoff.ControlSignal) (handoff.ControlSignal, string, bool, app.StoreResult) {
	if err := ctx.Err(); err != nil {
		return handoff.ControlSignal{}, "", false, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if err := signal.Validate(); err != nil {
		return handoff.ControlSignal{}, "", false, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	r.delegatedSessionMu.Lock()
	defer r.delegatedSessionMu.Unlock()
	if existing, err := r.loadHumanControlLocked(signal.HandoffID, signal.ControlID); err == nil {
		decision, decideErr := handoff.DecideControl(&existing.Signal, signal, signal.AuthorityEpoch)
		if decideErr != nil {
			return existing.Signal, existing.Outcome, false, app.StoreResult{Durability: app.DurableChange, Err: decideErr}
		}
		if decision.Action != handoff.ControlReplay {
			return handoff.ControlSignal{}, "", false, app.StoreResult{Durability: app.DurableChange, Err: handoffConflict(signal.HandoffID, "control_replay_invalid")}
		}
		return existing.Signal, existing.Outcome, false, app.StoreResult{Durability: app.DurableChange}
	} else if !errors.Is(err, ErrNotFound) {
		return handoff.ControlSignal{}, "", false, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	record, err := r.loadHandoffRecordLocked(signal.HandoffID)
	if err != nil {
		return handoff.ControlSignal{}, "", false, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if _, err := handoff.DecideControl(nil, signal, record.State.AuthorityEpoch); err != nil {
		return handoff.ControlSignal{}, "", false, app.StoreResult{Durability: app.DurableChange, Err: err}
	}
	if !controlAllowedInPhase(signal.Kind, record.State.Phase) {
		return handoff.ControlSignal{}, "", false, app.StoreResult{Durability: app.DurableChange, Err: failure.New(failure.HandoffNotPending, map[string]string{"handoff_id": signal.HandoffID, "phase": string(record.State.Phase)}, nil)}
	}
	now := r.now().UTC()
	stored := humanControlRecord{SchemaVersion: handoffStoreSchemaVersion, Signal: signal, CreatedAt: now, UpdatedAt: now}
	path := r.interactiveHandoffControlPath(signal.HandoffID, signal.ControlID)
	if err := ensurePrivateDir(r.interactiveHandoffControlSessionDir(signal.HandoffID)); err != nil {
		return handoff.ControlSignal{}, "", false, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	result := r.writer.Create(path, stored)
	if errors.Is(result.Err, os.ErrExist) {
		existing, loadErr := r.loadHumanControlLocked(signal.HandoffID, signal.ControlID)
		if loadErr == nil && existing.Signal == signal {
			return signal, existing.Outcome, false, app.StoreResult{Durability: app.DurableChange}
		}
	}
	if result.Err != nil {
		return handoff.ControlSignal{}, "", false, result
	}
	return signal, "", true, result
}

func (r *Repository) CompleteControlSignal(ctx context.Context, signal handoff.ControlSignal, outcome string) (string, app.StoreResult) {
	if err := ctx.Err(); err != nil {
		return "", app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if err := signal.Validate(); err != nil || !validControlOutcome(outcome) {
		return "", app.StoreResult{Durability: app.NoDurableChange, Err: failure.New(failure.InvalidInput, map[string]string{"field": "human_control_outcome"}, err)}
	}
	r.delegatedSessionMu.Lock()
	defer r.delegatedSessionMu.Unlock()
	record, err := r.loadHumanControlLocked(signal.HandoffID, signal.ControlID)
	if err != nil {
		return "", app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if record.Signal != signal {
		return record.Outcome, app.StoreResult{Durability: app.DurableChange, Err: handoffConflict(signal.HandoffID, "control_signal_conflict")}
	}
	if record.Completed {
		if record.Outcome != outcome {
			return record.Outcome, app.StoreResult{Durability: app.DurableChange, Err: handoffConflict(signal.HandoffID, "control_outcome_conflict")}
		}
		return record.Outcome, app.StoreResult{Durability: app.DurableChange}
	}
	record.Completed = true
	record.Outcome = outcome
	record.UpdatedAt = monotonicHandoffTime(r.now().UTC(), record.UpdatedAt)
	result := r.writer.Replace(r.interactiveHandoffControlPath(signal.HandoffID, signal.ControlID), record)
	return outcome, result
}

func (r *Repository) loadHumanControlLocked(handoffID, controlID string) (humanControlRecord, error) {
	var out humanControlRecord
	if err := readPrivateJSON(r.interactiveHandoffControlPath(handoffID, controlID), maxHumanControlRecordBytes, &out); err != nil {
		return out, err
	}
	if err := out.validate(); err != nil || out.Signal.HandoffID != handoffID || out.Signal.ControlID != controlID {
		return humanControlRecord{}, fmt.Errorf("invalid human control record")
	}
	return out, nil
}

func controlAllowedInPhase(kind handoff.HumanControlKind, phase handoff.Phase) bool {
	switch kind {
	case handoff.HumanControlStatus:
		return true
	case handoff.HumanControlReady, handoff.HumanControlAbort:
		return phase == handoff.PhaseHumanOwned
	case handoff.HumanControlResume, handoff.HumanControlTerminate:
		return phase == handoff.PhaseAborted || phase == handoff.PhaseHumanFencing || phase == handoff.PhaseReclaimPending
	case handoff.HumanControlRequestControl:
		return phase == handoff.PhaseAgentOwned
	default:
		return false
	}
}

func validControlOutcome(v string) bool {
	if len(v) < 1 || len(v) > 128 {
		return false
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '-' || c == '.') {
			return false
		}
	}
	return true
}
