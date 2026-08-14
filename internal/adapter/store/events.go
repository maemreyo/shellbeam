package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/observation"
)

const (
	MaxEventBytes       = 16 << 10
	MaxEventListRecords = 1024
	maxEventScanRecords = 65536
)

type eventCursorKeyRecord struct {
	SchemaVersion  int    `json:"schema_version"`
	StateRootEpoch string `json:"state_root_epoch"`
	Generation     string `json:"generation"`
	Secret         string `json:"secret"`
}

type EventRetentionPolicy struct {
	MaxEvents int
	MaxBytes  int64
	MaxAge    time.Duration
	Now       time.Time
}

type EventRetentionResult struct {
	CompactedThroughSeq observation.ChangeSeq
	RemainingEvents     int
	RemainingBytes      int64
}

func (r *Repository) initEventStore() error {
	if err := ensurePrivateDir(r.eventDir()); err != nil {
		return fmt.Errorf("observation events: %w", err)
	}
	if _, err := r.EventCursorKey(context.Background()); err != nil {
		return err
	}
	if _, err := r.LoadEventProjectionState(context.Background()); err != nil {
		return err
	}
	return nil
}

func (r *Repository) EventCursorKey(ctx context.Context) (observation.CursorKeyMaterial, error) {
	if err := ctx.Err(); err != nil {
		return observation.CursorKeyMaterial{}, err
	}
	path := r.eventCursorKeyPath()
	var record eventCursorKeyRecord
	if err := readPrivateJSON(path, 4096, &record); err == nil {
		return cursorKeyMaterial(record)
	} else if !errors.Is(err, ErrNotFound) {
		return observation.CursorKeyMaterial{}, err
	}
	record, err := newEventCursorKeyRecord()
	if err != nil {
		return observation.CursorKeyMaterial{}, err
	}
	result := r.writer.Create(path, record)
	if result.Err != nil {
		if err := readPrivateJSON(path, 4096, &record); err != nil {
			return observation.CursorKeyMaterial{}, result.Err
		}
	}
	return cursorKeyMaterial(record)
}

func (r *Repository) PutEvent(ctx context.Context, event observation.Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.eventMu.Lock()
	defer r.eventMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := event.Validate(); err != nil {
		return err
	}
	key, err := r.EventCursorKey(ctx)
	if err != nil {
		return err
	}
	if event.StateRootEpoch != key.StateRootEpoch {
		return fmt.Errorf("event state-root epoch mismatch")
	}
	path := r.eventPath(event.ChangeSeq)
	var existing observation.Event
	if err := readPrivateJSON(path, MaxEventBytes, &existing); err == nil {
		if reflect.DeepEqual(existing, event) {
			return nil
		}
		return fmt.Errorf("event_projection_conflict")
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	result := r.writer.Create(path, event)
	if result.Err == nil {
		return nil
	}
	if err := readPrivateJSON(path, MaxEventBytes, &existing); err == nil && reflect.DeepEqual(existing, event) {
		return nil
	}
	return result.Err
}

func (r *Repository) ListEvents(ctx context.Context, after, through observation.ChangeSeq, limit int) ([]observation.Event, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	r.eventMu.Lock()
	defer r.eventMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if limit < 1 || limit > MaxEventListRecords || through < after {
		return nil, false, fmt.Errorf("invalid event list bounds")
	}
	sequences, err := eventSequences(r.eventDir())
	if err != nil {
		return nil, false, err
	}
	out := make([]observation.Event, 0, min(limit, len(sequences)))
	more := false
	for _, seq := range sequences {
		if seq <= after || seq > through {
			continue
		}
		if len(out) == limit {
			more = true
			break
		}
		var event observation.Event
		if err := readPrivateJSON(r.eventPath(seq), MaxEventBytes, &event); err != nil {
			return nil, false, err
		}
		if event.ChangeSeq != seq || event.Validate() != nil {
			return nil, false, fmt.Errorf("invalid event projection")
		}
		out = append(out, event)
	}
	return out, more, nil
}

func (r *Repository) LoadEventProjectionState(ctx context.Context) (observation.ProjectionState, error) {
	if err := ctx.Err(); err != nil {
		return observation.ProjectionState{}, err
	}
	var state observation.ProjectionState
	err := readPrivateJSON(r.eventProjectionStatePath(), 4096, &state)
	if errors.Is(err, ErrNotFound) {
		return observation.ProjectionState{SchemaVersion: observation.SchemaVersion}, nil
	}
	if err != nil {
		return state, err
	}
	return state, state.Validate()
}

func (r *Repository) SaveEventProjectionState(ctx context.Context, state observation.ProjectionState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.eventMu.Lock()
	defer r.eventMu.Unlock()
	return r.saveEventProjectionStateLocked(ctx, state)
}

func (r *Repository) saveEventProjectionStateLocked(ctx context.Context, state observation.ProjectionState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := state.Validate(); err != nil {
		return err
	}
	current, err := r.LoadEventProjectionState(ctx)
	if err != nil {
		return err
	}
	if state.MaterializedThroughSeq < current.MaterializedThroughSeq || state.CompactedThroughSeq < current.CompactedThroughSeq {
		return fmt.Errorf("event_projection_state_regression")
	}
	if state == current {
		return nil
	}
	return r.writer.Replace(r.eventProjectionStatePath(), state).Err
}

func (r *Repository) eventDir() string {
	return filepath.Join(r.root, "observations", "events")
}
func (r *Repository) eventPath(seq observation.ChangeSeq) string {
	return filepath.Join(r.eventDir(), fmt.Sprintf("%020d.json", uint64(seq)))
}
func (r *Repository) eventCursorKeyPath() string {
	return filepath.Join(r.root, "observations", "cursor-key.json")
}
func (r *Repository) eventProjectionStatePath() string {
	return filepath.Join(r.root, "observations", "projection-state.json")
}
