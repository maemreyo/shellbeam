package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	observationapp "github.com/maemreyo/shellbeam/internal/app/observation"
	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	telemetryapp "github.com/maemreyo/shellbeam/internal/app/telemetry"
	activity "github.com/maemreyo/shellbeam/internal/core/activity"
	codeintel "github.com/maemreyo/shellbeam/internal/core/codeintel"
	observationcore "github.com/maemreyo/shellbeam/internal/core/observation"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	reprocore "github.com/maemreyo/shellbeam/internal/core/repro"
	structuredcore "github.com/maemreyo/shellbeam/internal/core/structuredresult"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type input struct {
	Action            string                    `json:"action"`
	OperationID       string                    `json:"operation_id,omitempty"`
	WorkspaceID       string                    `json:"workspace_id,omitempty"`
	ActivityID        string                    `json:"activity_id,omitempty"`
	CodeQuery         *codeintel.Query          `json:"code_query,omitempty"`
	WorkspaceHint     *workspace.Hint           `json:"workspace_hint,omitempty"`
	StructuredAdapter string                    `json:"structured_adapter,omitempty"`
	Command           string                    `json:"command,omitempty"`
	Argv              []string                  `json:"argv,omitempty"`
	Intent            *operation.DeclaredIntent `json:"intent,omitempty"`
	CWD               string                    `json:"cwd,omitempty"`
	TTY               bool                      `json:"tty,omitempty"`
	YieldMS           int64                     `json:"yield_time_ms,omitempty"`
	TimeoutMS         int64                     `json:"timeout_ms,omitempty"`
	MaxOutputBytes    int                       `json:"max_output_bytes,omitempty"`
	SessionID         string                    `json:"session_id,omitempty"`
	Cursor            int64                     `json:"cursor,omitempty"`
	InputOffset       int64                     `json:"input_offset,omitempty"`
	Chars             string                    `json:"chars,omitempty"`
	EOF               bool                      `json:"eof,omitempty"`
	KillID            string                    `json:"kill_id,omitempty"`
	Signal            string                    `json:"signal,omitempty"`
	Target            *observationcore.Target   `json:"target,omitempty"`
	AfterEventCursor  string                    `json:"after_event_cursor,omitempty"`
	MaxEvents         int                       `json:"max_events,omitempty"`
	RecordKind        structuredcore.RecordKind `json:"record_kind,omitempty"`
	Severity          structuredcore.Severity   `json:"severity,omitempty"`
	Path              string                    `json:"path,omitempty"`
	TestStatus        structuredcore.TestStatus `json:"test_status,omitempty"`
	Continuation      string                    `json:"continuation,omitempty"`
	MaxRecords        int                       `json:"max_records,omitempty"`
	MaxSamples        int                       `json:"max_samples,omitempty"`
	ReproCreateID     string                    `json:"repro_create_id,omitempty"`
	CapturePolicy     *reprocore.CapturePolicy  `json:"capture_policy,omitempty"`
	ReproID           string                    `json:"repro_id,omitempty"`
}

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }

func hasField(raw []byte, key string) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return false
	}
	_, ok := fields[key]
	return ok
}

func protocolGeneration(version string) int {
	if version >= modernMCP {
		return 2
	}
	return 1
}

func validateForVersion(version int, v input, raw []byte) error {
	if version == 2 {
		if err := validateV2FieldSet(v.Action, raw); err != nil {
			return err
		}
		return validateV2(v)
	}
	if v.Action == "inspect.server" {
		if err := validateV2FieldSet(v.Action, raw); err != nil {
			return err
		}
	}
	return validateV1(v)
}

