// Package receipt defines evidence required to interpret command outcomes.
package receipt

import (
	"fmt"

	"github.com/maemreyo/shellbeam/internal/core/evidence"
	project "github.com/maemreyo/shellbeam/internal/core/project"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

type SpawnEvidence struct {
	Attempted bool   `json:"attempted"`
	Succeeded bool   `json:"succeeded"`
	ErrorCode string `json:"error_code,omitempty"`
}
type ExitEvidence struct {
	Reaped bool   `json:"reaped"`
	Code   *int   `json:"code,omitempty"`
	Signal string `json:"signal,omitempty"`
}
type SignalEvidence struct {
	Requested string `json:"requested,omitempty"`
	Attempted bool   `json:"attempted"`
	Succeeded bool   `json:"succeeded"`
}

type Receipt struct {
	SchemaVersion                 int                     `json:"schema_version"`
	OperationID                   string                  `json:"operation_id"`
	SessionID                     string                  `json:"session_id"`
	Fingerprint                   string                  `json:"fingerprint,omitempty"`
	RequestFingerprint            string                  `json:"request_fingerprint,omitempty"`
	ExecutionFingerprint          string                  `json:"execution_fingerprint,omitempty"`
	ObservationBindingFingerprint string                  `json:"observation_binding_fingerprint,omitempty"`
	DaemonIncarnation             string                  `json:"daemon_incarnation"`
	ExecutionMode                 string                  `json:"execution_mode,omitempty"`
	Executable                    string                  `json:"executable,omitempty"`
	State                         session.State           `json:"state"`
	Outcome                       session.Outcome         `json:"outcome"`
	Shell                         string                  `json:"shell,omitempty"`
	CWD                           string                  `json:"cwd,omitempty"`
	TTY                           bool                    `json:"tty"`
	TimeoutMS                     int64                   `json:"timeout_ms"`
	OutputBytes                   int64                   `json:"output_bytes"`
	OutputComplete                bool                    `json:"output_complete"`
	InputAcceptedBytes            int64                   `json:"input_accepted_bytes"`
	InputDeliveredBytes           int64                   `json:"input_delivered_bytes"`
	StdinClosed                   bool                    `json:"stdin_closed"`
	FailureReason                 string                  `json:"failure_reason,omitempty"`
	WorkspaceProvenance           *WorkspaceProvenance    `json:"workspace_provenance,omitempty"`
	ProjectCommand                *project.CommandBinding `json:"project_command,omitempty"`
	Evidence                      *evidence.Contract      `json:"evidence,omitempty"`
	Spawn                         SpawnEvidence           `json:"spawn_evidence"`
	Exit                          ExitEvidence            `json:"exit_evidence"`
	Signal                        SignalEvidence          `json:"signal_evidence"`
}

func (r Receipt) Validate() error {
	switch r.SchemaVersion {
	case 1:
		if r.ProjectCommand != nil || r.Evidence != nil {
			return fmt.Errorf("derived provenance requires newer receipt schema")
		}
	case 2:
		if r.RequestFingerprint == "" || r.ExecutionFingerprint == "" {
			return fmt.Errorf("v2 receipt fingerprints missing")
		}
		if r.ProjectCommand != nil {
			return fmt.Errorf("project command provenance requires v3 receipt")
		}
		if r.Evidence != nil {
			if err := r.Evidence.Validate(); err != nil {
				return fmt.Errorf("invalid evidence provenance: %w", err)
			}
		}
	case 3:
		if r.Evidence != nil {
			return fmt.Errorf("v3 receipt cannot carry raw evidence provenance")
		}
		if r.RequestFingerprint == "" || r.ExecutionFingerprint == "" || r.ProjectCommand == nil {
			return fmt.Errorf("v3 receipt project command provenance missing")
		}
		if err := r.ProjectCommand.Validate(); err != nil {
			return fmt.Errorf("invalid v3 project command provenance: %w", err)
		}
	default:
		return fmt.Errorf("unsupported receipt schema")
	}
	if r.InputDeliveredBytes > r.InputAcceptedBytes {
		return fmt.Errorf("delivered input exceeds accepted")
	}
	if r.State == session.Completed && r.Outcome == session.Success {
		if !r.Spawn.Attempted || !r.Spawn.Succeeded || !r.Exit.Reaped || r.Exit.Code == nil || *r.Exit.Code != 0 || !r.OutputComplete || r.InputAcceptedBytes != r.InputDeliveredBytes {
			return fmt.Errorf("success lacks complete evidence")
		}
	}
	if r.State == session.Abandoned && r.Outcome != session.Ambiguous {
		return fmt.Errorf("abandoned must be ambiguous")
	}
	if r.State.Terminal() && r.Outcome == session.NoOutcome {
		return fmt.Errorf("terminal outcome missing")
	}
	if r.WorkspaceProvenance != nil {
		if r.SchemaVersion < 2 {
			return fmt.Errorf("workspace provenance requires v2 receipt")
		}
		if err := r.WorkspaceProvenance.Validate(); err != nil {
			return err
		}
	}
	return nil
}
