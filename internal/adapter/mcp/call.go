package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"
)

func call(ctx context.Context, h *bridge.Handler, req *mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	in, err := decodeInput(req.Params.Arguments)
	version := protocolGeneration(req.ProtocolVersion())
	if err != nil {
		return versionedToolError(version, "", "invalid_request", "invalid request", false), nil
	}
	if version == 2 && isDeferredAction(in.Action) {
		return toolErrorV2(in.Action, "feature_unavailable", "feature unavailable", false), nil
	}
	if err := validateForVersion(version, in, req.Params.Arguments); err != nil {
		return versionedToolError(version, in.Action, "invalid_request", err.Error(), false), nil
	}
	request := requestFromInput(version, in, req.Params.Arguments)
	out, err := h.Handle(ctx, request)
	if err != nil {
		return versionedToolError(version, in.Action, "daemon_unavailable", "daemon request failed", true), nil
	}
	if out.Code != "" {
		return versionedToolError(version, in.Action, out.Code, out.Message, out.Retryable), nil
	}
	if version == 2 {
		return successV2(in.Action, out), nil
	}
	return successV1(in.Action, out), nil
}

func decodeInput(raw []byte) (input, error) {
	var in input
	d := json.NewDecoder(bytesReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&in); err != nil {
		return input{}, err
	}
	return in, nil
}

func requestFromInput(version int, in input, raw []byte) bridge.Request {
	request := bridge.Request{Action: in.Action}
	if version == 2 || in.Action == "inspect.server" {
		request.ProtocolVersion = 2
	}
	yieldMS, maxOutput := in.YieldMS, in.MaxOutputBytes
	if !hasField(raw, "yield_time_ms") && in.Action == "start" {
		yieldMS = 10000
	}
	if !hasField(raw, "max_output_bytes") {
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
		signal := in.Signal
		if signal == "" {
			signal = "TERM"
		}
		request.Kill = app.KillRequest{SessionID: in.SessionID, KillID: in.KillID, Signal: signal}
	}
	return request
}

func successV1(action string, out bridge.Response) *mcpgo.CallToolResult {
	if action == "inspect.server" {
		if out.Server == nil {
			return toolError("invalid_daemon_response", "server catalog missing", false)
		}
		body := map[string]any{"schema_version": 1, "ok": true, "action": action, "server": out.Server}
		return toolSuccess("inspect.server: capabilities", body)
	}
	body := map[string]any{
		"schema_version": 1, "ok": true, "action": action,
		"session_id": out.View.SessionID, "state": out.View.State, "outcome": out.View.Outcome,
		"output": out.View.Output, "cursor": out.View.Cursor, "next_cursor": out.View.NextCursor,
		"truncated": out.View.Truncated, "accepted_input_bytes": out.View.AcceptedInputBytes,
		"next_input_offset": out.View.NextInputOffset, "eof_queued": out.View.EOFQueued,
		"kill_id": out.View.KillID, "signal": out.View.Signal, "receipt": out.View.Receipt,
	}
	if out.View.OperationID != "" {
		body["operation_id"] = out.View.OperationID
	}
	return toolSuccess(fmt.Sprintf("%s session %s: %s", action, out.View.SessionID, out.View.State), body)
}

func successV2(action string, out bridge.Response) *mcpgo.CallToolResult {
	body := map[string]any{"schema_version": 2, "ok": true, "action": action}
	summary := action
	switch action {
	case "start", "poll":
		if out.Result == nil {
			return toolErrorV2(action, "invalid_daemon_response", "structured result missing", false)
		}
		body["result"] = out.Result
		summary = fmt.Sprintf("%s session %s: %s", action, out.Result.Operation.SessionID, out.Result.Operation.State)
	case "write", "kill":
		body["view"] = controlView(out.View)
		summary = fmt.Sprintf("%s session %s: %s", action, out.View.SessionID, out.View.State)
	case "inspect.server":
		if out.Server == nil {
			return toolErrorV2(action, "invalid_daemon_response", "server catalog missing", false)
		}
		body["server"] = out.Server
		summary = "inspect.server: capabilities"
	}
	return toolSuccess(summary, body)
}

func controlView(view app.View) map[string]any {
	body := map[string]any{
		"session_id": view.SessionID, "state": view.State, "outcome": view.Outcome,
		"cursor": view.Cursor, "next_cursor": view.NextCursor, "truncated": view.Truncated,
		"accepted_input_bytes": view.AcceptedInputBytes, "next_input_offset": view.NextInputOffset,
		"eof_queued": view.EOFQueued, "kill_id": view.KillID, "signal": view.Signal,
	}
	if view.OperationID != "" {
		body["operation_id"] = view.OperationID
	}
	if view.Output != "" {
		body["output"] = view.Output
	}
	if view.Receipt != nil {
		body["receipt"] = view.Receipt
	}
	return body
}

func toolSuccess(summary string, body map[string]any) *mcpgo.CallToolResult {
	return &mcpgo.CallToolResult{Content: []mcpgo.Content{&mcpgo.TextContent{Text: summary}}, StructuredContent: body}
}

func versionedToolError(version int, action, code, message string, retryable bool) *mcpgo.CallToolResult {
	if version == 2 {
		return toolErrorV2(action, code, message, retryable)
	}
	return toolError(code, message, retryable)
}

func toolError(code, message string, retryable bool) *mcpgo.CallToolResult {
	body := map[string]any{"schema_version": 1, "ok": false, "error": map[string]any{"code": code, "message": message, "retryable": retryable}}
	return &mcpgo.CallToolResult{IsError: true, Content: []mcpgo.Content{&mcpgo.TextContent{Text: code + ": " + message}}, StructuredContent: body}
}

func toolErrorV2(action, code, message string, retryable bool) *mcpgo.CallToolResult {
	body := map[string]any{"schema_version": 2, "ok": false, "action": action, "error": map[string]any{"code": code, "message": message, "retryable": retryable}}
	return &mcpgo.CallToolResult{IsError: true, Content: []mcpgo.Content{&mcpgo.TextContent{Text: code + ": " + message}}, StructuredContent: body}
}