func validateV2(v input) error {
	switch v.Action {
	case "inspect.project", "inspect.workspace", "inspect.readiness":
		_, err := workspace.ParseWorkspaceID(v.WorkspaceID)
		return err
	case "inspect.activity":
		_, err := activity.ParseID(v.ActivityID)
		return err
	case "inspect.code":
		if _, err := workspace.ParseWorkspaceID(v.WorkspaceID); err != nil {
			return err
		}
		if v.ActivityID != "" {
			if _, err := activity.ParseID(v.ActivityID); err != nil {
				return err
			}
		}
		if v.CodeQuery == nil {
			return fmt.Errorf("inspect.code requires code_query")
		}
		return v.CodeQuery.Validate()
	case "inspect.structured":
		return (structuredapp.InspectRequest{OperationID: v.OperationID, Filter: structuredapp.RecordFilter{RecordKind: v.RecordKind, Severity: v.Severity, Path: v.Path, TestStatus: v.TestStatus}, Continuation: v.Continuation, MaxRecords: v.MaxRecords}).Validate()
	case "inspect.telemetry", "repro.create", "inspect.repro":
		return validateA4Input(v)
	case "inspect.events":
		if v.Target == nil {
			return fmt.Errorf("inspect.events requires target")
		}
		if err := v.Target.Validate(); err != nil {
			return err
		}
		if v.MaxEvents < 1 || v.MaxEvents > observationapp.MaxInspectEvents {
			return fmt.Errorf("invalid max_events")
		}
		if v.AfterEventCursor != "" && (!strings.HasPrefix(v.AfterEventCursor, observationapp.EventCursorPrefix) || len(v.AfterEventCursor) > observationapp.MaxEventCursorBytes) {
			return fmt.Errorf("invalid event cursor")
		}
		return nil
	}
	if v.Action != "start" {
		return validateV1(v)
	}
	if v.OperationID == "" {
		return fmt.Errorf("start requires operation_id")
	}
	if _, err := (operation.Intent{Command: v.Command, Argv: v.Argv}).ExecutionMode(); err != nil {
		return err
	}
	if v.Intent != nil {
		if err := v.Intent.Validate(); err != nil {
			return err
		}
	}
	if _, err := operation.ParseID(v.OperationID); err != nil {
		return err
	}
	address := workspace.Address{WorkspaceID: workspace.WorkspaceID(v.WorkspaceID), CWD: v.CWD}
	if err := address.Validate(); err != nil {
		return err
	}
	if v.WorkspaceHint != nil {
		if err := v.WorkspaceHint.Validate(); err != nil {
			return err
		}
	}
	if v.ActivityID != "" {
		if _, err := activity.ParseID(v.ActivityID); err != nil {
			return err
		}
	}
	if v.StructuredAdapter != "" && !operation.ValidStructuredAdapterID(v.StructuredAdapter) {
		return fmt.Errorf("invalid structured adapter")
	}
	return validateNonNegative(v)
}

func validateA4Input(v input) error {
	switch v.Action {
	case "inspect.telemetry":
		if _, err := operation.ParseID(v.OperationID); err != nil {
			return err
		}
		if v.MaxSamples < 1 || v.MaxSamples > telemetryapp.MaxInspectSamples {
			return fmt.Errorf("invalid max_samples")
		}
	case "repro.create":
		policy := reprocore.CapturePolicy{DependentDerivations: reprocore.CaptureCurrent}
		if v.CapturePolicy != nil {
			policy = *v.CapturePolicy
		}
		return (reprocore.CreateRequest{CreateID: v.ReproCreateID, OperationID: v.OperationID, Policy: policy}).Validate()
	case "inspect.repro":
		if !validReproIDInput(v.ReproID) {
			return fmt.Errorf("invalid repro_id")
		}
	}
	return nil
}

