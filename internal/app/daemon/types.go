package daemon

import (
	"github.com/maemreyo/shellbeam/internal/core/capability"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
	"time"
)

type Options struct {
	Incarnation         string
	Shell               string
	MaxQueuedInputBytes int
	TerminationGrace    time.Duration
	Capabilities        capability.Catalog
}
type StartRequest struct {
	OperationID    string `json:"operation_id"`
	Command        string `json:"command"`
	CWD            string `json:"cwd"`
	TTY            bool   `json:"tty"`
	TimeoutMS      int64  `json:"timeout_ms"`
	YieldMS        int64  `json:"yield_time_ms"`
	MaxOutputBytes int    `json:"max_output_bytes"`
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
	OperationID        string                 `json:"operation_id,omitempty"`
	SessionID          string                 `json:"session_id"`
	State              session.State          `json:"state"`
	Outcome            session.Outcome        `json:"outcome"`
	Output             string                 `json:"output,omitempty"`
	Cursor             int64                  `json:"cursor"`
	NextCursor         int64                  `json:"next_cursor"`
	Truncated          bool                   `json:"truncated"`
	AcceptedInputBytes int                    `json:"accepted_input_bytes,omitempty"`
	NextInputOffset    int64                  `json:"next_input_offset,omitempty"`
	EOFQueued          bool                   `json:"eof_queued,omitempty"`
	KillID             string                 `json:"kill_id,omitempty"`
	Signal             string                 `json:"signal,omitempty"`
	SignalAttempt      receipt.SignalEvidence `json:"signal_attempt,omitempty"`
	Receipt            *receipt.Receipt       `json:"receipt,omitempty"`
}

type ServerInfo struct {
	Capabilities capability.Catalog `json:"capabilities"`
}
