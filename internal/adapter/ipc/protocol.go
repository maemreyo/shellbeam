// Package ipc implements closed JSON over an authenticated Unix socket.
package ipc

import (
	"encoding/json"
	"fmt"
	"io"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/failure"
)

type Action struct {
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
type Request struct {
	IPVersion int    `json:"ipc_version"`
	RequestID string `json:"request_id"`
	Payload   Action `json:"payload"`
}
type Response struct {
	IPVersion int      `json:"ipc_version"`
	RequestID string   `json:"request_id"`
	OK        bool     `json:"ok"`
	View      app.View `json:"view"`
	Error     *Error   `json:"error,omitempty"`
}
type Error struct {
	Code      string            `json:"code"`
	Message   string            `json:"message"`
	Retryable bool              `json:"retryable"`
	Details   map[string]string `json:"details,omitempty"`
}

func errorEnvelope(err error) *Error {
	public := failure.Public(err)
	return &Error{
		Code:      string(public.Code),
		Message:   public.Message,
		Retryable: public.Retryable,
		Details:   public.Details,
	}
}

func decodeRequest(r io.Reader) (Request, error) {
	var v Request
	d := json.NewDecoder(io.LimitReader(r, 1<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(&v); err != nil {
		return v, err
	}
	if v.IPVersion != 1 {
		return v, fmt.Errorf("unsupported ipc version")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return v, fmt.Errorf("trailing json")
	}
	return v, nil
}

func legacyResponseView(view app.View) app.View {
	out := view
	out.AuthorityEpoch = 0
	if out.Receipt != nil && out.Receipt.SchemaVersion > 2 {
		out.Receipt = nil
		out.Failure = nil
	}
	return out
}
