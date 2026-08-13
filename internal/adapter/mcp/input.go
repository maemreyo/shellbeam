package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"io"
)

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }
func hasField(raw []byte, key string) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return false
	}
	_, ok := fields[key]
	return ok
}
func validate(v input) error {
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
	if v.YieldMS < 0 || v.TimeoutMS < 0 || v.MaxOutputBytes < 0 || v.Cursor < 0 {
		return fmt.Errorf("negative value")
	}
	return nil
}
