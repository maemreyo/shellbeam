package operation

import (
	environment "github.com/maemreyo/shellbeam/internal/core/environment"
	hermetic "github.com/maemreyo/shellbeam/internal/core/hermetic"
	trace "github.com/maemreyo/shellbeam/internal/core/inputtrace"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/evidence"
	project "github.com/maemreyo/shellbeam/internal/core/project"
)

type Reservation struct {
	SchemaVersion                 int                           `json:"schema_version"`
	OperationID                   ID                            `json:"operation_id"`
	ActivityID                    string                        `json:"activity_id,omitempty"`
	WorkspaceID                   string                        `json:"workspace_id,omitempty"`
	LogicalCWD                    string                        `json:"logical_cwd,omitempty"`
	SessionID                     SessionID                     `json:"session_id"`
	Fingerprint                   string                        `json:"fingerprint,omitempty"`
	RequestFingerprint            string                        `json:"request_fingerprint,omitempty"`
	ExecutionFingerprint          string                        `json:"execution_fingerprint,omitempty"`
	ObservationBindingFingerprint string                        `json:"observation_binding_fingerprint,omitempty"`
	StructuredAdapter             string                        `json:"structured_adapter,omitempty"`
	ExecutionMode                 ExecutionMode                 `json:"execution_mode,omitempty"`
	Executable                    string                        `json:"executable,omitempty"`
	Command                       string                        `json:"command,omitempty"`
	Argv                          []string                      `json:"argv,omitempty"`
	CWD                           string                        `json:"cwd"`
	TTY                           bool                          `json:"tty"`
	TimeoutMS                     int64                         `json:"timeout_ms"`
	Persistent                    bool                          `json:"persistent,omitempty"`
	SessionName                   string                        `json:"session_name,omitempty"`
	Shell                         string                        `json:"shell"`
	DaemonIncarnation             string                        `json:"daemon_incarnation"`
	ControlReservationBytes       int64                         `json:"control_reservation_bytes"`
	ProjectCommand                *project.CommandBinding       `json:"project_command,omitempty"`
	Intent                        *DeclaredIntent               `json:"intent,omitempty"`
	Evidence                      *evidence.Contract            `json:"evidence,omitempty"`
	CreatedAt                     time.Time                     `json:"created_at"`
	EnvironmentBinding            *environment.Binding          `json:"environment_binding,omitempty"`
	Trace                         *trace.InstrumentationBinding `json:"input_trace,omitempty"`
	ResourceLimits                *ResourceLimits               `json:"resource_limits,omitempty"`
	HermeticBoundary              *hermetic.BoundaryBinding     `json:"hermetic_boundary,omitempty"`
}

func (r Reservation) EffectiveRequestFingerprint() string {
	if r.RequestFingerprint != "" {
		return r.RequestFingerprint
	}
	return r.Fingerprint
}
