package persistentsession

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	SchemaVersion   = 1
	ProtocolVersion = 1

	MaxSessionNameBytes         = 128
	DefaultInspectRows          = 25
	MaxInspectRows              = 100
	MaxInputRecords             = 4096
	MaxInputRecordMetadataBytes = 1 << 20
	MaxKillRecords              = 256
	ReattachHandshakeTimeoutMS  = 2000
	StartupReattachConcurrency  = 16
	StartupReattachBudgetMS     = 5000
)

type Lifecycle string

const (
	LifecycleProvisioning Lifecycle = "provisioning"
	LifecycleLive         Lifecycle = "live"
	LifecycleTerminal     Lifecycle = "terminal"
	LifecycleLost         Lifecycle = "lost"
)

type OwnershipStatus string

const (
	OwnershipCurrent    OwnershipStatus = "current"
	OwnershipReattached OwnershipStatus = "reattached"
	OwnershipTerminal   OwnershipStatus = "terminal"
	OwnershipLost       OwnershipStatus = "lost"
)

const (
	SupervisionPerSession   = "per_session"
	ContinuityDaemonRestart = "daemon_restart"
)

type Binding struct {
	SchemaVersion          int       `json:"schema_version"`
	SessionID              string    `json:"session_id"`
	OperationID            string    `json:"operation_id"`
	ActivityID             string    `json:"activity_id,omitempty"`
	WorkspaceID            string    `json:"workspace_id,omitempty"`
	SessionName            string    `json:"session_name,omitempty"`
	Persistent             bool      `json:"persistent"`
	Supervision            string    `json:"supervision"`
	Continuity             string    `json:"continuity"`
	SupervisorGenerationID string    `json:"supervisor_generation_id"`
	SupervisorEndpointRef  string    `json:"supervisor_endpoint_ref"`
	Lifecycle              Lifecycle `json:"lifecycle"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

type InspectRequest struct {
	SessionName    string `json:"session_name,omitempty"`
	ActivityID     string `json:"activity_id,omitempty"`
	WorkspaceID    string `json:"workspace_id,omitempty"`
	State          string `json:"state,omitempty"`
	PersistentOnly *bool  `json:"persistent_only,omitempty"`
	Limit          int    `json:"max_records,omitempty"`
	Cursor         string `json:"continuation,omitempty"`
}

type Summary struct {
	SessionID           string          `json:"session_id"`
	SessionName         string          `json:"session_name,omitempty"`
	OperationID         string          `json:"operation_id"`
	ActivityID          string          `json:"activity_id,omitempty"`
	WorkspaceID         string          `json:"workspace_id,omitempty"`
	State               string          `json:"state"`
	Outcome             string          `json:"outcome,omitempty"`
	Persistent          bool            `json:"persistent"`
	OwnershipStatus     OwnershipStatus `json:"ownership_status"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
	OutputBytes         int64           `json:"output_bytes"`
	InputAcceptedBytes  int64           `json:"input_accepted_bytes,omitempty"`
	InputDeliveredBytes int64           `json:"input_delivered_bytes,omitempty"`
}

type InspectPage struct {
	Sessions     []Summary `json:"sessions"`
	Continuation string    `json:"continuation,omitempty"`
}

type BindingPage struct {
	Bindings     []Binding `json:"bindings"`
	Continuation string    `json:"continuation,omitempty"`
}

func ValidateSessionName(v string) error {
	if !utf8.ValidString(v) || len(v) < 1 || len(v) > MaxSessionNameBytes {
		return fmt.Errorf("invalid persistent session name")
	}
	if strings.TrimFunc(v, unicode.IsSpace) != v || strings.ContainsAny(v, "/\\") {
		return fmt.Errorf("invalid persistent session name")
	}
	for _, r := range v {
		if unicode.IsControl(r) {
			return fmt.Errorf("invalid persistent session name")
		}
	}
	return nil
}

func (v Binding) Validate() error {
	if v.SchemaVersion != SchemaVersion || !v.Persistent || v.Supervision != SupervisionPerSession || v.Continuity != ContinuityDaemonRestart {
		return fmt.Errorf("invalid persistent session binding")
	}
	if !validOpaque(v.SessionID) || !validOpaque(v.OperationID) || !validOpaque(v.SupervisorGenerationID) || !validOpaque(v.SupervisorEndpointRef) {
		return fmt.Errorf("invalid persistent session binding")
	}
	if v.SessionName != "" {
		if err := ValidateSessionName(v.SessionName); err != nil {
			return err
		}
	}
	switch v.Lifecycle {
	case LifecycleProvisioning, LifecycleLive, LifecycleTerminal, LifecycleLost:
	default:
		return fmt.Errorf("invalid persistent session lifecycle")
	}
	if v.CreatedAt.IsZero() || v.UpdatedAt.IsZero() || v.UpdatedAt.Before(v.CreatedAt) {
		return fmt.Errorf("invalid persistent session timestamps")
	}
	return nil
}

func validOpaque(v string) bool {
	if len(v) < 1 || len(v) > 128 {
		return false
	}
	for i := 0; i < len(v); i++ {
		b := v[i]
		if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_' || b == '-' || b == '.' {
			continue
		}
		return false
	}
	return true
}
