package receipt

import (
	"fmt"

	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/session"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type OperationState string

const (
	OperationStarting   OperationState = "starting"
	OperationRunning    OperationState = "running"
	OperationFinalizing OperationState = "finalizing"
	OperationTerminal   OperationState = "terminal"
)

type ChildState string

const (
	ChildPending     ChildState = "pending"
	ChildRunning     ChildState = "running"
	ChildExited      ChildState = "exited"
	ChildSpawnFailed ChildState = "spawn_failed"
	ChildUnknown     ChildState = "unknown"
)

type OperationResult struct {
	OperationID string         `json:"operation_id"`
	ActivityID  string         `json:"activity_id,omitempty"`
	WorkspaceID string         `json:"workspace_id,omitempty"`
	SessionID   string         `json:"session_id"`
	State       OperationState `json:"state"`
}

type ChildResult struct {
	State    ChildState      `json:"state"`
	Outcome  session.Outcome `json:"outcome,omitempty"`
	ExitCode *int            `json:"exit_code,omitempty"`
	Signal   string          `json:"signal,omitempty"`
	TimedOut bool            `json:"timed_out"`
}

type OutputResult struct {
	CanonicalStream string          `json:"canonical_stream"`
	Preview         string          `json:"preview,omitempty"`
	RawBytes        int64           `json:"raw_bytes"`
	ReturnedBytes   int64           `json:"returned_bytes"`
	Cursor          int64           `json:"cursor"`
	NextCursor      int64           `json:"next_cursor"`
	Truncated       bool            `json:"truncated"`
	OutputComplete  bool            `json:"output_complete"`
	CaptureQuality  CaptureQuality  `json:"capture_quality,omitempty"`
	CaptureReasons  []CaptureReason `json:"capture_reasons,omitempty"`
}

type Result struct {
	SchemaVersion            int                      `json:"schema_version"`
	SessionMode              string                   `json:"session_mode,omitempty"`
	AuthorityEpoch           delegated.AuthorityEpoch `json:"authority_epoch,omitempty"`
	EvidenceAuthority        string                   `json:"evidence_authority,omitempty"`
	InputAuthorityProvenance string                   `json:"input_authority_provenance,omitempty"`
	Operation                OperationResult          `json:"operation"`
	Child                    *ChildResult             `json:"child,omitempty"`
	Output                   OutputResult             `json:"output"`
	ContextEvents            []workspace.ContextEvent `json:"context_events,omitempty"`
	Advisories               []workspace.Advisory     `json:"advisories,omitempty"`
	Receipt                  *Receipt                 `json:"receipt,omitempty"`
	// Failure is the receipt interpreted: which stage failed, what kind of
	// failure it was, and whether repeating the request could differ. It is
	// derived from the receipt's evidence rather than stored alongside it, so
	// receipts written before this vocabulary existed are classified too.
	Failure *Failure `json:"failure,omitempty"`
}

type ResultInput struct {
	OperationID   string
	ActivityID    string
	WorkspaceID   string
	SessionID     string
	ContextEvents []workspace.ContextEvent
	Advisories    []workspace.Advisory
	State         session.State
	Outcome       session.Outcome
	Preview       string
	RawBytes      int64
	Cursor        int64
	NextCursor    int64
	Truncated     bool
	Receipt       *Receipt
}

func NewResult(in ResultInput) (Result, error) {
	operationState, err := mapOperationState(in.State)
	if err != nil {
		return Result{}, err
	}
	if in.OperationID == "" || in.SessionID == "" {
		return Result{}, fmt.Errorf("structured result identity missing")
	}
	if in.NextCursor < in.Cursor || in.RawBytes < in.NextCursor {
		return Result{}, fmt.Errorf("invalid output accounting")
	}
	result := Result{
		SchemaVersion: 2,
		Operation:     OperationResult{OperationID: in.OperationID, ActivityID: in.ActivityID, WorkspaceID: in.WorkspaceID, SessionID: in.SessionID, State: operationState},
		Output: OutputResult{
			CanonicalStream: "combined",
			Preview:         in.Preview,
			RawBytes:        in.RawBytes,
			ReturnedBytes:   in.NextCursor - in.Cursor,
			Cursor:          in.Cursor,
			NextCursor:      in.NextCursor,
			Truncated:       in.Truncated,
		},
		ContextEvents: append([]workspace.ContextEvent(nil), in.ContextEvents...),
		Advisories:    append([]workspace.Advisory(nil), in.Advisories...),
		Receipt:       in.Receipt,
		Failure:       in.Receipt.failureOf(),
	}
	if in.Receipt != nil {
		if in.Receipt.OperationID != in.OperationID || in.Receipt.SessionID != in.SessionID {
			return Result{}, fmt.Errorf("receipt identity mismatch")
		}
		if err := in.Receipt.Validate(); err != nil {
			return Result{}, err
		}
		result.Output.OutputComplete = in.Receipt.OutputComplete
		if in.Receipt.SchemaVersion == 5 {
			result.SessionMode = in.Receipt.SessionMode
			result.AuthorityEpoch = in.Receipt.AuthorityEpoch
			result.EvidenceAuthority = in.Receipt.EvidenceAuthority
			result.InputAuthorityProvenance = in.Receipt.InputAuthorityProvenance
			result.Output.CaptureQuality = in.Receipt.CaptureQuality
			result.Output.CaptureReasons = append([]CaptureReason(nil), in.Receipt.CaptureReasons...)
		}
		result.Child = childFromReceipt(*in.Receipt)
		return result, nil
	}
	result.Child = childFromLive(in.State, in.Outcome)
	return result, nil
}

func mapOperationState(state session.State) (OperationState, error) {
	switch state {
	case session.Starting:
		return OperationStarting, nil
	case session.Running:
		return OperationRunning, nil
	case session.Finalizing:
		return OperationFinalizing, nil
	case session.Completed, session.Failed, session.TimedOut, session.Killed, session.Abandoned:
		return OperationTerminal, nil
	default:
		return "", fmt.Errorf("unknown session state %q", state)
	}
}

func childFromLive(state session.State, outcome session.Outcome) *ChildResult {
	switch state {
	case session.Starting:
		return &ChildResult{State: ChildPending, Outcome: outcome}
	case session.Running:
		return &ChildResult{State: ChildRunning, Outcome: outcome}
	default:
		return &ChildResult{State: ChildUnknown, Outcome: outcome}
	}
}

func childFromReceipt(rec Receipt) *ChildResult {
	child := &ChildResult{Outcome: rec.Outcome, TimedOut: rec.Outcome == session.Timeout}
	switch {
	case rec.State == session.Abandoned:
		child.State = ChildUnknown
	case rec.Spawn.Attempted && !rec.Spawn.Succeeded:
		child.State = ChildSpawnFailed
	case rec.Exit.Reaped || (rec.SchemaVersion == 5 && rec.Exit.Code != nil):
		child.State = ChildExited
		child.ExitCode = rec.Exit.Code
		child.Signal = rec.Exit.Signal
	default:
		child.State = ChildUnknown
	}
	return child
}
