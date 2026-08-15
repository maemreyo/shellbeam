// Package process defines bounded current-host process observation contracts.
package process

import "time"

const (
	SchemaVersion          = 1
	MaxDescendants         = 128
	MaxTraversalDepth      = 8
	MaxObservationBytes    = 64 << 10
	MaxObservationDuration = 2 * time.Second
	MaxPortRecords         = 64
	MaxDiagnosticCodes     = 16
)

type TargetKind string
type Quality string
type Relation string
type State string
type PortQuality string

const (
	TargetSession TargetKind = "session"
	TargetPID     TargetKind = "pid"

	QualityComplete    Quality = "complete"
	QualityPartial     Quality = "partial"
	QualityUnavailable Quality = "unavailable"

	RelationShellBeamRoot       Relation = "shellbeam_root"
	RelationShellBeamDescendant Relation = "shellbeam_descendant"
	RelationExternal            Relation = "external"

	StateRunning  State = "running"
	StateSleeping State = "sleeping"
	StateStopped  State = "stopped"
	StateZombie   State = "zombie"
	StateExited   State = "exited"
	StateUnknown  State = "unknown"

	PortComplete    PortQuality = "complete"
	PortUnavailable PortQuality = "unavailable"

	DiagnosticObservationIncomplete = "process_observation_incomplete"
	DiagnosticLimitExceeded         = "process_limit_exceeded"
	DiagnosticPortUnavailable       = "port_observation_unavailable"
	DiagnosticIdentityChanged       = "process_identity_changed"
)

type SessionResolution struct {
	SessionID string `json:"session_id"`
	Known     bool   `json:"known"`
	Current   bool   `json:"current"`
	PID       int    `json:"pid,omitempty"`
	State     string `json:"state,omitempty"`
}

type Target struct {
	Kind      TargetKind `json:"kind"`
	SessionID string     `json:"session_id,omitempty"`
	PID       int        `json:"pid,omitempty"`
}

type Identity struct {
	Value      string    `json:"value"`
	StartTime  time.Time `json:"start_time,omitempty"`
	StartToken string    `json:"start_token,omitempty"`
}

type ArgvView struct {
	ExecutableIdentity string `json:"executable_identity,omitempty"`
	ArgumentCount      int    `json:"argument_count,omitempty"`
	Truncated          bool   `json:"truncated,omitempty"`
}

type ProcessFact struct {
	PID                int       `json:"pid"`
	ParentPID          int       `json:"parent_pid,omitempty"`
	Identity           *Identity `json:"process_identity,omitempty"`
	Relation           Relation  `json:"shellbeam_relation"`
	State              State     `json:"state"`
	StartTime          time.Time `json:"start_time,omitempty"`
	ExecutableIdentity string    `json:"executable_identity,omitempty"`
	ArgvView           *ArgvView `json:"argv_view,omitempty"`
}

type PortObservation struct {
	PID                int         `json:"pid"`
	Protocol           string      `json:"protocol"`
	LocalEndpointClass string      `json:"local_endpoint_class"`
	Port               int         `json:"port"`
	Quality            PortQuality `json:"quality"`
}

type Observation struct {
	SchemaVersion   int               `json:"schema_version"`
	ObservedAt      time.Time         `json:"observed_at"`
	Quality         Quality           `json:"quality"`
	Target          Target            `json:"target"`
	Root            *ProcessFact      `json:"root,omitempty"`
	Descendants     []ProcessFact     `json:"descendants,omitempty"`
	Ports           []PortObservation `json:"ports,omitempty"`
	Truncated       bool              `json:"truncated"`
	DiagnosticCodes []string          `json:"diagnostic_codes,omitempty"`
}
