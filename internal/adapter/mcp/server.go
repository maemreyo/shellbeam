// Package mcp exposes ShellBeam's single stateless MCP tool using the official SDK.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/maemreyo/shellbeam/api/schema"
	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"
)

const Instructions = "ShellBeam runs commands as the local OS user with full authority. Use it only for intended local execution. For start, create one operation_id and reuse it for every retry; if the outcome is unknown, never create another. Poll with session_id and cursor. For write, use next_input_offset; acceptance means queued, while the terminal receipt proves delivery. For kill, create one kill_id and reuse it. Never infer command success from MCP success; require a terminal receipt and spawn/exit evidence."

func ToolDefinition() *mcpgo.Tool {
	destructive, open := true, true
	input, _ := schema.Load(schema.MCPInputV1)
	output, _ := schema.Load(schema.MCPOutputV1)
	return &mcpgo.Tool{Name: "local_shell", Title: "ShellBeam — Local Shell", Description: "Run and control commands with the full authority of the local OS user. Transport success is not command success; inspect the terminal receipt.", InputSchema: json.RawMessage(input), OutputSchema: json.RawMessage(output), Annotations: &mcpgo.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: &destructive, OpenWorldHint: &open, IdempotentHint: false}}
}

func New(handler *bridge.Handler) *mcpgo.Server {
	server := mcpgo.NewServer(&mcpgo.Implementation{Name: "shellbeam", Title: "ShellBeam", Version: "v1"}, &mcpgo.ServerOptions{Instructions: Instructions})
	server.AddTool(ToolDefinition(), func(ctx context.Context, req *mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		return call(ctx, handler, req)
	})
	return server
}

func Run(ctx context.Context, handler *bridge.Handler) error {
	return New(handler).Run(ctx, &mcpgo.StdioTransport{})
}

type input struct {
	Action         string `json:"action"`
	OperationID    string `json:"operation_id,omitempty"`
	Command        string `json:"command,omitempty"`
	CWD            string `json:"cwd,omitempty"`
	TTY            bool   `json:"tty,omitempty"`
	YieldMS        int64  `json:"yield_time_ms,omitempty"`
	TimeoutMS      int64  `json:"timeout_ms,omitempty"`
	MaxOutputBytes int    `json:"max_output_bytes,omitempty"`
	SessionID      string `json:"session_id,omitempty"`
	Cursor         int64  `json:"cursor,omitempty"`
	InputOffset    int64  `json:"input_offset,omitempty"`
	Chars          string `json:"chars,omitempty"`
	EOF            bool   `json:"eof,omitempty"`
	KillID         string `json:"kill_id,omitempty"`
	Signal         string `json:"signal,omitempty"`
}

func call(ctx context.Context, h *bridge.Handler, req *mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	var in input
	d := json.NewDecoder(bytesReader(req.Params.Arguments))
	d.DisallowUnknownFields()
	if err := d.Decode(&in); err != nil {
		return toolError("invalid_request", err.Error(), false), nil
	}
	if err := validate(in); err != nil {
		return toolError("invalid_request", err.Error(), false), nil
	}
	request := bridge.Request{Action: in.Action}
	yieldMS, maxOutput := in.YieldMS, in.MaxOutputBytes
	if !hasField(req.Params.Arguments, "yield_time_ms") {
		if in.Action == "start" {
			yieldMS = 10000
		}
	}
	if !hasField(req.Params.Arguments, "max_output_bytes") {
		maxOutput = 20000
	}
	switch in.Action {
	case "start":
		request.Start = app.StartRequest{OperationID: in.OperationID, Command: in.Command, CWD: in.CWD, TTY: in.TTY, YieldMS: yieldMS, TimeoutMS: in.TimeoutMS, MaxOutputBytes: maxOutput}
	case "poll":
		request.Poll = app.PollRequest{SessionID: in.SessionID, Cursor: in.Cursor, YieldMS: yieldMS, MaxOutputBytes: maxOutput}
	case "write":
		request.Write = app.WriteRequest{SessionID: in.SessionID, InputOffset: in.InputOffset, Chars: in.Chars, EOF: in.EOF}
	case "kill":
		sig := in.Signal
		if sig == "" {
			sig = "TERM"
		}
		request.Kill = app.KillRequest{SessionID: in.SessionID, KillID: in.KillID, Signal: sig}
	}
	out, err := h.Handle(ctx, request)
	if err != nil {
		return toolError("daemon_unavailable", "daemon request failed", true), nil
	}
	if out.Code != "" {
		return toolError(out.Code, out.Message, out.Retryable), nil
	}
	body := map[string]any{"schema_version": 1, "ok": true, "action": in.Action, "session_id": out.View.SessionID, "state": out.View.State, "outcome": out.View.Outcome, "output": out.View.Output, "cursor": out.View.Cursor, "next_cursor": out.View.NextCursor, "truncated": out.View.Truncated, "accepted_input_bytes": out.View.AcceptedInputBytes, "next_input_offset": out.View.NextInputOffset, "eof_queued": out.View.EOFQueued, "kill_id": out.View.KillID, "signal": out.View.Signal, "receipt": out.View.Receipt}
	if out.View.OperationID != "" {
		body["operation_id"] = out.View.OperationID
	}
	summary := fmt.Sprintf("%s session %s: %s", in.Action, out.View.SessionID, out.View.State)
	return &mcpgo.CallToolResult{Content: []mcpgo.Content{&mcpgo.TextContent{Text: summary}}, StructuredContent: body}, nil
}

func toolError(code, message string, retryable bool) *mcpgo.CallToolResult {
	body := map[string]any{"schema_version": 1, "ok": false, "error": map[string]any{"code": code, "message": message, "retryable": retryable}}
	return &mcpgo.CallToolResult{IsError: true, Content: []mcpgo.Content{&mcpgo.TextContent{Text: code + ": " + message}}, StructuredContent: body}
}
