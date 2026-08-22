package contextexec

import (
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

// PublicOutput is bounded model-visible child output. Truncated names
// execution/capture truncation; PreviewTruncated names only the bounded view
// returned by this call.
type PublicOutput struct {
	Preview          string            `json:"preview,omitempty"`
	StdoutBytes      int64             `json:"stdout_bytes"`
	StderrBytes      int64             `json:"stderr_bytes"`
	RawBytes         int64             `json:"raw_bytes"`
	ReturnedBytes    int64             `json:"returned_bytes"`
	OutputComplete   bool              `json:"output_complete"`
	Truncated        bool              `json:"truncated,omitempty"`
	PreviewTruncated bool              `json:"preview_truncated,omitempty"`
	Attribution      OutputAttribution `json:"attribution"`
}

// PublicState is the bounded model-visible projection of durable context-exec
// state. It deliberately excludes the frozen context expectation, cwd, shell
// identity, helper identity/generation, and request fingerprint.
type PublicState struct {
	SchemaVersion       int                      `json:"schema_version"`
	ContextExecID       string                   `json:"context_exec_id"`
	SessionID           string                   `json:"session_id"`
	AuthorityEpoch      delegated.AuthorityEpoch `json:"authority_epoch"`
	Lifecycle           Lifecycle                `json:"lifecycle"`
	ChildOperationID    string                   `json:"child_operation_id,omitempty"`
	ChildSessionID      string                   `json:"child_session_id,omitempty"`
	FailureCode         string                   `json:"failure_code,omitempty"`
	RequestedExecutable string                   `json:"requested_executable,omitempty"`
	ResolvedExecutable  string                   `json:"resolved_executable,omitempty"`
	Spawn               *receipt.SpawnEvidence   `json:"spawn_evidence,omitempty"`
	Exit                *receipt.ExitEvidence    `json:"exit_evidence,omitempty"`
	Signal              *receipt.SignalEvidence  `json:"signal_evidence,omitempty"`
	TimedOut            *bool                    `json:"timed_out,omitempty"`
	Output              *PublicOutput            `json:"output,omitempty"`
	EvidenceQuality     EvidenceQuality          `json:"evidence_quality,omitempty"`
	EvidenceAuthority   string                   `json:"evidence_authority,omitempty"`
}
