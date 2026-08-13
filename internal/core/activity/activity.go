package activity

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

const SchemaVersion = 1
const MaxIDBytes = 128
const MaxOperationHistory = 64

type ID string

type OperationRef struct {
	OperationID string                `json:"operation_id"`
	SessionID   string                `json:"session_id"`
	WorkspaceID workspace.WorkspaceID `json:"workspace_id,omitempty"`
	ObservedAt  time.Time             `json:"observed_at"`
}

type Admission struct {
	ActivityID  ID
	OperationID string
	SessionID   string
	WorkspaceID workspace.WorkspaceID
	CWD         string
	ObservedAt  time.Time
}

type Activity struct {
	SchemaVersion       int                     `json:"schema_version"`
	ID                  ID                      `json:"activity_id"`
	Label               string                  `json:"label,omitempty"`
	WorkspaceIDs        []workspace.WorkspaceID `json:"workspace_ids,omitempty"`
	Operations          []OperationRef          `json:"operations,omitempty"`
	Baselines           []Baseline              `json:"baselines,omitempty"`
	CompactedOperations int                     `json:"compacted_operations"`
	CreatedAt           time.Time               `json:"created_at"`
	UpdatedAt           time.Time               `json:"updated_at"`
}

func ParseID(value string) (ID, error) {
	if value == "" || len(value) > MaxIDBytes || !utf8.ValidString(value) || value == "." || value == ".." {
		return "", fmt.Errorf("invalid activity id")
	}
	for _, r := range value {
		if r == 47 || r == 92 || unicode.IsControl(r) || unicode.IsSpace(r) {
			return "", fmt.Errorf("invalid activity id")
		}
	}
	return ID(value), nil
}

func New(id ID, now time.Time) Activity {
	return Activity{SchemaVersion: SchemaVersion, ID: id, CreatedAt: now.UTC(), UpdatedAt: now.UTC()}
}

func (a *Activity) ObserveOperation(ref OperationRef, maxHistory int) {
	if maxHistory <= 0 {
		maxHistory = MaxOperationHistory
	}
	if ref.WorkspaceID != "" && !containsWorkspace(a.WorkspaceIDs, ref.WorkspaceID) {
		a.WorkspaceIDs = append(a.WorkspaceIDs, ref.WorkspaceID)
	}
	a.Operations = append(a.Operations, ref)
	if maxHistory > 0 && len(a.Operations) > maxHistory {
		drop := len(a.Operations) - maxHistory
		a.Operations = append([]OperationRef(nil), a.Operations[drop:]...)
		a.CompactedOperations += drop
	}
	if ref.ObservedAt.After(a.UpdatedAt) {
		a.UpdatedAt = ref.ObservedAt.UTC()
	}
}

func (a Activity) BaselineFor(workspaceID workspace.WorkspaceID) (Baseline, bool) {
	for _, baseline := range a.Baselines {
		if baseline.WorkspaceID == workspaceID {
			return baseline, true
		}
	}
	return Baseline{}, false
}

func (a *Activity) AddBaseline(baseline Baseline) {
	if _, found := a.BaselineFor(baseline.WorkspaceID); found {
		return
	}
	a.Baselines = append(a.Baselines, baseline)
	if baseline.WorkspaceID != "" && !containsWorkspace(a.WorkspaceIDs, baseline.WorkspaceID) {
		a.WorkspaceIDs = append(a.WorkspaceIDs, baseline.WorkspaceID)
	}
}

func (a Activity) Validate(maxHistory int) error {
	if maxHistory <= 0 {
		maxHistory = MaxOperationHistory
	}
	if a.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported activity schema")
	}
	if _, err := ParseID(string(a.ID)); err != nil {
		return err
	}
	if a.CreatedAt.IsZero() || a.UpdatedAt.IsZero() || a.UpdatedAt.Before(a.CreatedAt) || a.CompactedOperations < 0 {
		return fmt.Errorf("invalid activity timestamps or compaction")
	}
	if maxHistory > 0 && len(a.Operations) > maxHistory {
		return fmt.Errorf("activity history exceeds limit")
	}
	seen := map[workspace.WorkspaceID]struct{}{}
	for _, id := range a.WorkspaceIDs {
		if _, err := workspace.ParseWorkspaceID(string(id)); err != nil {
			return err
		}
		if _, exists := seen[id]; exists {
			return fmt.Errorf("duplicate workspace reference")
		}
		seen[id] = struct{}{}
	}
	for _, baseline := range a.Baselines {
		if err := baseline.Validate(); err != nil {
			return err
		}
	}
	for _, ref := range a.Operations {
		if strings.TrimSpace(ref.OperationID) == "" || strings.TrimSpace(ref.SessionID) == "" || ref.ObservedAt.IsZero() {
			return fmt.Errorf("invalid activity operation reference")
		}
	}
	return nil
}

func containsWorkspace(values []workspace.WorkspaceID, id workspace.WorkspaceID) bool {
	for _, value := range values {
		if value == id {
			return true
		}
	}
	return false
}
