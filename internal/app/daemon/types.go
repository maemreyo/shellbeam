package daemon

import (
	"context"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	"github.com/maemreyo/shellbeam/internal/core/evidence"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
	"time"
)

type Options struct {
	Incarnation          string
	Shell                string
	MaxQueuedInputBytes  int
	TerminationGrace     time.Duration
	Capabilities         capability.Catalog
	StructuredWorker     StructuredWorker
	TelemetryWorker      TelemetryWorker
	ProjectCommandBinder ProjectCommandBinder
}
type StartRequest struct {
	ProtocolVersion   int                       `json:"-"`
	OperationID       string                    `json:"operation_id"`
	ActivityID        string                    `json:"activity_id,omitempty"`
	WorkspaceID       string                    `json:"workspace_id,omitempty"`
	WorkspaceHint     *workspace.Hint           `json:"workspace_hint,omitempty"`
	Command           string                    `json:"command,omitempty"`
	Argv              []string                  `json:"argv,omitempty"`
	Intent            *operation.DeclaredIntent `json:"intent,omitempty"`
	CWD               string                    `json:"cwd"`
	TTY               bool                      `json:"tty"`
	TimeoutMS         int64                     `json:"timeout_ms"`
	YieldMS           int64                     `json:"yield_time_ms"`
	MaxOutputBytes    int                       `json:"max_output_bytes"`
	StructuredAdapter string                    `json:"structured_adapter,omitempty"`
	Evidence          *evidence.Contract        `json:"evidence,omitempty"`
	ProjectCommandID  string                    `json:"project_command_id,omitempty"`
	Params            map[string]string         `json:"params,omitempty"`
}

type StructuredWorker interface {
	// ScheduleTerminal must be bounded and non-blocking with respect to parser execution.
	ScheduleTerminal(context.Context, receipt.Receipt, string) error
}
type TelemetryWorker interface {
	// ScheduleTerminal must be bounded and non-blocking with respect to telemetry derivation.
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
	RawOutputBytes     int64                    `json:"-"`
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