func validateV1(v input) error {
	switch v.Action {
	case "start":
		if v.OperationID == "" || v.Command == "" || len(v.CWD) == 0 || v.CWD[0] != '/' {
			return fmt.Errorf("start requires operation_id, command, and absolute cwd")
		}
		if _, err := operation.ParseID(v.OperationID); err != nil {
			return err
		}
		if v.SessionID != "" || v.Chars != "" || v.EOF || v.KillID != "" {
			return fmt.Errorf("cross-action field")
		}
	case "poll":
		if v.SessionID == "" || v.OperationID != "" || v.Command != "" || v.Chars != "" || v.KillID != "" {
			return fmt.Errorf("invalid poll fields")
		}
		if _, err := operation.ParseSessionID(v.SessionID); err != nil {
			return err
		}
	case "write":
		if v.SessionID == "" || v.InputOffset < 0 || (v.Chars == "") == (!v.EOF) {
			return fmt.Errorf("write requires exactly chars or eof")
		}
		if _, err := operation.ParseSessionID(v.SessionID); err != nil {
			return err
		}
		if v.OperationID != "" || v.Command != "" || v.KillID != "" {
			return fmt.Errorf("cross-action field")
		}
	case "inspect.server":
		return validateNonNegative(v)
	case "kill":
		if v.SessionID == "" || v.KillID == "" {
			return fmt.Errorf("kill requires session_id and kill_id")
		}
		if _, err := operation.ParseSessionID(v.SessionID); err != nil {
			return err
		}
		if _, err := operation.ParseID(v.KillID); err != nil {
			return err
		}
		if v.Signal != "" && v.Signal != "INT" && v.Signal != "TERM" && v.Signal != "KILL" {
			return fmt.Errorf("invalid signal")
		}
		if v.OperationID != "" || v.Command != "" || v.Chars != "" || v.EOF {
			return fmt.Errorf("cross-action field")
		}
	default:
		return fmt.Errorf("unknown action")
	}
	return validateNonNegative(v)
}

func validateNonNegative(v input) error {
	if v.YieldMS < 0 || v.TimeoutMS < 0 || v.MaxOutputBytes < 0 || v.Cursor < 0 || v.InputOffset < 0 {
		return fmt.Errorf("negative value")
	}
	return nil
}

func validateV2FieldSet(action string, raw []byte) error {
	allowed := map[string]bool{"action": true}
	for _, field := range v2ActionFields(action) {
		allowed[field] = true
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	for field := range fields {
		if !allowed[field] {
			return fmt.Errorf("cross-action field %q", field)
		}
	}
	return nil
}

func v2ActionFields(action string) []string {
	switch action {
	case "start":
		return []string{"operation_id", "workspace_id", "activity_id", "workspace_hint", "structured_adapter", "command", "argv", "intent", "cwd", "tty", "yield_time_ms", "timeout_ms", "max_output_bytes"}
	case "poll":
		return []string{"session_id", "cursor", "yield_time_ms", "max_output_bytes"}
	case "write":
		return []string{"session_id", "input_offset", "chars", "eof"}
	case "kill":
		return []string{"session_id", "kill_id", "signal"}
	case "inspect.server":
		return nil
	case "inspect.project", "inspect.workspace", "inspect.readiness":
		return []string{"workspace_id"}
	case "inspect.activity":
		return []string{"activity_id"}
	case "inspect.events":
		return []string{"target", "after_event_cursor", "max_events"}
	case "inspect.structured":
		return []string{"operation_id", "record_kind", "severity", "path", "test_status", "continuation", "max_records"}
	case "inspect.telemetry":
		return []string{"operation_id", "max_samples"}
	case "repro.create":
		return []string{"repro_create_id", "operation_id", "capture_policy"}
	case "inspect.repro":
		return []string{"repro_id"}
	case "inspect.code":
		return []string{"workspace_id", "activity_id", "code_query"}
	default:
		return nil
	}
}

func validReproIDInput(value string) bool {
	if len(value) != 32 || !strings.HasPrefix(value, "repro_") {
		return false
	}
	for _, r := range value[6:] {
		if strings.ContainsRune("0123456789ABCDEFGHJKMNPQRSTVWXYZ", r) {
			continue
		}
		return false
	}
	return true
}

func isDeferredAction(action string) bool {
	switch action {
	case "read_output":
		return true
	default:
		return false
	}
}
