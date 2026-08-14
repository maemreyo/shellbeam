package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type input struct {
	Action         string                    `json:"action"`
	OperationID    string                    `json:"operation_id,omitempty"`
	WorkspaceID    string                    `json:"workspace_id,omitempty"`
	WorkspaceHint  *workspace.Hint           `json:"workspace_hint,omitempty"`
	Command        string                    `json:"command,omitempty"`
	Argv           []string                  `json:"argv,omitempty"`
	Intent         *operation.DeclaredIntent `json:"intent,omitempty"`
	CWD            string                    `json:"cwd,omitempty"`
	TTY            bool                      `json:"tty,omitempty"`
	YieldMS        int64                     `json:"yield_time_ms,omitempty"`
	TimeoutMS      int64                     `json:"timeout_ms,omitempty"`
	MaxOutputBytes int                       `json:"max_output_bytes,omitempty"`
	SessionID      string                    `json:"session_id,omitempty"`
	Cursor         int64                     `json:"cursor,omitempty"`
	InputOffset    int64                     `json:"input_offset,omitempty"`
	Chars          string                    `json:"chars,omitempty"`
	EOF            bool                      `json:"eof,omitempty"`
	KillID         string                    `json:"kill_id,omitempty"`
	Signal         string                    `json:"signal,omitempty"`
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
	if v.Action == "inspect.project" {
		_, err := workspace.ParseWorkspaceID(v.WorkspaceID)
		return err
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
	return validateNonNegative(v)
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
		return []string{"operation_id", "workspace_id", "workspace_hint", "command", "argv", "intent", "cwd", "tty", "yield_time_ms", "timeout_ms", "max_output_bytes"}
	case "poll":
		return []string{"session_id", "cursor", "yield_time_ms", "max_output_bytes"}
	case "write":
		return []string{"session_id", "input_offset", "chars", "eof"}
	case "kill":
		return []string{"session_id", "kill_id", "signal"}
	case "inspect.server":
		return nil
	case "inspect.project":
		return []string{"workspace_id"}
	default:
		return nil
	}
}

func isDeferredAction(action string) bool {
	switch action {
	case "inspect.workspace", "inspect.activity", "read_output":
		return true
	default:
		return false
	}
}
