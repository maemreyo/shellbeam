//go:build linux || darwin

package supervisor

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

const timeoutStateSchemaVersion = 1

type TimeoutState struct {
	SchemaVersion      int                    `json:"schema_version"`
	TimeoutMS          int64                  `json:"timeout_ms"`
	TerminationGraceMS int64                  `json:"termination_grace_ms"`
	Deadline           time.Time              `json:"deadline"`
	Fired              bool                   `json:"fired"`
	Term               receipt.SignalEvidence `json:"term_signal"`
	Kill               receipt.SignalEvidence `json:"kill_signal"`
}

func freezeTimeoutState(layout Layout, timeoutMS int64, grace time.Duration, now time.Time) (TimeoutState, error) {
	if timeoutMS <= 0 || grace < 0 {
		return TimeoutState{}, fmt.Errorf("invalid persistent timeout")
	}
	state := TimeoutState{
		SchemaVersion: timeoutStateSchemaVersion, TimeoutMS: timeoutMS, TerminationGraceMS: grace.Milliseconds(),
		Deadline: now.UTC().Add(time.Duration(timeoutMS) * time.Millisecond),
	}
	path := timeoutStatePath(layout)
	if err := createPrivateJSON(path, state, 16<<10); err != nil {
		return TimeoutState{}, err
	}
	loaded, err := LoadTimeoutState(layout)
	if err != nil {
		return TimeoutState{}, err
	}
	if loaded.TimeoutMS != state.TimeoutMS || loaded.TerminationGraceMS != state.TerminationGraceMS || !loaded.Deadline.Equal(state.Deadline) {
		return TimeoutState{}, fmt.Errorf("persistent timeout conflict")
	}
	return loaded, nil
}

func persistTimeoutState(layout Layout, state TimeoutState) error {
	if err := validateTimeoutState(state); err != nil {
		return err
	}
	return replacePrivateJSON(timeoutStatePath(layout), state, 16<<10)
}

func LoadTimeoutState(layout Layout) (TimeoutState, error) {
	if err := validateLayout(layout); err != nil {
		return TimeoutState{}, err
	}
	var state TimeoutState
	if err := loadPrivateJSON(timeoutStatePath(layout), 16<<10, &state); err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, io.EOF) {
			return TimeoutState{}, err
		}
		return TimeoutState{}, fmt.Errorf("invalid persistent timeout state")
	}
	if err := validateTimeoutState(state); err != nil {
		return TimeoutState{}, err
	}
	return state, nil
}

func validateTimeoutState(state TimeoutState) error {
	if state.SchemaVersion != timeoutStateSchemaVersion || state.TimeoutMS <= 0 || state.TerminationGraceMS < 0 || state.Deadline.IsZero() {
		return fmt.Errorf("invalid persistent timeout state")
	}
	if state.Kill.Attempted && !state.Fired {
		return fmt.Errorf("invalid persistent timeout state")
	}
	if state.Term.Succeeded && !state.Term.Attempted || state.Kill.Succeeded && !state.Kill.Attempted {
		return fmt.Errorf("invalid persistent timeout state")
	}
	return nil
}

func timeoutStatePath(layout Layout) string { return filepath.Join(layout.SessionDir, "timeout.json") }
