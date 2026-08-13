package ipc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	"github.com/maemreyo/shellbeam/internal/core/failure"
)

const ipcV2 = 2

type RequestV2 struct {
	IPVersion      int    `json:"ipc_version"`
	Kind           string `json:"kind"`
	RequestID      string `json:"request_id"`
	Action         string `json:"action"`
	OperationID    string `json:"operation_id,omitempty"`
	Command        string `json:"command,omitempty"`
	CWD            string `json:"cwd,omitempty"`
	TTY            bool   `json:"tty,omitempty"`
	TimeoutMS      int64  `json:"timeout_ms,omitempty"`
	YieldMS        int64  `json:"yield_time_ms,omitempty"`
	MaxOutputBytes int    `json:"max_output_bytes,omitempty"`
	SessionID      string `json:"session_id,omitempty"`
	Cursor         int64  `json:"cursor,omitempty"`
	InputOffset    int64  `json:"input_offset,omitempty"`
	Chars          string `json:"chars,omitempty"`
	EOF            bool   `json:"eof,omitempty"`
	KillID         string `json:"kill_id,omitempty"`
	Signal         string `json:"signal,omitempty"`
}

type ResponseV2 struct {
	IPVersion int                 `json:"ipc_version"`
	Kind      string              `json:"kind"`
	RequestID string              `json:"request_id"`
	Action    string              `json:"action"`
	OK        bool                `json:"ok"`
	View      *app.View           `json:"view,omitempty"`
	Server    *capability.Catalog `json:"server,omitempty"`
	Error     *Error              `json:"error,omitempty"`
}

type v2Header struct {
	IPVersion int    `json:"ipc_version"`
	Kind      string `json:"kind"`
	RequestID string `json:"request_id"`
	Action    string `json:"action"`
}

func decodeRequestV2(r io.Reader) (RequestV2, error) {
	data, err := readBoundedJSON(r)
	if err != nil {
		return RequestV2{}, failure.New(failure.InvalidInput, map[string]string{"reason": "invalid_json"}, err)
	}
	var header v2Header
	if err := json.Unmarshal(data, &header); err != nil {
		return RequestV2{}, failure.New(failure.InvalidInput, map[string]string{"reason": "invalid_json"}, err)
	}
	partial := RequestV2{IPVersion: header.IPVersion, Kind: header.Kind, RequestID: header.RequestID, Action: header.Action}
	if header.IPVersion != ipcV2 {
		return partial, failure.New(failure.FeatureUnavailable, map[string]string{"feature": "ipc_version", "required_version": "2"}, fmt.Errorf("unsupported ipc version"))
	}
	if header.Kind != "request" {
		return partial, failure.New(failure.InvalidInput, map[string]string{"field": "kind"}, fmt.Errorf("invalid v2 kind"))
	}
	if isDeferredV2Action(header.Action) {
		return partial, failure.New(failure.FeatureUnavailable, map[string]string{"feature": header.Action}, fmt.Errorf("unsupported v2 feature"))
	}
	if !isSupportedV2Action(header.Action) {
		return partial, failure.New(failure.InvalidInput, map[string]string{"field": "action"}, fmt.Errorf("unknown v2 action"))
	}
	if err := validateV2FieldSet(data, header.Action); err != nil {
		return partial, err
	}
	var out RequestV2
	if err := strictDecodeV2(data, &out); err != nil {
		return partial, failure.New(failure.InvalidInput, map[string]string{"reason": "invalid_request"}, err)
	}
	if err := validateRequestV2(out); err != nil {
		return out, err
	}
	return out, nil
}

func validateV2FieldSet(data []byte, action string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return failure.New(failure.InvalidInput, map[string]string{"reason": "invalid_json"}, err)
	}
	allowed := map[string]bool{"ipc_version": true, "kind": true, "request_id": true, "action": true}
	for _, field := range actionFieldsV2(action) {
		allowed[field] = true
	}
	for field := range fields {
		if !allowed[field] {
			return failure.New(failure.InvalidInput, map[string]string{"field": field}, fmt.Errorf("unexpected field"))
		}
	}
	return nil
}

func actionFieldsV2(action string) []string {
	switch action {
	case "start":
		return []string{"operation_id", "command", "cwd", "tty", "timeout_ms", "yield_time_ms", "max_output_bytes"}
	case "poll":
		return []string{"session_id", "cursor", "yield-time_ms", "max_output_bytes"}
	case "write":
		return []string{"session_id", "input_offset", "chars", "eof"}
	case "kill":
		return []string{"session_id", "kill_id", "signal"}
	default:
		return nil
	}
}

func validateRequestV2(v RequestV2) error {
	if v.IPVersion != ipcV2 {
		return failure.New(failure.FeatureUnavailable, map[string]string{"feature": "ipc_version", "required_version": "2"}, fmt.Errorf("unsupported ipc version"))
	}
	if v.Kind != "request" {
		return failure.New(failure.InvalidInput, map[string]string{"field": "kind"}, fmt.Errorf("invalid v2 kind"))
	}
	if isDeferredV2Action(v.Action) {
		return failure.New(failure.FeatureUnavailable, map[string]string{"feature": v.Action}, fmt.Errorf("unsupported v2 feature"))
	}
	if !isSupportedV2Action(v.Action) {
		return failure.New(failure.InvalidInput, map[string]string{"field": "action"}, fmt.Errorf("unknown v2 action"))
	}
	switch v.Action {
	case "start":
		if v.OperationID == "" || v.Command == "" || v.CWD == "" {
			return failure.New(failure.InvalidInput, map[string]string{"reason": "missing_start_field"}, fmt.Errorf("missing start field"))
		}
	case "poll":
		if v.SessionID == "" {
			return failure.New(failure.InvalidInput, map[string]string{"field": "session_id"}, fmt.Errorf("missing session id"))
		}
	case "write":
		if v.SessionID == "" || (v.Chars == "" && !v.EOF) || (v.Chars != "" && v.EOF) {
			return failure.New(failure.InvalidInput, map[string]string{"reason": "invalid_write"}, fmt.Errorf("invalid write request"))
		}
	case "kill":
		if v.SessionID == "" || v.KillID == "" {
			return failure.New(failure.InvalidInput, map[string]string{"reason": "missing_kill_field"}, fmt.Errorf("missing kill field"))
		}
	}
	return nil
}

func isSupportedV2Action(action string) bool {
	switch action {
	case "start", "poll", "write", "kill", "inspect.server":
		return true
	default:
		return false
	}
}

func isDeferredV2Action(action string) bool {
	switch action {
	case "inspect.workspace", "inspect.activity", "inspect.project", "read_output":
		return true
	default:
		return false
	}
}

func readBoundedJSON(r io.Reader) ([]byte, error) {
	limited := io.LimitReader(r, (1<<20)+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(data) > 1<<20 {
		return nil, fmt.Errorf("request too large")
	}
	return data, nil
}

func strictDecodeV2(data []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return fmt.Errorf("trailing json")
	}
	return nil
}
