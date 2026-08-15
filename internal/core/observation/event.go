package observation

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

const (
	SchemaVersion   = 1
	MaxSummaryBytes = 512
	MaxSubjectBytes = 256
)

type ChangeSeq uint64
type EventKind string
type Continuity string

const (
	EventWorkspaceGenerationChanged EventKind = "workspace_generation_changed"
	EventOperationAdmitted          EventKind = "operation_admitted"
	EventProcessStarted             EventKind = "process_started"
	EventOutputAvailable            EventKind = "output_available"
	EventProcessTerminal            EventKind = "process_terminal"
	EventEvidenceRecorded           EventKind = "evidence_recorded"
	EventEvidenceValidityChanged    EventKind = "evidence_validity_changed"
	EventArtifactObserved           EventKind = "artifact_observed"
	EventManifestStatusChanged      EventKind = "manifest_status_changed"
	EventSessionHealthChanged       EventKind = "session_health_changed"
	EventStructuredChanged          EventKind = "structured_results_changed"
	EventCodeDiagnosticsChanged     EventKind = "code_diagnostics_changed"
	EventTelemetryChanged           EventKind = "telemetry_changed"
	EventReproRecorded              EventKind = "repro_recorded"
	EventMutationScopeChanged       EventKind = "mutation_scope_changed"

	ContinuityComplete         Continuity = "complete"
	ContinuitySnapshotRequired Continuity = "snapshot_required"
	ContinuityUnavailable      Continuity = "unavailable"
)

type Correlation struct {
	RepositoryID        string `json:"repository_id,omitempty"`
	WorkspaceID         string `json:"workspace_id,omitempty"`
	ActivityID          string `json:"activity_id,omitempty"`
	OperationID         string `json:"operation_id,omitempty"`
	SessionID           string `json:"session_id,omitempty"`
	WorkspaceGeneration string `json:"workspace_generation,omitempty"`
}

type Event struct {
	SchemaVersion  int         `json:"schema_version"`
	EventID        string      `json:"event_id"`
	StateRootEpoch string      `json:"state_root_epoch"`
	ChangeSeq      ChangeSeq   `json:"change_seq"`
	Kind           EventKind   `json:"kind"`
	RecordedAt     time.Time   `json:"recorded_at"`
	Correlation    Correlation `json:"correlation"`
	SubjectRef     string      `json:"subject_ref"`
	Summary        string      `json:"summary,omitempty"`
}

func InitialEventKinds() []EventKind {
	return []EventKind{EventWorkspaceGenerationChanged, EventOperationAdmitted, EventProcessStarted, EventOutputAvailable, EventProcessTerminal, EventEvidenceRecorded, EventEvidenceValidityChanged, EventArtifactObserved, EventManifestStatusChanged, EventSessionHealthChanged, EventStructuredChanged, EventCodeDiagnosticsChanged, EventTelemetryChanged, EventReproRecorded, EventMutationScopeChanged}
}

func (e Event) Validate() error {
	if e.SchemaVersion != SchemaVersion || !validOpaque(e.EventID, 128) || !validOpaque(e.StateRootEpoch, 128) || e.ChangeSeq == 0 || e.RecordedAt.IsZero() || !validEventKind(e.Kind) || !safeText(e.SubjectRef, MaxSubjectBytes) || (e.Summary != "" && !safeText(e.Summary, MaxSummaryBytes)) {
		return fmt.Errorf("invalid observation event")
	}
	return e.Correlation.Validate()
}

func (c Correlation) Validate() error {
	if c.OperationID != "" {
		if _, err := operation.ParseID(c.OperationID); err != nil {
			return err
		}
	}
	if c.SessionID != "" {
		if _, err := operation.ParseSessionID(c.SessionID); err != nil {
			return err
		}
	}
	if c.WorkspaceID != "" {
		if _, err := workspace.ParseWorkspaceID(c.WorkspaceID); err != nil {
			return err
		}
	}
	if c.RepositoryID != "" {
		if _, err := workspace.ParseRepositoryID(c.RepositoryID); err != nil {
			return err
		}
	}
	if c.ActivityID != "" && !validBoundedID(c.ActivityID) {
		return fmt.Errorf("invalid activity id")
	}
	if c.WorkspaceGeneration != "" && (!strings.HasPrefix(c.WorkspaceGeneration, "gen_") || len(c.WorkspaceGeneration) != 68) {
		return fmt.Errorf("invalid workspace generation")
	}
	return nil
}

func validEventKind(v EventKind) bool {
	for _, kind := range InitialEventKinds() {
		if v == kind {
			return true
		}
	}
	return false
}

func validBoundedID(v string) bool { return validOpaque(v, 128) && !strings.ContainsAny(v, "/\\") }
func validOpaque(v string, max int) bool {
	return strings.TrimSpace(v) == v && v != "" && len(v) <= max && safeText(v, max)
}
func safeText(v string, max int) bool {
	if v == "" || len(v) > max {
		return false
	}
	for _, r := range v {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
