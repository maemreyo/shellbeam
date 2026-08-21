package daemon

import (
	"context"
	traceapp "github.com/maemreyo/shellbeam/internal/app/inputtrace"
	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	"github.com/maemreyo/shellbeam/internal/core/evidence"
	hermeticcore "github.com/maemreyo/shellbeam/internal/core/hermetic"
	trace "github.com/maemreyo/shellbeam/internal/core/inputtrace"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
	"time"
)

type Options struct {
	// DefaultTimeoutMS bounds an ordinary command whose caller named no
	// timeout. Zero leaves such commands unbounded, which is the behaviour that
	// let stuck sessions hold capacity indefinitely.
	DefaultTimeoutMS          int64
	MaxTimeoutMS              int64
	Incarnation               string
	Shell                     string
	MaxQueuedInputBytes       int
	TerminationGrace          time.Duration
	Capabilities              capability.Catalog
	StructuredWorker          StructuredWorker
	StructuredCapturePreparer StructuredCapturePreparer
	StructuredCaptureTerminal StructuredCaptureTerminal
	TelemetryWorker           TelemetryWorker
	EvidenceWorker            EvidenceWorker
	ProjectCommandBinder      ProjectCommandBinder
	PersistentRuntime         PersistentRuntime
	MediaReader               MediaReader
	MediaReadBudget           time.Duration
	InputTracePreparer        traceapp.Preparer
	InputTraceWorker          InputTraceWorker
	HermeticRuntime           HermeticRuntime
}
type StartRequest struct {
	ProtocolVersion     int                                 `json:"-"`
	OperationID         string                              `json:"operation_id"`
	ActivityID          string                              `json:"activity_id,omitempty"`
	ExperimentID        string                              `json:"experiment_id,omitempty"`
	WorkspaceID         string                              `json:"workspace_id,omitempty"`
	WorkspaceHint       *workspace.Hint                     `json:"workspace_hint,omitempty"`
	Command             string                              `json:"command,omitempty"`
	Argv                []string                            `json:"argv,omitempty"`
	Intent              *operation.DeclaredIntent           `json:"intent,omitempty"`
	CWD                 string                              `json:"cwd"`
	TTY                 bool                                `json:"tty"`
	TimeoutMS           int64                               `json:"timeout_ms"`
	StdinMode           operation.StdinMode                 `json:"stdin_mode,omitempty"`
	TimeoutMode         operation.TimeoutMode               `json:"timeout_mode,omitempty"`
	TraceMode           trace.Mode                          `json:"trace_mode,omitempty"`
	ResourceLimits      *operation.ResourceLimits           `json:"limits,omitempty"`
	Hermetic            *hermeticcore.Request               `json:"hermetic,omitempty"`
	Persistent          bool                                `json:"persistent,omitempty"`
	SessionName         string                              `json:"session_name,omitempty"`
	YieldMS             int64                               `json:"yield_time_ms"`
	MaxOutputBytes      int                                 `json:"max_output_bytes"`
	StructuredAdapter   string                              `json:"structured_adapter,omitempty"`
	Evidence            *evidence.Contract                  `json:"evidence,omitempty"`
	VerificationAttempt *evidence.VerificationAttemptIntent `json:"verification_attempt,omitempty"`
	ProjectCommandID    string                              `json:"project_command_id,omitempty"`
	Params              map[string]string                   `json:"params,omitempty"`
}

type StructuredWorker interface {
	// ScheduleTerminal must be bounded and non-blocking with respect to parser execution.
	ScheduleTerminal(context.Context, receipt.Receipt, string) error
}

type StructuredCapturePrepareRequest struct {
	OperationID       operation.ID
	SessionID         operation.SessionID
	WorkspaceID       string
	StructuredAdapter string
	Argv              []string
	CWD               string
	ExecutionMode     operation.ExecutionMode
	Executable        string
}

type StructuredCapturePreparation struct {
	AdapterID     string
	CaptureDigest string
	Owned         bool
}

