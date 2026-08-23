// Package browserbridge defines the Browser Bridge Protocol v1 wire contract.
//
// The protocol is deliberately not a ShellBeam action passthrough. A caller
// names one verb from a closed enum plus at most one correlation id, and the
// host maps that verb to a fixed read plan whose every other identifier is
// derived from the activity record. See the design sections 19 and 20.
package browserbridge

import (
	"fmt"
	"time"
	"unicode/utf8"
)

const ProtocolVersion = 1

const (
	TargetResponseBytes         = 65536
	MaxResponseBytes            = 262144
	MaxActivityEvents           = 64
	MaxStructuredOperations     = 8
	MaxFailingCasesPerOperation = 16
	MaxVerificationWorkspaces   = 4
	MaxCorrelationIDBytes       = 128
	MaxCursorBytes              = 512
)

func SupportedVersions() []int { return []int{ProtocolVersion} }

type Verb string

const (
	VerbHello                  Verb = "hello"
	VerbActivityFacts          Verb = "activity_facts"
	VerbActivityEvents         Verb = "activity_events"
	VerbVerificationFacts      Verb = "verification_facts"
	VerbStructuredFailureFacts Verb = "structured_failure_facts"
)

type Status string

const (
	StatusOK                   Status = "ok"
	StatusFactsUnavailable     Status = "facts_unavailable"
	StatusDaemonUnreachable    Status = "daemon_unreachable"
	StatusProtocolIncompatible Status = "protocol_incompatible"
	StatusInvalidRequest       Status = "invalid_request"
)

type Request struct {
	ProtocolVersion int    `json:"protocol_version"`
	Verb            Verb   `json:"verb"`
	CorrelationID   string `json:"correlation_id,omitempty"`
	Cursor          string `json:"cursor,omitempty"`
}

func (r Request) Validate() error {
	if r.ProtocolVersion != ProtocolVersion {
		return fmt.Errorf("unsupported protocol version")
	}
	switch r.Verb {
	case VerbHello:
		if r.CorrelationID != "" || r.Cursor != "" {
			return fmt.Errorf("hello takes no selectors")
		}
		return nil
	case VerbActivityFacts, VerbVerificationFacts, VerbStructuredFailureFacts:
		if r.Cursor != "" {
			return fmt.Errorf("verb does not accept a cursor")
		}
	case VerbActivityEvents:
		if len(r.Cursor) > MaxCursorBytes || !utf8.ValidString(r.Cursor) {
			return fmt.Errorf("invalid cursor")
		}
	default:
		return fmt.Errorf("unknown verb")
	}
	if r.CorrelationID == "" || len(r.CorrelationID) > MaxCorrelationIDBytes || !utf8.ValidString(r.CorrelationID) {
		return fmt.Errorf("invalid correlation_id")
	}
	return nil
}

type Coverage struct {
	HistoricalOperations string `json:"historical_operations,omitempty"`
	CompactedOperations  int    `json:"compacted_operations"`
	Truncated            bool   `json:"truncated"`
	TruncationReason     string `json:"truncation_reason,omitempty"`
}

type SessionFacts struct {
	Provisioning int `json:"provisioning"`
	Live         int `json:"live"`
	Terminal     int `json:"terminal"`
	Lost         int `json:"lost"`
}

type ActivityFacts struct {
	Found              bool         `json:"found"`
	LatestOperationAt  *time.Time   `json:"latest_operation_at,omitempty"`
	OperationsRetained int          `json:"operations_retained"`
	WorkspaceIDs       []string     `json:"workspace_ids,omitempty"`
	Sessions           SessionFacts `json:"sessions"`
	SessionsTruncated  bool         `json:"sessions_truncated,omitempty"`
}

type EventFacts struct {
	Returned int        `json:"returned"`
	Kinds    []string   `json:"kinds,omitempty"`
	Cursor   string     `json:"cursor,omitempty"`
	LatestAt *time.Time `json:"latest_at,omitempty"`
}

type WorkspaceVerification struct {
	WorkspaceID      string `json:"workspace_id"`
	PolicyState      string `json:"policy_state"`
	GateStatus       string `json:"gate_status"`
	SourceGeneration string `json:"source_generation,omitempty"`
	Satisfied        int    `json:"satisfied"`
	Waived           int    `json:"waived"`
	Blocking         int    `json:"blocking"`
	Indeterminate    int    `json:"indeterminate"`
}

type FailingCase struct {
	Name    string `json:"name"`
	Package string `json:"package,omitempty"`
	Status  string `json:"status"`
}

type OperationFailureFacts struct {
	OperationID      string        `json:"operation_id"`
	AdapterID        string        `json:"adapter_id,omitempty"`
	AdapterVersion   int           `json:"adapter_version,omitempty"`
	Authority        string        `json:"authority,omitempty"`
	DerivationMethod string        `json:"derivation_method,omitempty"`
	Completeness     string        `json:"completeness,omitempty"`
	TestPassed       int           `json:"test_passed"`
	TestFailed       int           `json:"test_failed"`
	Errors           int           `json:"errors"`
	FailingCases     []FailingCase `json:"failing_cases,omitempty"`
	CasesTruncated   bool          `json:"cases_truncated,omitempty"`
}

type Response struct {
	ProtocolVersion   int                     `json:"protocol_version"`
	SupportedVersions []int                   `json:"supported_versions"`
	Verb              Verb                    `json:"verb"`
	Status            Status                  `json:"status"`
	Reason            string                  `json:"reason,omitempty"`
	Activity          *ActivityFacts          `json:"activity,omitempty"`
	Events            *EventFacts             `json:"events,omitempty"`
	Verification      []WorkspaceVerification `json:"verification,omitempty"`
	Structured        []OperationFailureFacts `json:"structured,omitempty"`
	Coverage          Coverage                `json:"coverage"`
}
