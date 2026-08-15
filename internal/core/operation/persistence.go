package operation

import (
	"time"

	"github.com/maemreyo/shellbeam/internal/core/evidence"
	project "github.com/maemreyo/shellbeam/internal/core/project"
)

type Reservation struct {
	SchemaVersion                 int                     `json:"schema_version"`
	OperationID                   ID                      `json:"operation_id"`
	ActivityID                    string                  `json:"activity_id,omitempty"`
	WorkspaceID                   string                  `json:"workspace_id,omitempty"`
	LogicalCWD                    string                  `json:"logical_cwd,omitempty"`
	SessionID                     SessionID               `json:"session_id"`
	Fingerprint                   string                  `json:"fingerprint,omitempty"`
	RequestFingerprint            string                  `json:"request_fingerprint,omitempty"`
	ExecutionFingerprint          string                  `json:"execution_fingerprint,omitempty"`
	ObservationBindingFingerprint string                  `json:"observation_binding_fingerprint,omitempty"`
	StructuredAdapter             string                  `json:"structured_adapter,omitempty"`
	ExecutionMode                 ExecutionMode           `json:"execution_mode,omitempty"`
	Executable                    string                  `json:"executable,omitempty"`
	Command                       string                  `json:"command,omitempty"`
	Argv                          []string                `json:"argv,omitempty"`
	CWD                           string                  `json:"cwd"`
	TTY                           bool                    `json:"tty"`
	TimeoutMS                     int64                   `json:"timeout_ms"`
	Shell                         string                  `json:"shell"`
	DaemonIncarnation             string                  `json:"daemon_incarnation"`
	ControlReservationBytes       int64                   `json:"control_reservation_bytes"`
	ProjectCommand                *project.CommandBinding `json:"project_command,omitempty"`
	Evidence                      *evidence.Contract      `json:"evidence,omitempty"`
	CreatedAt                     time.Time               `json:"created_at"`
}

func (r Reservation) EffectiveRequestFingerprint() string {
	if r.RequestFingerprint != "" {
		return r.RequestFingerprint
	}
	return r.Fingerprint
}