type StructuredCapturePreparer interface {
	PrepareStructuredCapture(context.Context, StructuredCapturePrepareRequest) (StructuredCapturePreparation, error)
	AbortStructuredCapture(context.Context, operation.ID, operation.SessionID) error
}

type StructuredCaptureTerminal interface {
	AcquireTerminal(context.Context, operation.Reservation) structuredapp.TerminalCaptureResult
	ScheduleTerminal(context.Context, receipt.Receipt, structuredapp.TerminalCaptureResult) error
}
type TelemetryWorker interface {
	// ScheduleTerminal must be bounded and non-blocking with respect to telemetry derivation.
	ScheduleTerminal(context.Context, receipt.Receipt) error
}
type TelemetryResourceWorker interface {
	ScheduleTerminalWithResources(context.Context, receipt.Receipt, *receipt.ResourceEvidence) error
}
type EvidenceWorker interface {
	// ScheduleTerminal must be bounded and non-blocking with respect to evidence derivation.
	ScheduleTerminal(context.Context, receipt.Receipt) error
}
type InputTraceWorker interface {
	ScheduleTerminal(context.Context, receipt.Receipt) error
}

type PollRequest struct {
	SessionID      string `json:"session_id"`
	Cursor         int64  `json:"cursor"`
	YieldMS        int64  `json:"yield_time_ms"`
	MaxOutputBytes int    `json:"max_output_bytes"`
}
type WriteRequest struct {
	SessionID   string `json:"session_id"`
	InputOffset int64  `json:"input_offset"`
	Chars       string `json:"chars,omitempty"`
	EOF         bool   `json:"eof,omitempty"`
}
type KillRequest struct {
	SessionID string `json:"session_id"`
	KillID    string `json:"kill_id"`
	Signal    string `json:"signal"`
}
type View struct {
	OperationID        string                   `json:"operation_id,omitempty"`
	ActivityID         string                   `json:"activity_id,omitempty"`
	WorkspaceID        string                   `json:"workspace_id,omitempty"`
	ContextEvents      []workspace.ContextEvent `json:"context_events,omitempty"`
	Advisories         []workspace.Advisory     `json:"advisories,omitempty"`
	SessionID          string                   `json:"session_id"`
	State              session.State            `json:"state"`
	Outcome            session.Outcome          `json:"outcome"`
	Output             string                   `json:"output,omitempty"`
	Cursor             int64                    `json:"cursor"`
	NextCursor         int64                    `json:"next_cursor"`
	Truncated          bool                     `json:"truncated"`
	AcceptedInputBytes int                      `json:"accepted_input_bytes,omitempty"`
	NextInputOffset    int64                    `json:"next_input_offset,omitempty"`
	EOFQueued          bool                     `json:"eof_queued,omitempty"`
	KillID             string                   `json:"kill_id,omitempty"`
	Signal             string                   `json:"signal,omitempty"`
	SignalAttempt      receipt.SignalEvidence   `json:"signal_attempt,omitempty"`
	Receipt            *receipt.Receipt         `json:"receipt,omitempty"`
	// Failure is the receipt's evidence interpreted for the caller.
	Failure        *receipt.Failure `json:"failure,omitempty"`
	RawOutputBytes int64            `json:"-"`
}

type ServerInfo struct {
	Capabilities capability.Catalog `json:"capabilities"`
}

func (v View) StructuredResult() (receipt.Result, error) {
	return receipt.NewResult(receipt.ResultInput{
		OperationID: v.OperationID, ActivityID: v.ActivityID, WorkspaceID: v.WorkspaceID, SessionID: v.SessionID, ContextEvents: v.ContextEvents, Advisories: v.Advisories, State: v.State, Outcome: v.Outcome,
		Preview: v.Output, RawBytes: v.RawOutputBytes, Cursor: v.Cursor, NextCursor: v.NextCursor,
		Truncated: v.Truncated, Receipt: v.Receipt,
	})
}
